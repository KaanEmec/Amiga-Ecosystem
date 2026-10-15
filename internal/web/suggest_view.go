package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
)

// suggestLayout is the data every public suggestion page passes to
// "public_layout".
type suggestLayout struct {
	Title   string
	Section string // list, new, detail
	SiteKey string // Turnstile site key; "" renders no widget
}

func (s *Server) suggestLayout(title, section string) suggestLayout {
	return suggestLayout{
		Title: title, Section: section, SiteKey: s.suggest.siteKey,
	}
}

// suggestRow is a public suggestion with its display labels.
type suggestRow struct {
	suggest.Public
	TypeLabel   string
	StatusLabel string
	StatusTone  string
	Open        bool  // votes may still change
	Decided     int64 // decided_at, 0 when pending
	DupOf       int64 // canonical suggestion of a duplicate, 0 when none
}

func newSuggestRow(p suggest.Public) suggestRow {
	label, tone := statusInfo(p.Status)
	row := suggestRow{Public: p, TypeLabel: typeLabel(p.Type), StatusLabel: label, StatusTone: tone, Open: p.Status == store.StatusPending}
	if p.DecidedAt != nil {
		row.Decided = *p.DecidedAt
	}
	if p.DuplicateOf != nil {
		row.DupOf = *p.DuplicateOf
	}
	return row
}

// ---------------------------------------------------------------- list

type suggestListPage struct {
	suggestLayout
	P                 suggest.ListParams
	Items             []suggestRow
	Total, Page       int
	Pages             int
	PrevURL, NextURL  string
	TypeOptions       []suggest.Option
	StatusOptions     []suggest.Option
	SubmissionsPaused bool
	VotingPaused      bool
}

var statusFilterOptions = []suggest.Option{
	{ID: "", Label: "Any status"}, {ID: store.StatusPending, Label: "Pending"},
	{ID: store.StatusAccepted, Label: "Accepted"}, {ID: store.StatusRejected, Label: "Rejected"},
	{ID: store.StatusDuplicate, Label: "Duplicate"},
}

func typeOptions(withAny bool) []suggest.Option {
	var out []suggest.Option
	if withAny {
		out = append(out, suggest.Option{ID: "", Label: "Any type"})
	}
	for _, t := range suggest.Types {
		out = append(out, suggest.Option{ID: t, Label: typeLabel(t)})
	}
	return out
}

func (s *Server) newListPage(p suggest.ListParams, res suggest.ListResult, subPaused, votePaused bool) suggestListPage {
	page := suggestListPage{
		suggestLayout: s.suggestLayout("Suggestions", "list"),
		P:             p, Total: res.Total, Page: res.Page, Pages: res.Pages,
		TypeOptions: typeOptions(true), StatusOptions: statusFilterOptions,
		SubmissionsPaused: subPaused, VotingPaused: votePaused,
	}
	for _, it := range res.Items {
		page.Items = append(page.Items, newSuggestRow(it))
	}
	link := func(n int) string {
		v := url.Values{}
		for k, val := range map[string]string{"q": p.Query, "type": p.Type, "status": p.Status} {
			if val != "" {
				v.Set(k, val)
			}
		}
		if p.Sort != "newest" {
			v.Set("sort", p.Sort)
		}
		if n > 1 {
			v.Set("page", strconv.Itoa(n))
		}
		if len(v) == 0 {
			return "/suggestions"
		}
		return "/suggestions?" + v.Encode()
	}
	if res.Page > 1 {
		page.PrevURL = link(res.Page - 1)
	}
	if res.Page < res.Pages {
		page.NextURL = link(res.Page + 1)
	}
	return page
}

// ---------------------------------------------------------------- detail

type fieldRow struct {
	Label    string
	Current  string
	Proposed string
	Removed  bool // the proposal removes this field
}

type imageView struct {
	URL, Credit, SourcePage string
}

type suggestDetailPage struct {
	suggestLayout
	S            suggestRow
	Created      bool
	EntryName    string
	EntryLive    bool
	Rows         []fieldRow
	Image        *imageView
	FinalRows    []fieldRow
	VotingPaused bool
}

