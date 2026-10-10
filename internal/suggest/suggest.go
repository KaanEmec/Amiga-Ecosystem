// Package suggest implements public proposals (new entry, change, removal,
// image), +1 votes, duplicate hints and the per-browser / per-IP rate
// limits. It validates proposals against the live catalogue vocabulary and
// stores them as pending; decisions belong to the owner panel.
package suggest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
)

// Proposal types.
const (
	TypeNew    = "new"
	TypeChange = "change"
	TypeRemove = "remove"
	TypeImage  = "image"
)

// Types lists the proposal types in display order.
var Types = []string{TypeNew, TypeChange, TypeRemove, TypeImage}

// Errors returned by the service. Validation problems are *ValidationError.
var (
	ErrNotFound          = errors.New("suggestion not found")
	ErrClosed            = errors.New("this suggestion has been resolved; its votes are final")
	ErrRateLimited       = errors.New("too many requests from this browser or network; please try again later")
	ErrSubmissionsPaused = errors.New("new suggestions are paused for the moment; please try again later")
	ErrVotingPaused      = errors.New("voting is paused for the moment; please try again later")
	ErrNoVisitor         = errors.New("missing browser id")
)

// ValidationError lists what is wrong with a proposal, in form order.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid proposal: " + strings.Join(e.Problems, "; ")
}

// Limits on visitor input.
const (
	maxName    = 120
	maxShort   = 200 // by, version, credit, contact, tag
	maxText    = 1200
	maxReason  = 2000
	maxSummary = 300
	maxURL     = 1000
	maxSources = 10
	maxList    = 20
)

