package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/store"
)

// --- entry list ---------------------------------------------------------------

type adminEntryRow struct {
	ID       string
	Name     string
	Year     int
	Category string
	Weight   int
	Rev      int64
	Deleted  bool
	Updated  int64
}

type adminEntriesView struct {
	adminView
	Q       string
	Deleted bool
	Rows    []adminEntryRow
	Live    int
	Removed int
}

func (s *Server) adminEntries(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	withDeleted := r.URL.Query().Get("deleted") == "1"
	rows, err := s.store.Q().ListEntries(r.Context(), true)
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	v := adminEntriesView{adminView: s.adminViewFor(r, "Entries", "entries"), Q: q, Deleted: withDeleted}
	needle := strings.ToLower(q)
	for _, row := range rows {
		deleted := row.DeletedAt != nil
		if deleted {
			v.Removed++
		} else {
			v.Live++
		}
		if deleted && !withDeleted {
			continue
		}
		e, err := catalog.ParseEntry(row.Doc)
		if err != nil {
			s.adminFail(w, r, err)
			return
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.ID), needle) && !strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		er := adminEntryRow{ID: row.ID, Name: e.Name, Category: e.Category, Rev: row.Rev, Deleted: deleted, Updated: row.UpdatedAt}
		if e.Year != nil {
			er.Year = *e.Year
		}
		if e.Weight != nil {
			er.Weight = *e.Weight
		}
		v.Rows = append(v.Rows, er)
	}
	s.render(w, http.StatusOK, "admin_entries.html", v)
}

// --- entry form ---------------------------------------------------------------

// adminForm holds the editable entry fields as submitted strings.
type adminForm struct {
	ID, Name, Year, By, Category, Kind, Status, Weight string
	Version, URL, What, Why, Tags                      string
	Image, ImageCredit, ImageFit, ImageBackground      string
	ImagePosition, Reason                              string
	Arch, Related                                      []string
	Unverified                                         bool
}

type adminOpt struct {
	Value, Label string
	Selected     bool
}

type adminOptGroup struct {
	Label   string
	Options []adminOpt
}

type adminFieldDiff struct {
	Key, Before, After string
}

type adminRevision struct {
	Rev     int64
	Actor   string
	Reason  string
	When    int64
	Kind    string // created, edited, removed, restored
	Changes []adminFieldDiff
	CanUse  bool // has an "after" document that differs from the live one
}

type adminEntryView struct {
	adminView
	IsNew      bool
	ID         string
	Rev        int64
	Deleted    bool
	Form       adminForm
	Categories []adminOptGroup
	Kinds      []adminOpt
	Statuses   []adminOpt
	Archs      []adminOpt
	Weights    []adminOpt
	Fits       []adminOpt
	Related    []adminOpt
	Revisions  []adminRevision
	Images     []store.Image
	Dangling   bool // restore blocked by missing related entries
}

// adminFormFromDoc fills the form from an entry document.
func adminFormFromDoc(doc json.RawMessage) (adminForm, error) {
	var m struct {
		ID              string   `json:"id"`
		Name            string   `json:"name"`
		Year            *int     `json:"year"`
		By              string   `json:"by"`
		Category        string   `json:"category"`
		Kind            string   `json:"kind"`
		Status          string   `json:"status"`
		Weight          *int     `json:"weight"`
		Version         string   `json:"version"`
		URL             string   `json:"url"`
		What            string   `json:"what"`
		Why             string   `json:"why"`
		Tags            []string `json:"tags"`
		Image           string   `json:"image"`
		ImageCredit     string   `json:"imageCredit"`
		ImageFit        string   `json:"imageFit"`
		ImageBackground string   `json:"imageBackground"`
		ImagePosition   string   `json:"imagePosition"`
		Arch            []string `json:"arch"`
		Related         []string `json:"related"`
		Unverified      bool     `json:"unverified"`
	}
	if err := json.Unmarshal(doc, &m); err != nil {
		return adminForm{}, fmt.Errorf("decoding entry: %w", err)
	}
	f := adminForm{
		ID: m.ID, Name: m.Name, By: m.By, Category: m.Category, Kind: m.Kind, Status: m.Status,
		Version: m.Version, URL: m.URL, What: m.What, Why: m.Why, Tags: strings.Join(m.Tags, ", "),
		Image: m.Image, ImageCredit: m.ImageCredit, ImageFit: m.ImageFit, ImageBackground: m.ImageBackground,
		ImagePosition: m.ImagePosition, Arch: m.Arch, Related: m.Related, Unverified: m.Unverified,
	}
	if m.Year != nil {
		f.Year = strconv.Itoa(*m.Year)
	}
	if m.Weight != nil {
		f.Weight = strconv.Itoa(*m.Weight)
	}
	return f, nil
}

