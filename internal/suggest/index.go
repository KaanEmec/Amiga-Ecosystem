package suggest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"amigaconstellation/internal/catalog"
)

// Option is one vocabulary item with its label.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Group is a category group with its categories.
type Group struct {
	Label string   `json:"label"`
	Cats  []Option `json:"cats"`
}

// Options is the live vocabulary used by the form.
type Options struct {
	Groups   []Group
	Kinds    []Option
	Statuses []Option
	Archs    []Option
}

// Label returns the label of id in opts, or id itself.
func Label(opts []Option, id string) string {
	for _, o := range opts {
		if o.ID == id {
			return o.Label
		}
	}
	return id
}

// CategoryLabel returns the label of a category id, or the id itself.
func (o Options) CategoryLabel(id string) string {
	for _, g := range o.Groups {
		for _, c := range g.Cats {
			if c.ID == id {
				return c.Label
			}
		}
	}
	return id
}

// EntryRef is a live catalogue entry, as used for hints and the picker.
type EntryRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Year     int    `json:"year"`
	Category string `json:"category"`
	By       string `json:"by,omitempty"`

	norm   string
	tokens []string
}

// index is the live catalogue (vocab and entry names) parsed from the
// public catalogue cache; it is rebuilt when the cache ETag changes.
type index struct {
	etag    string
	vocab   catalog.Vocab
	options Options
	entries []EntryRef
	byID    map[string]int
}

func (s *Service) index(ctx context.Context) (*index, error) {
	body, etag, err := s.catalog.Cache().Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading catalogue: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx != nil && s.idx.etag == etag {
		return s.idx, nil
	}
	idx, err := buildIndex(body)
	if err != nil {
		return nil, err
	}
	idx.etag = etag
	s.idx = idx
	return idx, nil
}

func buildIndex(body []byte) (*index, error) {
	var doc struct {
		Meta    json.RawMessage `json:"meta"`
		Entries []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Year     int    `json:"year"`
			Category string `json:"category"`
			By       string `json:"by"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parsing catalogue: %w", err)
	}
	vocab, err := catalog.ParseVocab(doc.Meta)
	if err != nil {
		return nil, fmt.Errorf("catalogue vocabulary: %w", err)
	}
	var meta struct {
		Groups []struct {
			Label string   `json:"label"`
			Cats  []Option `json:"cats"`
		} `json:"groups"`
		Kinds         []Option `json:"kinds"`
		Statuses      []Option `json:"statuses"`
		Architectures []Option `json:"architectures"`
	}
	if err := json.Unmarshal(doc.Meta, &meta); err != nil {
		return nil, fmt.Errorf("catalogue meta: %w", err)
	}
	idx := &index{
		vocab: vocab,
		options: Options{
			Kinds:    meta.Kinds,
			Statuses: meta.Statuses,
			Archs:    meta.Architectures,
		},
		entries: make([]EntryRef, len(doc.Entries)),
		byID:    make(map[string]int, len(doc.Entries)),
	}
	for _, g := range meta.Groups {
		idx.options.Groups = append(idx.options.Groups, Group{Label: g.Label, Cats: g.Cats})
	}
	for i, e := range doc.Entries {
		ref := EntryRef{ID: e.ID, Name: e.Name, Year: e.Year, Category: e.Category, By: e.By}
		ref.norm = normalize(e.Name)
		ref.tokens = strings.Fields(ref.norm)
		idx.entries[i] = ref
		idx.byID[e.ID] = i
	}
	return idx, nil
}

func (idx *index) known() map[string]bool {
	out := make(map[string]bool, len(idx.byID))
	for id := range idx.byID {
		out[id] = true
	}
	return out
}

func (idx *index) entry(id string) (EntryRef, bool) {
	i, ok := idx.byID[id]
	if !ok {
		return EntryRef{}, false
	}
	return idx.entries[i], true
}

// normalize lowercases and turns every run of non-alphanumerics into one
// space.
func normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// slugify derives an entry id from a name.
func slugify(name string) string {
	slug := strings.ReplaceAll(normalize(name), " ", "-")
	var b strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	slug = strings.Trim(b.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-")
	}
	return slug
}

// similarity scores how well query matches a name (both normalized):
// 0 means no match.
func similarity(q string, qTokens []string, name string, nameTokens []string) int {
	switch {
	case q == "" || name == "":
		return 0
	case q == name:
		return 100
	case strings.HasPrefix(name, q):
		return 80
	case strings.Contains(name, q) || (len(name) >= 4 && strings.Contains(q, name)):
		return 60
	}
	var significant, hits int
	for _, t := range qTokens {
		if len(t) < 2 {
			continue
		}
		significant++
		for _, n := range nameTokens {
			if n == t || (len(t) >= 3 && strings.HasPrefix(n, t)) {
				hits++
				break
			}
		}
	}
	switch {
	case significant == 0 || hits == 0:
		return 0
	case hits == significant:
		return 50
	case hits*2 >= significant && hits >= 1 && significant >= 2:
		return 30
	}
	return 0
}

// EntryNames maps the given ids to live entry names; unknown ids are
// absent.
func (s *Service) EntryNames(ctx context.Context, ids []string) (map[string]string, error) {
	idx, err := s.index(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if ref, ok := idx.entry(id); ok {
			out[id] = ref.Name
		}
	}
	return out, nil
}