// Fields are the entry fields a visitor may propose. nil means "not
// supplied". Owner-controlled fields (weight, unverified, image*) do not
// exist here, so they are dropped when decoding.
type Fields struct {
	ID       *string  `json:"id,omitempty"`
	Name     *string  `json:"name,omitempty"`
	Year     *int     `json:"year,omitempty"`
	Category *string  `json:"category,omitempty"`
	Kind     *string  `json:"kind,omitempty"`
	Status   *string  `json:"status,omitempty"`
	Arch     []string `json:"arch,omitempty"`
	By       *string  `json:"by,omitempty"`
	What     *string  `json:"what,omitempty"`
	Why      *string  `json:"why,omitempty"`
	Related  []string `json:"related,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	URL      *string  `json:"url,omitempty"`
	Version  *string  `json:"version,omitempty"`
}

// FieldOrder is the key order of proposed entry objects (the seed's order).
var FieldOrder = []string{"id", "name", "year", "category", "kind", "status", "arch", "by", "what", "why", "related", "tags", "url", "version"}

// FieldLabels are human names for entry fields.
var FieldLabels = map[string]string{
	"id": "Id", "name": "Name", "year": "Year", "category": "Category", "kind": "Kind",
	"status": "Status", "arch": "Architectures", "by": "Made by", "what": "What it is",
	"why": "Why it matters", "related": "Descends from", "tags": "Tags", "url": "Official site",
	"version": "Version", "weight": "Importance", "image": "Image", "imageCredit": "Image credit",
}

// supplied returns the supplied fields, trimmed and de-duplicated.
func (f Fields) supplied() map[string]any {
	m := map[string]any{}
	str := func(k string, p *string) {
		if p != nil {
			m[k] = strings.TrimSpace(*p)
		}
	}
	list := func(k string, l []string) {
		if l == nil {
			return
		}
		out := []string{}
		seen := map[string]bool{}
		for _, v := range l {
			v = strings.TrimSpace(v)
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		m[k] = out
	}
	str("id", f.ID)
	str("name", f.Name)
	if f.Year != nil {
		m["year"] = *f.Year
	}
	str("category", f.Category)
	str("kind", f.Kind)
	str("status", f.Status)
	list("arch", f.Arch)
	str("by", f.By)
	str("what", f.What)
	str("why", f.Why)
	list("related", f.Related)
	list("tags", f.Tags)
	str("url", f.URL)
	str("version", f.Version)
	return m
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []string:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}

// Proposal is what a visitor submits.
type Proposal struct {
	Type            string   `json:"type"`
	EntryID         string   `json:"entry_id"`
	Fields          Fields   `json:"fields"`
	ImageURL        string   `json:"image_url"`
	ImageCredit     string   `json:"image_credit"`
	ImageSourcePage string   `json:"image_source_page"`
	Summary         string   `json:"summary"`
	Reason          string   `json:"reason"`
	Sources         []string `json:"sources"`
	Contact         string   `json:"contact"`
}

// Visitor identifies who acts: the signed browser id and the salted IP hash.
type Visitor struct {
	ID     string
	IPHash string
}

// Service is the suggestions feature.
type Service struct {
	store   *store.Store
	catalog *catalog.Catalog
	cfg     config.Config
	now     func() time.Time

	mu  sync.Mutex
	idx *index
}

// New builds the service. clock is usually time.Now.
func New(st *store.Store, cat *catalog.Catalog, cfg config.Config, clock func() time.Time) *Service {
	return &Service{store: st, catalog: cat, cfg: cfg, now: clock}
}

// Options returns the live vocabulary for forms and labels.
func (s *Service) Options(ctx context.Context) (Options, error) {
	idx, err := s.index(ctx)
	if err != nil {
		return Options{}, err
	}
	return idx.options, nil
}

// Entry returns a live entry's reference and its full document.
func (s *Service) Entry(ctx context.Context, id string) (EntryRef, json.RawMessage, error) {
	idx, err := s.index(ctx)
	if err != nil {
		return EntryRef{}, nil, err
	}
	ref, ok := idx.entry(id)
	if !ok {
		return EntryRef{}, nil, fmt.Errorf("entry %q: %w", id, store.ErrNotFound)
	}
	e, err := s.store.Q().GetEntry(ctx, id)
	if err != nil {
		return EntryRef{}, nil, err
	}
	return ref, e.Doc, nil
}

// Paused reports the submission and voting pause switches.
func (s *Service) Paused(ctx context.Context) (submissions, voting bool, err error) {
	q := s.store.Q()
	if submissions, err = q.SettingBool(ctx, store.SettingSubmissionsPaused); err != nil {
		return false, false, err
	}
	if voting, err = q.SettingBool(ctx, store.SettingVotingPaused); err != nil {
		return false, false, err
	}
	return submissions, voting, nil
}

// Create validates a proposal and stores it as pending. It enforces the
// submission pause and the per-browser and per-IP daily limits.
func (s *Service) Create(ctx context.Context, p Proposal, v Visitor) (int64, error) {
	if v.ID == "" {
		return 0, ErrNoVisitor
	}
	if paused, err := s.store.Q().SettingBool(ctx, store.SettingSubmissionsPaused); err != nil {
		return 0, err
	} else if paused {
		return 0, ErrSubmissionsPaused
	}
	sg, err := s.build(ctx, p)
	if err != nil {
		return 0, err
	}
	now := s.now()
	sg.VoterID, sg.IPHash, sg.CreatedAt = v.ID, v.IPHash, now.Unix()

	var id int64
	err = s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		lim := rateLimit{kind: "suggest", window: 24 * time.Hour, perVisitor: s.cfg.RateSuggestPerDay, perIP: s.cfg.RateSuggestPerDayIP}
		if err := lim.check(ctx, q, v, now); err != nil {
			return err
		}
		if err := lim.record(ctx, q, v, now); err != nil {
			return err
		}
		var err error
		id, err = q.InsertSuggestion(ctx, sg)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("creating suggestion: %w", err)
	}
	return id, nil
}

// problems collects validation messages.
type problems []string

func (p *problems) add(format string, args ...any) { *p = append(*p, fmt.Sprintf(format, args...)) }

func (p *problems) maxLen(label, v string, n int) {
	if utf8.RuneCountInString(v) > n {
		p.add("%s is too long (at most %d characters)", label, n)
	}
}

func (p problems) err() error {
	if len(p) == 0 {
		return nil
	}
	return &ValidationError{Problems: p}
}

// httpURL reports whether s is an absolute http(s) URL with a host.
func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// build validates p and turns it into a pending suggestion row (without
// visitor columns).
func (s *Service) build(ctx context.Context, p Proposal) (store.Suggestion, error) {
	idx, err := s.index(ctx)
	if err != nil {
		return store.Suggestion{}, err
	}
	var probs problems
	reason := strings.TrimSpace(p.Reason)
	summary := strings.TrimSpace(p.Summary)
	contact := strings.TrimSpace(p.Contact)
	probs.maxLen("The reason", reason, maxReason)
	probs.maxLen("The summary", summary, maxSummary)
	probs.maxLen("The contact", contact, maxShort)

	sources := []string{}
	for _, src := range p.Sources {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}
		if len(src) > maxURL || !httpURL(src) {
			probs.add("Source %q must be a full http(s) URL", truncate(src, 80))
			continue
		}
		sources = append(sources, src)
	}
	if len(sources) > maxSources {
		probs.add("At most %d sources, please", maxSources)
	}

	sg := store.Suggestion{Type: p.Type, Reason: reason, Contact: contact}
	var payload json.RawMessage
	var autoSummary string
	switch p.Type {
	case TypeNew:
		payload, sg.Title, autoSummary = s.buildNew(ctx, idx, p.Fields, &probs)
	case TypeChange, TypeRemove, TypeImage:
		entryID := strings.TrimSpace(p.EntryID)
		ref, ok := idx.entry(entryID)
		if !ok {
			probs.add("Choose an existing entry")
			return store.Suggestion{}, probs.err()
		}
		e, err := s.store.Q().GetEntry(ctx, entryID)
		if err != nil {
			return store.Suggestion{}, err
		}
		rev := e.Rev
		sg.EntryID, sg.Snapshot, sg.BaseRev, sg.Title = entryID, e.Doc, &rev, ref.Name
		switch p.Type {
		case TypeChange:
			payload, autoSummary = buildChange(idx, e.Doc, p.Fields, &probs)
		case TypeRemove:
			payload = json.RawMessage("{}")
			if reason == "" {
				probs.add("Explain why the entry should be removed")
			}
			autoSummary = "Remove: " + truncate(reason, 140)
		case TypeImage:
			payload, autoSummary = buildImage(p, &probs)
			if reason == "" {
				probs.add("Explain why this image suits the entry")
			}
		}
	default:
		probs.add("Choose what kind of suggestion this is")
	}
	if err := probs.err(); err != nil {
		return store.Suggestion{}, err
	}
	if summary == "" {
		summary = truncate(autoSummary, maxSummary)
	}
	srcJSON, err := json.Marshal(sources)
	if err != nil {
		return store.Suggestion{}, fmt.Errorf("encoding sources: %w", err)
	}
	sg.Summary, sg.Payload, sg.Sources = summary, payload, srcJSON
	return sg, nil
}

func (s *Service) buildNew(ctx context.Context, idx *index, f Fields, probs *problems) (json.RawMessage, string, string) {
	vals := f.supplied()
	name, _ := vals["name"].(string)
	probs.maxLen("The name", name, maxName)
	id, _ := vals["id"].(string)
	if id == "" {
		id = slugify(name)
	}
	vals["id"] = id
	checkFieldLimits(vals, probs)
	if id != "" {
		if _, err := s.store.Q().GetEntry(ctx, id); err == nil {
			probs.add("An entry with the id %q already exists; it may already be in the catalogue (otherwise choose another id)", id)
		} else if !errors.Is(err, store.ErrNotFound) {
			probs.add("Could not check the id, please retry")
		}
	}
	doc := map[string]any{}
	for k, v := range vals {
		if !isEmpty(v) {
			doc[k] = v
		}
	}
	checkEntry(idx, doc, id, probs)
	payload, err := orderedJSON(doc)
	if err != nil {
		probs.add("Could not encode the entry")
	}
	what, _ := vals["what"].(string)
	return payload, name, truncate(what, 160)
}

func buildChange(idx *index, snapshot json.RawMessage, f Fields, probs *problems) (json.RawMessage, string) {
	var snap map[string]json.RawMessage
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		probs.add("The current entry could not be read")
		return nil, ""
	}
	id := ""
	_ = json.Unmarshal(snap["id"], &id)
	vals := f.supplied()
	delete(vals, "id")
	checkFieldLimits(vals, probs)

	changed := map[string]any{}
	var labels []string
	for _, k := range FieldOrder {
		v, ok := vals[k]
		if !ok {
			continue
		}
		var cur any
		if raw, has := snap[k]; has {
			_ = json.Unmarshal(raw, &cur)
		}
		if isEmpty(v) {
			if !isEmpty(cur) {
				changed[k] = nil
				labels = append(labels, strings.ToLower(FieldLabels[k]))
			}
			continue
		}
		if !reflect.DeepEqual(roundTrip(v), cur) {
			changed[k] = v
			labels = append(labels, strings.ToLower(FieldLabels[k]))
		}
	}
	if len(changed) == 0 {
		probs.add("Nothing was changed; edit at least one field")
		return nil, ""
	}
	merged := map[string]any{}
	for k, raw := range snap {
		var v any
		_ = json.Unmarshal(raw, &v)
		merged[k] = v
	}
	for k, v := range changed {
		if v == nil {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	checkEntry(idx, merged, id, probs)
	payload, err := orderedJSON(changed)
	if err != nil {
		probs.add("Could not encode the change")
	}
	return payload, "Change " + strings.Join(labels, ", ")
}

func buildImage(p Proposal, probs *problems) (json.RawMessage, string) {
	img := strings.TrimSpace(p.ImageURL)
	credit := strings.TrimSpace(p.ImageCredit)
	page := strings.TrimSpace(p.ImageSourcePage)
	host := ""
	if img == "" || len(img) > maxURL || !httpURL(img) {
		probs.add("The image URL must be a full http(s) URL")
	} else if u, err := url.Parse(img); err == nil {
		host = u.Hostname()
	}
	if credit == "" {
		probs.add("Credit the image (author, licence or source)")
	}
	probs.maxLen("The image credit", credit, maxShort)
	if page != "" && (len(page) > maxURL || !httpURL(page)) {
		probs.add("The source page must be a full http(s) URL")
	}
	m := map[string]any{"image_url": img, "credit": credit}
	if page != "" {
		m["source_page"] = page
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(m)
	return json.RawMessage(bytes.TrimSpace(buf.Bytes())), "New image from " + host
}

// checkFieldLimits bounds free-text and list sizes.
func checkFieldLimits(vals map[string]any, probs *problems) {
	for _, k := range []string{"by", "version"} {
		if v, _ := vals[k].(string); v != "" {
			probs.maxLen(FieldLabels[k], v, maxShort)
		}
	}
	for _, k := range []string{"what", "why"} {
		if v, _ := vals[k].(string); v != "" {
			probs.maxLen(FieldLabels[k], v, maxText)
		}
	}
	if v, _ := vals["url"].(string); v != "" && (len(v) > maxURL || !httpURL(v)) {
		probs.add("The official site must be a full http(s) URL")
	}
	for _, k := range []string{"arch", "related", "tags"} {
		if l, _ := vals[k].([]string); len(l) > maxList {
			probs.add("%s: at most %d items", FieldLabels[k], maxList)
		}
	}
	if tags, _ := vals["tags"].([]string); len(tags) > 0 {
		for _, t := range tags {
			probs.maxLen("Each tag", t, 40)
		}
	}
}

// checkEntry validates a whole proposed entry against the live vocabulary.
// Importance is owner-controlled, so a placeholder satisfies that check.
func checkEntry(idx *index, doc map[string]any, id string, probs *problems) {
	raw, err := json.Marshal(doc)
	if err != nil {
		probs.add("Could not encode the entry")
		return
	}
	e, err := catalog.ParseEntry(raw)
	if err != nil {
		probs.add("The entry fields have the wrong shape")
		return
	}
	placeholder := 1
	e.Weight = &placeholder
	for _, msg := range catalog.CheckEntry(e, idx.vocab, idx.known()) {
		probs.add("%s", capitalize(msg))
	}
	for _, r := range e.Related {
		if r == id && id != "" {
			probs.add("An entry cannot descend from itself")
		}
	}
}

// orderedJSON encodes an object with keys in FieldOrder (unknown keys
// last), null for nil values, and no HTML escaping.
func orderedJSON(m map[string]any) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	write := func(k string, v any) error {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(k); err != nil {
			return err
		}
		if err := enc.Encode(v); err != nil {
			return err
		}
		parts := bytes.SplitN(b.Bytes(), []byte("\n"), 2)
		buf.Write(parts[0])
		buf.WriteByte(':')
		buf.Write(bytes.TrimSpace(parts[1]))
		return nil
	}
	for _, k := range FieldOrder {
		if v, ok := m[k]; ok {
			if err := write(k, v); err != nil {
				return nil, fmt.Errorf("encoding %s: %w", k, err)
			}
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// roundTrip converts a Go value to its generic JSON form for comparison.
func roundTrip(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