func (s *Server) newDetailPage(r *http.Request, sg suggest.Public) (suggestDetailPage, error) {
	ctx := r.Context()
	page := suggestDetailPage{
		suggestLayout: s.suggestLayout(fmt.Sprintf("Suggestion #%d: %s", sg.ID, sg.Title), "detail"),
		S:             newSuggestRow(sg),
		Created:       r.URL.Query().Get("created") == "1",
		EntryName:     sg.Title,
	}
	_, votePaused, err := s.suggest.svc.Paused(ctx)
	if err != nil {
		return page, err
	}
	page.VotingPaused = votePaused
	opts, err := s.suggest.svc.Options(ctx)
	if err != nil {
		return page, err
	}
	payload := decodeObject(sg.Payload)
	snapshot := decodeObject(sg.Snapshot)
	final := decodeObject(sg.Final)

	ids := append(relatedIDs(payload), relatedIDs(snapshot)...)
	ids = append(ids, relatedIDs(final)...)
	if sg.EntryID != "" {
		ids = append(ids, sg.EntryID)
	}
	names, err := s.suggest.svc.EntryNames(ctx, ids)
	if err != nil {
		return page, err
	}
	if n, ok := names[sg.EntryID]; ok {
		page.EntryName, page.EntryLive = n, true
	}
	f := valueFormatter{opts: opts, names: names}

	switch sg.Type {
	case suggest.TypeNew:
		for _, k := range orderedKeys(payload) {
			page.Rows = append(page.Rows, fieldRow{Label: fieldLabel(k), Proposed: f.format(k, payload[k])})
		}
	case suggest.TypeChange:
		for _, k := range orderedKeys(payload) {
			row := fieldRow{Label: fieldLabel(k), Current: f.format(k, snapshot[k]), Proposed: f.format(k, payload[k])}
			row.Removed = isNullJSON(payload[k])
			page.Rows = append(page.Rows, row)
		}
	case suggest.TypeImage:
		var img struct {
			URL        string `json:"image_url"`
			Credit     string `json:"credit"`
			SourcePage string `json:"source_page"`
		}
		_ = json.Unmarshal(sg.Payload, &img)
		page.Image = &imageView{URL: img.URL, Credit: img.Credit, SourcePage: img.SourcePage}
	}
	for _, k := range orderedKeys(final) {
		page.FinalRows = append(page.FinalRows, fieldRow{Label: fieldLabel(k), Proposed: f.format(k, final[k])})
	}
	return page, nil
}

func decodeObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func isNullJSON(raw json.RawMessage) bool {
	return len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null"
}