func adminFormFromRequest(r *http.Request) adminForm {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	_ = r.ParseForm()
	return adminForm{
		ID: v("id"), Name: v("name"), Year: v("year"), By: v("by"), Category: v("category"), Kind: v("kind"),
		Status: v("status"), Weight: v("weight"), Version: v("version"), URL: v("url"), What: v("what"), Why: v("why"),
		Tags: v("tags"), Image: v("image"), ImageCredit: v("imageCredit"), ImageFit: v("imageFit"),
		ImageBackground: v("imageBackground"), ImagePosition: v("imagePosition"), Reason: v("reason"),
		Arch: adminNonEmpty(r.PostForm["arch"]), Related: adminNonEmpty(r.PostForm["related"]),
		Unverified: r.PostFormValue("unverified") == "1",
	}
}

func adminNonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// adminDocFromForm applies the form to base (the current document, or empty for
// a new entry), keeping key order and any fields the form does not cover.
func adminDocFromForm(base json.RawMessage, f adminForm, isNew bool) (json.RawMessage, []string) {
	var problems []string
	str := func(key, val string) catalog.Field {
		if val == "" {
			return catalog.Field{Key: key, Remove: true}
		}
		return catalog.Field{Key: key, Value: val}
	}
	num := func(key, val string) catalog.Field {
		if val == "" {
			return catalog.Field{Key: key, Remove: true}
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s must be a whole number", key))
			return catalog.Field{Key: key, Remove: true}
		}
		return catalog.Field{Key: key, Value: n}
	}
	list := func(key string, vals []string) catalog.Field {
		if len(vals) == 0 {
			return catalog.Field{Key: key, Remove: true}
		}
		return catalog.Field{Key: key, Value: vals}
	}
	var tags []string
	for t := range strings.SplitSeq(f.Tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	var fields []catalog.Field
	if isNew {
		fields = append(fields, catalog.Field{Key: "id", Value: f.ID})
	}
	fields = append(fields,
		str("image", f.Image), str("imageCredit", f.ImageCredit), str("imageFit", f.ImageFit),
		str("imageBackground", f.ImageBackground), str("imagePosition", f.ImagePosition),
		catalog.Field{Key: "name", Value: f.Name}, num("year", f.Year),
		catalog.Field{Key: "category", Value: f.Category}, catalog.Field{Key: "kind", Value: f.Kind},
		catalog.Field{Key: "status", Value: f.Status}, catalog.Field{Key: "arch", Value: adminOrEmpty(f.Arch)},
		num("weight", f.Weight), str("by", f.By),
		catalog.Field{Key: "what", Value: f.What}, catalog.Field{Key: "why", Value: f.Why},
		str("version", f.Version), str("url", f.URL), list("related", f.Related), list("tags", tags),
	)
	if f.Unverified {
		fields = append(fields, catalog.Field{Key: "unverified", Value: true})
	} else {
		fields = append(fields, catalog.Field{Key: "unverified", Remove: true})
	}
	doc, err := catalog.Patch(base, fields...)
	if err != nil {
		problems = append(problems, err.Error())
	}
	return doc, problems
}

func adminOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// adminMeta is the part of meta the form needs for its selects.
type adminMeta struct {
	Groups []struct {
		Label string `json:"label"`
		Cats  []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"cats"`
	} `json:"groups"`
	Kinds         []adminMetaItem `json:"kinds"`
	Statuses      []adminMetaItem `json:"statuses"`
	Architectures []adminMetaItem `json:"architectures"`
}

type adminMetaItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func adminOpts(items []adminMetaItem, selected ...string) []adminOpt {
	out := make([]adminOpt, 0, len(items))
	for _, it := range items {
		out = append(out, adminOpt{Value: it.ID, Label: it.Label, Selected: slices.Contains(selected, it.ID)})
	}
	return out
}

// adminWeightOpts lists the importance weights 1-5 for a select.
func adminWeightOpts(selected string) []adminOpt {
	out := make([]adminOpt, 0, 5)
	for i := 1; i <= 5; i++ {
		n := strconv.Itoa(i)
		label := n
		if i >= catalog.FeaturedWeight {
			label += " (featured landmark)"
		}
		out = append(out, adminOpt{Value: n, Label: label, Selected: n == selected})
	}
	return out
}

// adminFillEntryView adds the select options (from the live vocabulary) and the
// related picker to v, based on v.Form.
func (s *Server) adminFillEntryView(r *http.Request, v *adminEntryView) error {
	ctx := r.Context()
	q := s.store.Q()
	metaRaw, err := q.GetMeta(ctx, store.MetaMeta)
	if err != nil {
		return err
	}
	var meta adminMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return fmt.Errorf("decoding meta: %w", err)
	}
	f := v.Form
	for _, g := range meta.Groups {
		grp := adminOptGroup{Label: g.Label}
		for _, c := range g.Cats {
			grp.Options = append(grp.Options, adminOpt{Value: c.ID, Label: c.Label, Selected: c.ID == f.Category})
		}
		v.Categories = append(v.Categories, grp)
	}
	v.Kinds = adminOpts(meta.Kinds, f.Kind)
	v.Statuses = adminOpts(meta.Statuses, f.Status)
	v.Archs = adminOpts(meta.Architectures, f.Arch...)
	v.Weights = adminWeightOpts(f.Weight)
	for _, fit := range []string{"", "contain", "cover"} {
		label := fit
		if fit == "" {
			label = "default (cover)"
		}
		v.Fits = append(v.Fits, adminOpt{Value: fit, Label: label, Selected: fit == f.ImageFit})
	}

	// Related picker: current links first in their order, then every other
	// live entry in catalogue order.
	rows, err := q.ListEntries(ctx, false)
	if err != nil {
		return err
	}
	labels := make(map[string]string, len(rows))
	var others []adminOpt
	for _, row := range rows {
		if row.ID == v.ID {
			continue
		}
		e, err := catalog.ParseEntry(row.Doc)
		if err != nil {
			return err
		}
		label := e.Name
		if e.Year != nil {
			label += fmt.Sprintf(" (%d)", *e.Year)
		}
		labels[row.ID] = label
		if !slices.Contains(f.Related, row.ID) {
			others = append(others, adminOpt{Value: row.ID, Label: label})
		}
	}
	v.Related = nil
	for _, id := range f.Related {
		label, ok := labels[id]
		if !ok {
			label = "(missing entry)"
		}
		v.Related = append(v.Related, adminOpt{Value: id, Label: label, Selected: true})
	}
	v.Related = append(v.Related, others...)
	return nil
}

// adminLoadEntryView builds the edit page for id from the database.
func (s *Server) adminLoadEntryView(r *http.Request, id string) (adminEntryView, error) {
	ctx := r.Context()
	q := s.store.Q()
	row, err := q.GetEntry(ctx, id)
	if err != nil {
		return adminEntryView{}, err
	}
	form, err := adminFormFromDoc(row.Doc)
	if err != nil {
		return adminEntryView{}, err
	}
	v := adminEntryView{
		adminView: s.adminViewFor(r, form.Name, "entries"),
		ID:        id, Rev: row.Rev, Deleted: row.DeletedAt != nil, Form: form,
	}
	revs, err := q.ListRevisions(ctx, id)
	if err != nil {
		return adminEntryView{}, err
	}
	for _, rv := range revs {
		ar := adminRevisionOf(rv)
		ar.CanUse = rv.After != nil && !v.Deleted && rv.Rev != row.Rev && !bytes.Equal(rv.After, row.Doc)
		v.Revisions = append(v.Revisions, ar)
	}
	if v.Images, err = q.ListImagesByEntry(ctx, id); err != nil {
		return adminEntryView{}, err
	}
	return v, nil
}

// adminRevisionOf describes one revision for the history list.
func adminRevisionOf(rv store.Revision) adminRevision {
	ar := adminRevision{Rev: rv.Rev, Actor: rv.Actor, Reason: rv.Reason, When: rv.CreatedAt, Changes: adminDiffFields(rv.Before, rv.After)}
	switch {
	case rv.Before == nil && rv.After != nil && rv.Rev == 1:
		ar.Kind = "created"
	case rv.Before == nil:
		ar.Kind = "restored"
	case rv.After == nil:
		ar.Kind = "removed"
	default:
		ar.Kind = "edited"
	}
	return ar
}

// adminDiffFields lists top-level keys that differ between two documents.
func adminDiffFields(before, after json.RawMessage) []adminFieldDiff {
	var a, b map[string]json.RawMessage
	_ = json.Unmarshal(before, &a)
	_ = json.Unmarshal(after, &b)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var out []adminFieldDiff
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range keys {
			if !yield(k) {
				return
			}
		}
	}) {
		if string(a[k]) == string(b[k]) {
			continue
		}
		out = append(out, adminFieldDiff{Key: k, Before: adminShow(a[k]), After: adminShow(b[k])})
	}
	return out
}

// adminShow renders a JSON value for display: strings unquoted, absent as "—".
func adminShow(raw json.RawMessage) string {
	if raw == nil {
		return "—"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func (s *Server) adminRenderEntry(w http.ResponseWriter, r *http.Request, status int, v adminEntryView) {
	if err := s.adminFillEntryView(r, &v); err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.render(w, status, "admin_entry.html", v)
}

func (s *Server) adminNewEntry(w http.ResponseWriter, r *http.Request) {
	v := adminEntryView{adminView: s.adminViewFor(r, "New entry", "entries"), IsNew: true, Form: adminForm{Weight: "2"}}
	s.adminRenderEntry(w, r, http.StatusOK, v)
}

func (s *Server) adminCreateEntry(w http.ResponseWriter, r *http.Request) {
	f := adminFormFromRequest(r)
	v := adminEntryView{adminView: s.adminViewFor(r, "New entry", "entries"), IsNew: true, Form: f}
	doc, problems := adminDocFromForm(nil, f, true)
	if len(problems) > 0 {
		v.Error, v.Problems = "Not published: fix the fields below.", problems
		s.adminRenderEntry(w, r, http.StatusUnprocessableEntity, v)
		return
	}
	pub, err := s.catalog.Create(r.Context(), doc, adminActor, f.Reason, nil)
	if err != nil {
		status, msg, problems, ok := adminProblems(err)
		if !ok {
			s.adminFail(w, r, err)
			return
		}
		v.Error, v.Problems = msg, problems
		s.adminRenderEntry(w, r, status, v)
		return
	}
	http.Redirect(w, r, "/admin/entries/"+pub.ID+"?ok=created", http.StatusSeeOther)
}

func (s *Server) adminEditEntry(w http.ResponseWriter, r *http.Request) {
	v, err := s.adminLoadEntryView(r, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.adminRenderEntry(w, r, http.StatusOK, v)
}

// adminExpectedRev reads the hidden rev field the form was rendered with.
func adminExpectedRev(r *http.Request) int64 {
	n, _ := strconv.ParseInt(r.PostFormValue("rev"), 10, 64)
	return n
}

// adminEntryErrorPage re-renders the entry page with an error. When keep is
// non-nil the submitted form replaces the stored values (so edits survive).
func (s *Server) adminEntryErrorPage(w http.ResponseWriter, r *http.Request, id string, err error, keep *adminForm, submittedRev int64) {
	status, msg, problems, ok := adminProblems(err)
	if !ok {
		s.adminFail(w, r, err)
		return
	}
	v, lerr := s.adminLoadEntryView(r, id)
	if errors.Is(lerr, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if lerr != nil {
		s.adminFail(w, r, lerr)
		return
	}
	if keep != nil {
		v.Form = *keep
		if submittedRev > 0 && !errors.Is(err, catalog.ErrStale) {
			v.Rev = submittedRev
		}
	}
	if keep != nil && errors.Is(err, catalog.ErrStale) {
		msg += " The form still holds your edits and the newer revision is listed in the history; saving again publishes your version over it."
	}
	v.Notice, v.Error, v.Problems = "", msg, problems
	s.adminRenderEntry(w, r, status, v)
}

func (s *Server) adminSaveEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := s.store.Q().GetEntry(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	f := adminFormFromRequest(r)
	f.ID = id
	rev := adminExpectedRev(r)
	doc, problems := adminDocFromForm(row.Doc, f, false)
	if len(problems) > 0 {
		s.adminEntryErrorPage(w, r, id, &catalog.ValidationError{Problems: problems}, &f, rev)
		return
	}
	pub, err := s.catalog.Update(r.Context(), id, doc, rev, adminActor, f.Reason, nil)
	if err != nil {
		s.adminEntryErrorPage(w, r, id, err, &f, rev)
		return
	}
	ok := "saved"
	if pub.Unchanged {
		ok = ""
	}
	http.Redirect(w, r, "/admin/entries/"+id+"?ok="+ok, http.StatusSeeOther)
}

// adminAttachImage fetches an image URL through the safe fetcher, publishes
// it into the media dir and sets it as the entry's image in a new revision.
func (s *Server) adminAttachImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	rev := adminExpectedRev(r)
	row, err := s.store.Q().GetEntry(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	// Check staleness before downloading anything.
	if row.DeletedAt != nil {
		s.adminEntryErrorPage(w, r, id, catalog.ErrDeleted, nil, 0)
		return
	}
	if row.Rev != rev {
		s.adminEntryErrorPage(w, r, id, &catalog.StaleError{ID: id, Expected: rev, Current: row.Rev}, nil, 0)
		return
	}
	credit := strings.TrimSpace(r.PostFormValue("credit"))
	sourcePage := strings.TrimSpace(r.PostFormValue("source_page"))
	staged, err := s.admin.images.Stage(ctx, r.PostFormValue("image_url"))
	if err != nil {
		s.adminImageErrorPage(w, r, id, err)
		return
	}
	pub, err := s.admin.images.Publish(staged, id, credit, sourcePage, nil)
	if err != nil {
		_ = s.admin.images.Discard(staged)
		s.adminImageErrorPage(w, r, id, err)
		return
	}
	fields := []catalog.Field{{Key: "image", Value: pub.Path}}
	if credit != "" {
		fields = append(fields, catalog.Field{Key: "imageCredit", Value: credit})
	}
	doc, err := catalog.Patch(row.Doc, fields...)
	if err != nil {
		s.undoImage(ctx, pub)
		s.adminFail(w, r, err)
		return
	}
	if _, err := s.catalog.Update(ctx, id, doc, rev, adminActor, "image fetched from "+staged.SourceURL, nil, pub.Record); err != nil {
		s.undoImage(ctx, pub)
		s.adminEntryErrorPage(w, r, id, err, nil, 0)
		return
	}
	http.Redirect(w, r, "/admin/entries/"+id+"?ok=image", http.StatusSeeOther)
}

func (s *Server) adminImageErrorPage(w http.ResponseWriter, r *http.Request, id string, ferr error) {
	s.log.InfoContext(r.Context(), "image fetch refused", "entry", id, "err", ferr)
	v, err := s.adminLoadEntryView(r, id)
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	v.Notice = ""
	v.Error = "Image not attached: " + ferr.Error()
	s.adminRenderEntry(w, r, http.StatusUnprocessableEntity, v)
}

// adminRevertEntry publishes the "after" document of an earlier revision.
func (s *Server) adminRevertEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	target, _ := strconv.ParseInt(r.PostFormValue("to"), 10, 64)
	revs, err := s.store.Q().ListRevisions(ctx, id)
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	i := slices.IndexFunc(revs, func(rv store.Revision) bool { return rv.Rev == target && rv.After != nil })
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	reason := fmt.Sprintf("reverted to rev %d", target)
	if extra := strings.TrimSpace(r.PostFormValue("reason")); extra != "" {
		reason += ": " + extra
	}
	if _, err := s.catalog.Update(ctx, id, revs[i].After, adminExpectedRev(r), adminActor, reason, nil); err != nil {
		s.adminEntryErrorPage(w, r, id, err, nil, 0)
		return
	}
	http.Redirect(w, r, "/admin/entries/"+id+"?ok=reverted", http.StatusSeeOther)
}

// --- remove / restore -----------------------------------------------------------

type adminRemoveView struct {
	adminView
	ID   string
	Name string
	Rev  int64
	Deps catalog.Dependencies
}

func (s *Server) adminBuildRemoveView(r *http.Request, id string) (adminRemoveView, error) {
	row, err := s.store.Q().GetEntry(r.Context(), id)
	if err != nil {
		return adminRemoveView{}, err
	}
	deps, err := s.catalog.Dependencies(r.Context(), id)
	if err != nil {
		return adminRemoveView{}, err
	}
	return adminRemoveView{adminView: s.adminViewFor(r, "Remove "+deps.Name, "entries"), ID: id, Name: deps.Name, Rev: row.Rev, Deps: deps}, nil
}

func (s *Server) adminRemovePage(w http.ResponseWriter, r *http.Request) {
	v, err := s.adminBuildRemoveView(r, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, catalog.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "admin_remove.html", v)
}

func (s *Server) adminRemoveEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if r.PostFormValue("confirm") != "1" {
		v, err := s.adminBuildRemoveView(r, id)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, catalog.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.adminFail(w, r, err)
			return
		}
		v.Error = "Tick the confirmation box to remove this entry and clean up its links."
		s.render(w, http.StatusUnprocessableEntity, "admin_remove.html", v)
		return
	}
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if _, err := s.catalog.Remove(r.Context(), id, adminExpectedRev(r), adminActor, reason, nil); err != nil {
		s.adminEntryErrorPage(w, r, id, err, nil, 0)
		return
	}
	http.Redirect(w, r, "/admin/entries/"+id+"?ok=removed", http.StatusSeeOther)
}

func (s *Server) adminRestoreEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	var err error
	if r.PostFormValue("drop_missing") == "1" {
		err = s.adminRestoreDroppingMissing(r, id, reason)
	} else {
		_, err = s.catalog.Restore(ctx, id, adminActor, reason)
	}
	if err != nil {
		var ve *catalog.ValidationError
		if errors.As(err, &ve) {
			v, lerr := s.adminLoadEntryView(r, id)
			if lerr != nil {
				s.adminFail(w, r, lerr)
				return
			}
			v.Notice = ""
			v.Error = "Not restored: the entry would not validate any more. Restore the missing entries first, or restore without the links to them."
			v.Problems, v.Dangling = ve.Problems, true
			s.adminRenderEntry(w, r, http.StatusConflict, v)
			return
		}
		s.adminEntryErrorPage(w, r, id, err, nil, 0)
		return
	}
	http.Redirect(w, r, "/admin/entries/"+id+"?ok=restored", http.StatusSeeOther)
}

// adminRestoreDroppingMissing restores id after removing related links to
// entries that are no longer live.
func (s *Server) adminRestoreDroppingMissing(r *http.Request, id, reason string) error {
	ctx := r.Context()
	q := s.store.Q()
	row, err := q.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	live, err := q.ListEntries(ctx, false)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(live))
	for _, e := range live {
		known[e.ID] = true
	}
	e, err := catalog.ParseEntry(row.Doc)
	if err != nil {
		return err
	}
	var kept, dropped []string
	for _, rel := range e.Related {
		if known[rel] {
			kept = append(kept, rel)
		} else {
			dropped = append(dropped, rel)
		}
	}
	f := catalog.Field{Key: "related", Value: kept}
	if len(kept) == 0 {
		f = catalog.Field{Key: "related", Remove: true}
	}
	doc, err := catalog.Patch(row.Doc, f)
	if err != nil {
		return err
	}
	note := "restored without links to " + strings.Join(dropped, ", ")
	if reason != "" {
		note += ": " + reason
	}
	_, err = s.catalog.RestoreAs(ctx, id, doc, adminActor, note)
	return err
}
