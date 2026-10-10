package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"amigaconstellation/internal/store"
)

// Publication errors. Validation failures are *ValidationError.
var (
	ErrStale      = errors.New("entry changed since it was loaded")
	ErrNotFound   = errors.New("entry not found")
	ErrExists     = errors.New("entry id already exists")
	ErrDeleted    = errors.New("entry is removed")
	ErrNotDeleted = errors.New("entry is not removed")
)

// FeaturedWeight is the weight from which an entry is a featured landmark.
const FeaturedWeight = 5

// Moderation log actions written by publications.
const (
	ActionCreate         = "create"
	ActionUpdate         = "update"
	ActionRemove         = "remove"
	ActionRestore        = "restore"
	ActionRelatedCleanup = "related_cleanup"
	ActionTourCleanup    = "tour_cleanup"
)

// StaleError reports an optimistic-concurrency conflict. It matches
// ErrStale with errors.Is.
type StaleError struct {
	ID       string
	Expected int64 // rev the caller loaded
	Current  int64 // rev in the database
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("entry %q is at rev %d, not %d: %v", e.ID, e.Current, e.Expected, ErrStale)
}

// Is makes errors.Is(err, ErrStale) true.
func (e *StaleError) Is(target error) bool { return target == ErrStale }

// TxHook runs inside a publication transaction after the catalogue writes.
// Returning an error rolls the whole publication back. Suggestion review
// uses it to flip the suggestion status in the same transaction.
type TxHook func(ctx context.Context, tx *sql.Tx) error

// Publication describes a committed change.
type Publication struct {
	ID           string
	Rev          int64    // the entry's rev after the change
	Unchanged    bool     // Update with an identical document: nothing written
	Affected     []string // other entries rewritten by related cleanup
	ToursChanged []string // tours whose steps lost the removed entry
}

// Dependent is an entry that lists another one in related.
type Dependent struct {
	ID   string
	Name string
}

// TourStop is one guided-route step pointing at an entry.
type TourStop struct {
	TourID    string
	TourLabel string
	Step      int // 1-based position
}

// Dependencies is what removing an entry would touch.
type Dependencies struct {
	ID        string
	Name      string
	Weight    int
	Featured  bool        // weight >= FeaturedWeight: shown as a landmark plate
	RelatedBy []Dependent // live entries whose related lists ID
	TourStops []TourStop
}

// Empty reports whether removal touches nothing else.
func (d Dependencies) Empty() bool {
	return !d.Featured && len(d.RelatedBy) == 0 && len(d.TourStops) == 0
}

// Create publishes a new entry (rev 1) at the end of the document order.
func (c *Catalog) Create(ctx context.Context, doc json.RawMessage, actor, reason string, suggestionID *int64, hooks ...TxHook) (Publication, error) {
	body, e, err := normalizeEntry(doc)
	if err != nil {
		return Publication{}, err
	}
	return c.publish(ctx, hooks, func(p *pubTx) (Publication, error) {
		if i, ok := p.idx[e.ID]; ok {
			if p.rows[i].DeletedAt != nil {
				return Publication{}, fmt.Errorf("%q (removed; restore it instead): %w", e.ID, ErrExists)
			}
			return Publication{}, fmt.Errorf("%q: %w", e.ID, ErrExists)
		}
		ord := int64(-1)
		for _, r := range p.rows {
			ord = max(ord, r.Ord)
		}
		row := store.Entry{ID: e.ID, Doc: body, Rev: 1, Ord: ord + 1, CreatedAt: p.now, UpdatedAt: p.now}
		p.add(row)
		if err := p.validate(); err != nil {
			return Publication{}, err
		}
		if err := p.q.InsertEntry(ctx, row); err != nil {
			return Publication{}, err
		}
		if err := p.record(ctx, e.ID, 1, nil, body, actor, reason, suggestionID, ActionCreate, nil); err != nil {
			return Publication{}, err
		}
		return Publication{ID: e.ID, Rev: 1}, nil
	})
}

