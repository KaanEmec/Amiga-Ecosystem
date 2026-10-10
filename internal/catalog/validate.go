package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// maxReportedProblems caps how many problems one validation error lists.
const maxReportedProblems = 25

// idPattern is the entry id slug: lowercase letters, digits and inner hyphens.
var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Entry holds the fields of an entry that validation and the server need.
// The full object (with every other content field) stays in the raw JSON.
type Entry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Year     *int     `json:"year"`
	Category string   `json:"category"`
	Kind     string   `json:"kind"`
	Status   string   `json:"status"`
	Arch     []string `json:"arch"`
	Weight   *int     `json:"weight"`
	What     string   `json:"what"`
	Why      string   `json:"why"`
	Related  []string `json:"related"`
	Image    *string  `json:"image"`
	URL      *string  `json:"url"`
}

// Vocab is the controlled vocabulary from meta.
type Vocab struct {
	Categories map[string]bool // meta.groups[].cats[].id
	Kinds      map[string]bool // meta.kinds[].id
	Statuses   map[string]bool // meta.statuses[].id
	Archs      map[string]bool // meta.architectures[].id
}

type idItem struct {
	ID string `json:"id"`
}

// ParseVocab extracts the vocabulary from the meta document.
func ParseVocab(meta json.RawMessage) (Vocab, error) {
	var m struct {
		Groups []struct {
			ID   string   `json:"id"`
			Cats []idItem `json:"cats"`
		} `json:"groups"`
		Kinds         []idItem `json:"kinds"`
		Statuses      []idItem `json:"statuses"`
		Architectures []idItem `json:"architectures"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		return Vocab{}, fmt.Errorf("meta: %w", err)
	}
	set := func(items []idItem) map[string]bool {
		out := make(map[string]bool, len(items))
		for _, it := range items {
			if it.ID != "" {
				out[it.ID] = true
			}
		}
		return out
	}
	v := Vocab{
		Categories: map[string]bool{},
		Kinds:      set(m.Kinds),
		Statuses:   set(m.Statuses),
		Archs:      set(m.Architectures),
	}
	for _, g := range m.Groups {
		for _, c := range g.Cats {
			if c.ID != "" {
				v.Categories[c.ID] = true
			}
		}
	}
	switch {
	case len(v.Categories) == 0:
		return Vocab{}, errors.New("meta.groups defines no categories")
	case len(v.Kinds) == 0:
		return Vocab{}, errors.New("meta.kinds is empty")
	case len(v.Statuses) == 0:
		return Vocab{}, errors.New("meta.statuses is empty")
	case len(v.Archs) == 0:
		return Vocab{}, errors.New("meta.architectures is empty")
	}
	return v, nil
}

// ParseEntry decodes the validated fields of one raw entry.
func ParseEntry(raw json.RawMessage) (Entry, error) {
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return Entry{}, fmt.Errorf("decoding entry: %w", err)
	}
	return e, nil
}

// CheckEntry validates one entry against the vocabulary. Related ids are
// checked against known when it is non-nil.
func CheckEntry(e Entry, v Vocab, known map[string]bool) []string {
	var p []string
	add := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }

	if e.ID == "" {
		add("id is required")
	} else if !idPattern.MatchString(e.ID) || len(e.ID) > 64 {
		add("id %q must be a lowercase slug (a-z, 0-9, inner hyphens, max 64 chars)", e.ID)
	}
	if strings.TrimSpace(e.Name) == "" {
		add("name is required")
	}
	if e.Year == nil {
		add("year is required")
	} else if *e.Year < 1900 || *e.Year > 2100 {
		add("year %d is out of range", *e.Year)
	}
	switch {
	case e.Category == "":
		add("category is required")
	case !v.Categories[e.Category]:
		add("unknown category %q", e.Category)
	}
	switch {
	case e.Kind == "":
		add("kind is required")
	case !v.Kinds[e.Kind]:
		add("unknown kind %q", e.Kind)
	}
	switch {
	case e.Status == "":
		add("status is required")
	case !v.Statuses[e.Status]:
		add("unknown status %q", e.Status)
	}
	if len(e.Arch) == 0 {
		add("arch needs at least one architecture")
	}
	for _, a := range e.Arch {
		if !v.Archs[a] {
			add("unknown arch %q", a)
		}
	}
	if e.Weight == nil {
		add("weight is required")
	} else if *e.Weight < 1 || *e.Weight > 5 {
		add("weight %d must be 1-5", *e.Weight)
	}
	if strings.TrimSpace(e.What) == "" {
		add("what is required")
	}
	if strings.TrimSpace(e.Why) == "" {
		add("why is required")
	}
	if known != nil {
		for _, r := range e.Related {
			if !known[r] {
				add("related %q does not exist", r)
			}
		}
	}
	if e.Image != nil && *e.Image != "" && !validImageRef(*e.Image) {
		add("image %q must be an http(s) URL, a data: URI or a relative path (./ or images/)", *e.Image)
	}
	if e.URL != nil && *e.URL != "" && !httpURL(*e.URL) {
		add("url %q must be an absolute http(s) URL", *e.URL)
	}
	return p
}

// httpURL reports whether s is an absolute http(s) URL with a host and no
// credentials (the entry's "url" is rendered as a link).
func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// validImageRef mirrors build.py: URL, data: URI or relative path. Relative
// paths may not climb out of their directory.
func validImageRef(s string) bool {
	switch {
	case strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "data:"):
		return true
	case strings.HasPrefix(s, "./"), strings.HasPrefix(s, "images/"):
		for _, seg := range strings.Split(s, "/") {
			if seg == ".." {
				return false
			}
		}
		return !strings.ContainsAny(s, "\\\x00")
	}
	return false
}

// ValidationError lists every problem found in a document.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	shown := e.Problems
	more := ""
	if len(shown) > maxReportedProblems {
		more = fmt.Sprintf("\n  ... and %d more", len(shown)-maxReportedProblems)
		shown = shown[:maxReportedProblems]
	}
	return fmt.Sprintf("invalid catalogue (%d problems):\n  %s%s", len(e.Problems), strings.Join(shown, "\n  "), more)
}

// Validate checks the whole document: required sections, vocabulary,
// unique ids, resolvable related links and tour steps. It returns the
// parsed entries in document order.
func Validate(doc *Document) ([]Entry, error) {
	var p []string
	sections := []struct {
		name string
		raw  json.RawMessage
	}{{"meta", doc.Meta}, {"domains", doc.Domains}, {"tours", doc.Tours}}
	for _, sec := range sections {
		if len(sec.raw) == 0 || string(sec.raw) == "null" {
			p = append(p, fmt.Sprintf("missing %q section", sec.name))
		}
	}
	if len(p) > 0 {
		return nil, &ValidationError{Problems: p}
	}
	vocab, err := ParseVocab(doc.Meta)
	if err != nil {
		return nil, &ValidationError{Problems: []string{err.Error()}}
	}

	entries := make([]Entry, len(doc.Entries))
	known := make(map[string]bool, len(doc.Entries))
	for i, raw := range doc.Entries {
		e, err := ParseEntry(raw)
		if err != nil {
			p = append(p, fmt.Sprintf("entry #%d: %v", i, err))
			continue
		}
		if known[e.ID] {
			p = append(p, fmt.Sprintf("entry #%d: duplicate id %q", i, e.ID))
		}
		known[e.ID] = true
		entries[i] = e
	}
	for i, e := range entries {
		label := fmt.Sprintf("entry #%d", i)
		if e.ID != "" {
			label = fmt.Sprintf("entry %q", e.ID)
		}
		for _, msg := range CheckEntry(e, vocab, known) {
			p = append(p, label+": "+msg)
		}
	}

	var tours []struct {
		ID    string   `json:"id"`
		Steps []idItem `json:"steps"`
	}
	if err := json.Unmarshal(doc.Tours, &tours); err != nil {
		p = append(p, fmt.Sprintf("tours: %v", err))
	}
	for _, t := range tours {
		for _, st := range t.Steps {
			if !known[st.ID] {
				p = append(p, fmt.Sprintf("tour %q references unknown entry %q", t.ID, st.ID))
			}
		}
	}
	var domains []json.RawMessage
	if err := json.Unmarshal(doc.Domains, &domains); err != nil {
		p = append(p, fmt.Sprintf("domains: %v", err))
	}

	if len(p) > 0 {
		return nil, &ValidationError{Problems: p}
	}
	return entries, nil
}