// orderedKeys returns m's keys in entry field order, then the rest sorted.
func orderedKeys(m map[string]json.RawMessage) []string {
	var keys []string
	for _, k := range suggest.FieldOrder {
		if _, ok := m[k]; ok {
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range m {
		if !slices.Contains(suggest.FieldOrder, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return append(keys, rest...)
}

func relatedIDs(m map[string]json.RawMessage) []string {
	var ids []string
	_ = json.Unmarshal(m["related"], &ids)
	return ids
}

func fieldLabel(k string) string {
	if l, ok := suggest.FieldLabels[k]; ok {
		return l
	}
	return k
}

// valueFormatter renders entry field values as text, with vocabulary labels
// and entry names.
type valueFormatter struct {
	opts  suggest.Options
	names map[string]string
}

func (f valueFormatter) format(key string, raw json.RawMessage) string {
	if isNullJSON(raw) {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	one := func(s string) string {
		switch key {
		case "category":
			return f.opts.CategoryLabel(s)
		case "kind":
			return suggest.Label(f.opts.Kinds, s)
		case "status":
			return suggest.Label(f.opts.Statuses, s)
		case "arch":
			return suggest.Label(f.opts.Archs, s)
		case "related":
			if n, ok := f.names[s]; ok {
				return n
			}
		}
		return s
	}
	switch x := v.(type) {
	case string:
		return one(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case []any:
		parts := make([]string, 0, len(x))
		for _, it := range x {
			if s, ok := it.(string); ok {
				parts = append(parts, one(s))
			} else {
				b, _ := json.Marshal(it)
				parts = append(parts, string(b))
			}
		}
		return strings.Join(parts, ", ")
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------------------------------------------------------------- form

// formValues is the suggestion form's state, re-rendered on failure.
type formValues struct {
	Type, Entry                                                 string
	Name, ID, Year, Category, Kind, Status, By, What, Why, Tags string
	URL, Version                                                string
	Arch, Related                                               []string
	ImageURL, ImageCredit, ImageSourcePage                      string
	Summary, Reason, Sources, Contact                           string
}

// HasArch reports whether an architecture is selected.
func (v formValues) HasArch(id string) bool { return slices.Contains(v.Arch, id) }

func formFromRequest(r *http.Request) formValues {
	f := r.PostForm
	get := func(k string) string { return strings.TrimSpace(f.Get(k)) }
	v := formValues{
		Type: get("type"), Entry: get("entry"),
		Name: get("f_name"), ID: get("f_id"), Year: get("f_year"), Category: get("f_category"),
		Kind: get("f_kind"), Status: get("f_status"), By: get("f_by"), What: get("f_what"), Why: get("f_why"),
		Tags: get("f_tags"), URL: get("f_url"), Version: get("f_version"),
		Arch:     f["f_arch"],
		ImageURL: get("image_url"), ImageCredit: get("image_credit"), ImageSourcePage: get("image_source_page"),
		Summary: get("summary"), Reason: get("reason"), Sources: f.Get("sources"), Contact: get("contact"),
	}
	v.Related = append(v.Related, f["f_related"]...)
	v.Related = append(v.Related, splitList(get("f_related_add"))...)
	return v
}

func (v *formValues) fillFromEntry(doc json.RawMessage) {
	var e struct {
		Name, Category, Kind, Status, By, What, Why, URL, Version string
		Year                                                      int
		Arch, Related, Tags                                       []string
	}
	if err := json.Unmarshal(doc, &e); err != nil {
		return
	}
	v.Name, v.Category, v.Kind, v.Status, v.By, v.What, v.Why = e.Name, e.Category, e.Kind, e.Status, e.By, e.What, e.Why
	v.URL, v.Version, v.Arch, v.Related, v.Tags = e.URL, e.Version, e.Arch, e.Related, strings.Join(e.Tags, ", ")
	if e.Year != 0 {
		v.Year = strconv.Itoa(e.Year)
	}
}

// inputProblems reports form-only problems the service cannot see.
func (v formValues) inputProblems() []string {
	if (v.Type == suggest.TypeNew || v.Type == suggest.TypeChange) && v.Year != "" {
		if _, err := strconv.Atoi(v.Year); err != nil {
			return []string{"Year must be a number, like 1992"}
		}
	}
	return nil
}

func (v formValues) proposal() suggest.Proposal {
	p := suggest.Proposal{
		Type: v.Type, EntryID: v.Entry, ImageURL: v.ImageURL, ImageCredit: v.ImageCredit,
		ImageSourcePage: v.ImageSourcePage, Summary: v.Summary, Reason: v.Reason,
		Sources: strings.Fields(v.Sources), Contact: v.Contact,
	}
	if v.Type != suggest.TypeNew && v.Type != suggest.TypeChange {
		return p
	}
	str := func(s string) *string { return &s }
	nonNil := func(l []string) []string {
		if l == nil {
			return []string{}
		}
		return l
	}
	p.Fields = suggest.Fields{
		Name: str(v.Name), Category: str(v.Category), Kind: str(v.Kind), Status: str(v.Status),
		Arch: nonNil(v.Arch), By: str(v.By), What: str(v.What), Why: str(v.Why),
		Related: nonNil(v.Related), Tags: nonNil(splitList(v.Tags)), URL: str(v.URL), Version: str(v.Version),
	}
	if y, err := strconv.Atoi(v.Year); err == nil {
		p.Fields.Year = &y
	}
	if v.Type == suggest.TypeNew {
		p.Fields.ID = str(v.ID)
	}
	return p
}

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
}

type suggestFormPage struct {
	suggestLayout
	V                 formValues
	Errors            []string
	FormTS            string
	Options           suggest.Options
	TypeOptions       []suggest.Option
	EntryName         string
	Related           []suggest.Option // selected related ids with names
	Pending           []suggestRow     // pending suggestions about the chosen entry
	SubmissionsPaused bool
}

func (s *Server) renderSuggestForm(w http.ResponseWriter, r *http.Request, status int, v formValues, errs []string) {
	ctx := r.Context()
	if !slices.Contains(suggest.Types, v.Type) {
		v.Type = suggest.TypeNew
	}
	opts, err := s.suggest.svc.Options(ctx)
	if err != nil {
		s.suggestFail(w, r, err)
		return
	}
	subPaused, _, err := s.suggest.svc.Paused(ctx)
	if err != nil {
		s.suggestFail(w, r, err)
		return
	}
	names, err := s.suggest.svc.EntryNames(ctx, append([]string{v.Entry}, v.Related...))
	if err != nil {
		s.suggestFail(w, r, err)
		return
	}
	page := suggestFormPage{
		suggestLayout: s.suggestLayout("Suggest an addition or change", "new"),
		V:             v, Errors: errs, FormTS: s.suggest.signer.FormToken(s.suggest.now()),
		Options: opts, TypeOptions: typeOptions(false), EntryName: names[v.Entry],
		SubmissionsPaused: subPaused,
	}
	for _, id := range v.Related {
		label := names[id]
		if label == "" {
			label = id
		}
		page.Related = append(page.Related, suggest.Option{ID: id, Label: label})
	}
	if page.EntryName != "" {
		pending, err := s.suggest.svc.ForEntry(ctx, v.Entry)
		if err != nil {
			s.suggestFail(w, r, err)
			return
		}
		for _, p := range pending {
			page.Pending = append(page.Pending, newSuggestRow(p))
		}
	}
	s.render(w, status, "suggestions_new.html", page)
}

type suggestErrorPage struct {
	suggestLayout
	Heading, Message string
}

func (s *Server) renderSuggestError(w http.ResponseWriter, status int, heading, msg string) {
	s.render(w, status, "suggestions_error.html", suggestErrorPage{
		suggestLayout: s.suggestLayout(heading, "error"), Heading: heading, Message: msg,
	})
}