// Update replaces a live entry's document. expectedRev is the rev the
// caller edited; if the entry moved on since, it fails with *StaleError.
// The id inside doc must equal id (entries are never renamed).
func (c *Catalog) Update(ctx context.Context, id string, doc json.RawMessage, expectedRev int64, actor, reason string, suggestionID *int64, hooks ...TxHook) (Publication, error) {
	body, e, err := normalizeEntry(doc)
	if err != nil {
		return Publication{}, err
	}
	if e.ID != id {
		return Publication{}, &ValidationError{Problems: []string{fmt.Sprintf("id %q cannot be changed to %q", id, e.ID)}}
	}
	return c.publish(ctx, hooks, func(p *pubTx) (Publication, error) {
		old, err := p.live(id, expectedRev)
		if err != nil {
			return Publication{}, err
		}
		if bytes.Equal(old.Doc, body) {
			return Publication{ID: id, Rev: old.Rev, Unchanged: true}, nil
		}
		next := old
		next.Doc, next.Rev, next.UpdatedAt = body, old.Rev+1, p.now
		p.rows[p.idx[id]] = next
		if err := p.validate(); err != nil {
			return Publication{}, err
		}
		if err := p.q.UpdateEntry(ctx, next, old.Rev); err != nil {
			return Publication{}, err
		}
		detail := map[string]any{"fields": changedKeys(old.Doc, body)}
		if err := p.record(ctx, id, next.Rev, old.Doc, body, actor, reason, suggestionID, ActionUpdate, detail); err != nil {
			return Publication{}, err
		}
		return Publication{ID: id, Rev: next.Rev}, nil
	})
}

// Remove soft-deletes a live entry and, in the same transaction, strips it
// from every other entry's related list (each gets a new revision) and from
// tour steps.
func (c *Catalog) Remove(ctx context.Context, id string, expectedRev int64, actor, reason string, suggestionID *int64, hooks ...TxHook) (Publication, error) {
	return c.publish(ctx, hooks, func(p *pubTx) (Publication, error) {
		old, err := p.live(id, expectedRev)
		if err != nil {
			return Publication{}, err
		}
		next := old
		next.Rev, next.DeletedAt, next.UpdatedAt = old.Rev+1, &p.now, p.now
		p.rows[p.idx[id]] = next

		type cleanup struct{ old, next store.Entry }
		var cleaned []cleanup
		for i, r := range p.rows {
			if r.DeletedAt != nil || r.ID == id {
				continue
			}
			doc, changed, err := dropRelated(r.Doc, id)
			if err != nil {
				return Publication{}, fmt.Errorf("entry %q: %w", r.ID, err)
			}
			if !changed {
				continue
			}
			n := r
			n.Doc, n.Rev, n.UpdatedAt = doc, r.Rev+1, p.now
			p.rows[i] = n
			cleaned = append(cleaned, cleanup{r, n})
		}
		tours, tourIDs, err := dropTourSteps(p.tours, id)
		if err != nil {
			return Publication{}, err
		}
		p.tours = tours
		if err := p.validate(); err != nil {
			return Publication{}, err
		}

		pub := Publication{ID: id, Rev: next.Rev, ToursChanged: tourIDs}
		if err := p.q.UpdateEntry(ctx, next, old.Rev); err != nil {
			return Publication{}, err
		}
		for _, cl := range cleaned {
			pub.Affected = append(pub.Affected, cl.next.ID)
		}
		detail := map[string]any{"related_cleaned": orEmpty(pub.Affected), "tours_changed": orEmpty(tourIDs)}
		if err := p.record(ctx, id, next.Rev, old.Doc, nil, actor, reason, suggestionID, ActionRemove, detail); err != nil {
			return Publication{}, err
		}
		cleanupReason := fmt.Sprintf("related link to removed %q dropped", id)
		if reason != "" {
			cleanupReason += ": " + reason
		}
		for _, cl := range cleaned {
			if err := p.q.UpdateEntry(ctx, cl.next, cl.old.Rev); err != nil {
				return Publication{}, err
			}
			if err := p.record(ctx, cl.next.ID, cl.next.Rev, cl.old.Doc, cl.next.Doc, actor, cleanupReason, suggestionID,
				ActionRelatedCleanup, map[string]any{"removed": id}); err != nil {
				return Publication{}, err
			}
		}
		if len(tourIDs) > 0 {
			if err := p.q.PutMeta(ctx, store.MetaTours, p.tours); err != nil {
				return Publication{}, err
			}
			if err := p.log(ctx, id, suggestionID, ActionTourCleanup, map[string]any{"removed": id, "tours": tourIDs}); err != nil {
				return Publication{}, err
			}
		}
		return pub, nil
	})
}

// Restore brings a removed entry back with its last document as a new
// revision. It fails with *ValidationError when the document no longer
// validates, e.g. a related entry was removed meanwhile; the owner resolves
// that with RestoreAs. Related links and tour stops dropped by the removal
// are not re-added.
func (c *Catalog) Restore(ctx context.Context, id, actor, reason string, hooks ...TxHook) (Publication, error) {
	return c.restore(ctx, id, nil, actor, reason, hooks)
}

// RestoreAs restores a removed entry with a replacement document (same id),
// for resolving conflicts that block Restore.
func (c *Catalog) RestoreAs(ctx context.Context, id string, doc json.RawMessage, actor, reason string, hooks ...TxHook) (Publication, error) {
	body, e, err := normalizeEntry(doc)
	if err != nil {
		return Publication{}, err
	}
	if e.ID != id {
		return Publication{}, &ValidationError{Problems: []string{fmt.Sprintf("id %q cannot be changed to %q", id, e.ID)}}
	}
	return c.restore(ctx, id, body, actor, reason, hooks)
}

func (c *Catalog) restore(ctx context.Context, id string, body json.RawMessage, actor, reason string, hooks []TxHook) (Publication, error) {
	return c.publish(ctx, hooks, func(p *pubTx) (Publication, error) {
		i, ok := p.idx[id]
		if !ok {
			return Publication{}, fmt.Errorf("%q: %w", id, ErrNotFound)
		}
		old := p.rows[i]
		if old.DeletedAt == nil {
			return Publication{}, fmt.Errorf("%q: %w", id, ErrNotDeleted)
		}
		next := old
		if body != nil {
			next.Doc = body
		}
		next.Rev, next.DeletedAt, next.UpdatedAt = old.Rev+1, nil, p.now
		p.rows[i] = next
		if err := p.validate(); err != nil {
			return Publication{}, err
		}
		if err := p.q.UpdateEntry(ctx, next, old.Rev); err != nil {
			return Publication{}, err
		}
		if err := p.record(ctx, id, next.Rev, nil, next.Doc, actor, reason, nil, ActionRestore, nil); err != nil {
			return Publication{}, err
		}
		return Publication{ID: id, Rev: next.Rev}, nil
	})
}

// Dependencies lists what removing the live entry id would touch.
func (c *Catalog) Dependencies(ctx context.Context, id string) (Dependencies, error) {
	q := c.store.Q()
	rows, err := q.ListEntries(ctx, false)
	if err != nil {
		return Dependencies{}, err
	}
	d := Dependencies{ID: id}
	found := false
	for _, r := range rows {
		e, err := ParseEntry(r.Doc)
		if err != nil {
			return Dependencies{}, fmt.Errorf("entry %q: %w", r.ID, err)
		}
		if r.ID == id {
			found = true
			d.Name = e.Name
			if e.Weight != nil {
				d.Weight = *e.Weight
			}
			d.Featured = d.Weight >= FeaturedWeight
			continue
		}
		if slices.Contains(e.Related, id) {
			d.RelatedBy = append(d.RelatedBy, Dependent{ID: r.ID, Name: e.Name})
		}
	}
	if !found {
		return Dependencies{}, fmt.Errorf("%q: %w", id, ErrNotFound)
	}
	toursRaw, err := q.GetMeta(ctx, store.MetaTours)
	if err != nil {
		return Dependencies{}, err
	}
	var tours []struct {
		ID    string   `json:"id"`
		Label string   `json:"label"`
		Steps []idItem `json:"steps"`
	}
	if err := json.Unmarshal(toursRaw, &tours); err != nil {
		return Dependencies{}, fmt.Errorf("decoding tours: %w", err)
	}
	for _, t := range tours {
		for i, st := range t.Steps {
			if st.ID == id {
				d.TourStops = append(d.TourStops, TourStop{TourID: t.ID, TourLabel: t.Label, Step: i + 1})
			}
		}
	}
	return d, nil
}

// Vocab returns the live controlled vocabulary.
func (c *Catalog) Vocab(ctx context.Context) (Vocab, error) {
	meta, err := c.store.Q().GetMeta(ctx, store.MetaMeta)
	if err != nil {
		return Vocab{}, err
	}
	return ParseVocab(meta)
}

// pubTx is the in-memory view of the catalogue inside one publication.
type pubTx struct {
	q     store.Queries
	now   int64
	meta  map[string]json.RawMessage
	tours json.RawMessage
	rows  []store.Entry // all entries, document order
	idx   map[string]int
}

func (c *Catalog) publish(ctx context.Context, hooks []TxHook, fn func(*pubTx) (Publication, error)) (Publication, error) {
	var pub Publication
	err := c.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		meta, err := q.ListMeta(ctx)
		if err != nil {
			return err
		}
		rows, err := q.ListEntries(ctx, true)
		if err != nil {
			return err
		}
		p := &pubTx{q: q, now: c.now().Unix(), meta: meta, tours: meta[store.MetaTours], idx: make(map[string]int, len(rows))}
		for _, r := range rows {
			p.add(r)
		}
		if pub, err = fn(p); err != nil {
			return err
		}
		for _, h := range hooks {
			if err := h(ctx, tx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Publication{}, err
	}
	if !pub.Unchanged {
		c.cache.Invalidate()
	}
	return pub, nil
}

func (p *pubTx) add(r store.Entry) {
	p.idx[r.ID] = len(p.rows)
	p.rows = append(p.rows, r)
}

// live returns the live entry id, checking its rev.
func (p *pubTx) live(id string, expectedRev int64) (store.Entry, error) {
	i, ok := p.idx[id]
	if !ok {
		return store.Entry{}, fmt.Errorf("%q: %w", id, ErrNotFound)
	}
	r := p.rows[i]
	if r.DeletedAt != nil {
		return store.Entry{}, fmt.Errorf("%q: %w", id, ErrDeleted)
	}
	if r.Rev != expectedRev {
		return store.Entry{}, &StaleError{ID: id, Expected: expectedRev, Current: r.Rev}
	}
	return r, nil
}

// validate checks the whole catalogue as it would be after the change.
func (p *pubTx) validate() error {
	doc := &Document{Meta: p.meta[store.MetaMeta], Domains: p.meta[store.MetaDomains], Tours: p.tours}
	for _, r := range p.rows {
		if r.DeletedAt == nil {
			doc.Entries = append(doc.Entries, r.Doc)
		}
	}
	_, err := Validate(doc)
	return err
}

// record writes the revision row and the moderation event of one entry change.
func (p *pubTx) record(ctx context.Context, id string, rev int64, before, after json.RawMessage, actor, reason string, suggestionID *int64, action string, detail map[string]any) error {
	if _, err := p.q.InsertRevision(ctx, store.Revision{
		EntryID: id, Rev: rev, Before: before, After: after,
		Actor: actor, Reason: reason, SuggestionID: suggestionID, CreatedAt: p.now,
	}); err != nil {
		return err
	}
	if detail == nil {
		detail = map[string]any{}
	}
	detail["rev"] = rev
	detail["actor"] = actor
	if reason != "" {
		detail["reason"] = reason
	}
	return p.log(ctx, id, suggestionID, action, detail)
}

func (p *pubTx) log(ctx context.Context, id string, suggestionID *int64, action string, detail map[string]any) error {
	raw, err := marshalNoEscape(detail)
	if err != nil {
		return err
	}
	_, err = p.q.InsertModeration(ctx, store.ModerationEvent{
		SuggestionID: suggestionID, EntryID: &id, Action: action, Detail: raw, CreatedAt: p.now,
	})
	return err
}

// normalizeEntry compacts doc and checks it is a JSON object with an id.
func normalizeEntry(doc json.RawMessage) (json.RawMessage, Entry, error) {
	body, err := compact(doc)
	if err != nil {
		return nil, Entry{}, &ValidationError{Problems: []string{"entry is not valid JSON: " + err.Error()}}
	}
	if len(body) == 0 || body[0] != '{' {
		return nil, Entry{}, &ValidationError{Problems: []string{"entry must be a JSON object"}}
	}
	e, err := ParseEntry(body)
	if err != nil {
		return nil, Entry{}, &ValidationError{Problems: []string{err.Error()}}
	}
	if e.ID == "" {
		return nil, Entry{}, &ValidationError{Problems: []string{"id is required"}}
	}
	return body, e, nil
}

// dropRelated drops id from doc.related, removing the key when it empties.
func dropRelated(doc json.RawMessage, id string) (json.RawMessage, bool, error) {
	e, err := ParseEntry(doc)
	if err != nil {
		return nil, false, err
	}
	if !slices.Contains(e.Related, id) {
		return doc, false, nil
	}
	kept := slices.DeleteFunc(slices.Clone(e.Related), func(r string) bool { return r == id })
	f := Field{Key: "related", Value: kept}
	if len(kept) == 0 {
		f = Field{Key: "related", Remove: true}
	}
	out, err := Patch(doc, f)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// dropTourSteps drops steps pointing at id. It returns the tours document
// (unchanged bytes when nothing matched) and the ids of tours it changed.
func dropTourSteps(tours json.RawMessage, id string) (json.RawMessage, []string, error) {
	var list []json.RawMessage
	if err := json.Unmarshal(tours, &list); err != nil {
		return nil, nil, fmt.Errorf("decoding tours: %w", err)
	}
	var changed []string
	for i, raw := range list {
		var t struct {
			ID    string            `json:"id"`
			Steps []json.RawMessage `json:"steps"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, nil, fmt.Errorf("decoding tour #%d: %w", i, err)
		}
		kept := make([]json.RawMessage, 0, len(t.Steps))
		for _, st := range t.Steps {
			var s idItem
			if err := json.Unmarshal(st, &s); err != nil {
				return nil, nil, fmt.Errorf("decoding tour %q step: %w", t.ID, err)
			}
			if s.ID != id {
				kept = append(kept, st)
			}
		}
		if len(kept) == len(t.Steps) {
			continue
		}
		out, err := Patch(raw, Field{Key: "steps", Value: kept})
		if err != nil {
			return nil, nil, fmt.Errorf("tour %q: %w", t.ID, err)
		}
		list[i] = out
		changed = append(changed, t.ID)
	}
	if len(changed) == 0 {
		return tours, nil, nil
	}
	out, err := marshalNoEscape(list)
	if err != nil {
		return nil, nil, err
	}
	return out, changed, nil
}

// Field is one change applied by Patch: set Key to Value, or remove it.
type Field struct {
	Key    string
	Value  any
	Remove bool
}

// Patch applies fields to a JSON object, keeping the position of existing
// keys and the bytes of untouched values; new keys are appended in order.
func Patch(obj json.RawMessage, fields ...Field) (json.RawMessage, error) {
	kv, err := decodeObject(obj)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		i := slices.IndexFunc(kv, func(p objField) bool { return p.key == f.Key })
		if f.Remove {
			if i >= 0 {
				kv = slices.Delete(kv, i, i+1)
			}
			continue
		}
		val, err := marshalNoEscape(f.Value)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Key, err)
		}
		if i >= 0 {
			kv[i].val = val
		} else {
			kv = append(kv, objField{f.Key, val})
		}
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range kv {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := marshalNoEscape(p.key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(p.val)
	}
	buf.WriteByte('}')
	return compact(buf.Bytes())
}

type objField struct {
	key string
	val json.RawMessage
}

// decodeObject splits a JSON object into its members in source order.
func decodeObject(raw json.RawMessage) ([]objField, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("decoding object: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("decoding object: not a JSON object")
	}
	var out []objField
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("decoding object: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("decoding object: key is not a string")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("decoding object member %q: %w", key, err)
		}
		out = append(out, objField{key, val})
	}
	return out, nil
}

// changedKeys lists the top-level keys whose values differ between a and b.
func changedKeys(a, b json.RawMessage) []string {
	ka, errA := decodeObject(a)
	kb, errB := decodeObject(b)
	if errA != nil || errB != nil {
		return []string{}
	}
	va := make(map[string]string, len(ka))
	for _, p := range ka {
		va[p.key] = string(p.val)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, p := range kb {
		seen[p.key] = true
		if old, ok := va[p.key]; !ok || old != string(p.val) {
			out = append(out, p.key)
		}
	}
	for _, p := range ka {
		if !seen[p.key] {
			out = append(out, p.key)
		}
	}
	return out
}

// marshalNoEscape encodes v compactly without HTML escaping, matching the
// seed file's encoding.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encoding JSON: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
