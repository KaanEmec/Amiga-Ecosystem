package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"amigaconstellation/internal/store"
)

// ErrNotEmpty is returned by Import when the catalogue already has entries.
var ErrNotEmpty = errors.New("catalogue already has entries")

// ErrEmpty is returned by Export when nothing has been imported yet.
var ErrEmpty = errors.New("catalogue is empty")

// Catalog reads and writes catalogue content through the store and keeps
// the public catalogue cache in step with it.
type Catalog struct {
	store *store.Store
	cache *Cache
	now   func() time.Time
}

// New builds a Catalog on top of an open, migrated store.
func New(st *store.Store) *Catalog {
	c := &Catalog{store: st, now: time.Now}
	c.cache = NewCache(c.publicJSON)
	return c
}

// Cache returns the public catalogue cache.
func (c *Catalog) Cache() *Cache { return c.cache }

// ImportStats summarises what an import changed.
type ImportStats struct {
	Created   int
	Updated   int
	Restored  int
	Deleted   int
	Unchanged int
}

// Export returns the live catalogue (deleted entries excluded) in seed
// document order.
func (c *Catalog) Export(ctx context.Context) (*Document, error) {
	q := c.store.Q()
	meta, err := q.ListMeta(ctx)
	if err != nil {
		return nil, err
	}
	if meta[store.MetaMeta] == nil {
		return nil, ErrEmpty
	}
	rows, err := q.ListEntries(ctx, false)
	if err != nil {
		return nil, err
	}
	doc := &Document{
		Readme:  meta[store.MetaReadme],
		Meta:    meta[store.MetaMeta],
		Domains: meta[store.MetaDomains],
		Tours:   meta[store.MetaTours],
		Entries: make([]json.RawMessage, len(rows)),
	}
	for i, r := range rows {
		doc.Entries[i] = r.Doc
	}
	return doc, nil
}

// Import loads doc into an empty catalogue. It fails with ErrNotEmpty if
// any entry exists (deleted ones included).
func (c *Catalog) Import(ctx context.Context, doc *Document, actor string) (ImportStats, error) {
	return c.write(ctx, doc, actor, "import", true)
}

// ImportReplace validates doc and makes the catalogue match it in one
// transaction: changed entries get a new rev, entries missing from doc are
// soft-deleted, previously deleted entries present in doc are restored.
// Every change writes a revision row.
func (c *Catalog) ImportReplace(ctx context.Context, doc *Document, actor string) (ImportStats, error) {
	return c.write(ctx, doc, actor, "import --replace", false)
}

func (c *Catalog) write(ctx context.Context, doc *Document, actor, reason string, requireEmpty bool) (ImportStats, error) {
	if _, err := Validate(doc); err != nil {
		return ImportStats{}, err
	}
	var stats ImportStats
	err := c.store.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		stats, err = c.apply(ctx, store.NewQueries(tx), doc, actor, reason, requireEmpty)
		return err
	})
	if err != nil {
		return ImportStats{}, err
	}
	c.cache.Invalidate()
	return stats, nil
}

// apply writes a validated document. It must run inside a transaction.
func (c *Catalog) apply(ctx context.Context, q store.Queries, doc *Document, actor, reason string, requireEmpty bool) (ImportStats, error) {
	now := c.now().Unix()
	existing, err := q.ListEntries(ctx, true)
	if err != nil {
		return ImportStats{}, err
	}
	if requireEmpty && len(existing) > 0 {
		return ImportStats{}, fmt.Errorf("%w (%d)", ErrNotEmpty, len(existing))
	}
	byID := make(map[string]store.Entry, len(existing))
	for _, e := range existing {
		byID[e.ID] = e
	}

	var stats ImportStats
	seen := make(map[string]bool, len(doc.Entries))
	for i, raw := range doc.Entries {
		body, err := compact(raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("entry #%d: %w", i, err)
		}
		e, err := ParseEntry(body)
		if err != nil {
			return ImportStats{}, fmt.Errorf("entry #%d: %w", i, err)
		}
		seen[e.ID] = true
		ord := int64(i)

		old, ok := byID[e.ID]
		if !ok {
			stats.Created++
			if err := q.InsertEntry(ctx, store.Entry{ID: e.ID, Doc: body, Rev: 1, Ord: ord, CreatedAt: now, UpdatedAt: now}); err != nil {
				return ImportStats{}, err
			}
			if _, err := q.InsertRevision(ctx, store.Revision{EntryID: e.ID, Rev: 1, After: body, Actor: actor, Reason: reason, CreatedAt: now}); err != nil {
				return ImportStats{}, err
			}
			continue
		}

		deleted := old.DeletedAt != nil
		if !deleted && bytes.Equal(old.Doc, body) {
			stats.Unchanged++
			if old.Ord != ord {
				old.Ord = ord
				if err := q.UpdateEntry(ctx, old, old.Rev); err != nil {
					return ImportStats{}, err
				}
			}
			continue
		}

		var before json.RawMessage
		if deleted {
			stats.Restored++
		} else {
			stats.Updated++
			before = old.Doc
		}
		next := store.Entry{ID: e.ID, Doc: body, Rev: old.Rev + 1, Ord: ord, UpdatedAt: now}
		if err := q.UpdateEntry(ctx, next, old.Rev); err != nil {
			return ImportStats{}, err
		}
		if _, err := q.InsertRevision(ctx, store.Revision{EntryID: e.ID, Rev: next.Rev, Before: before, After: body, Actor: actor, Reason: reason, CreatedAt: now}); err != nil {
			return ImportStats{}, err
		}
	}

	for _, old := range existing {
		if seen[old.ID] || old.DeletedAt != nil {
			continue
		}
		stats.Deleted++
		next := old
		next.Rev = old.Rev + 1
		next.DeletedAt = &now
		next.UpdatedAt = now
		if err := q.UpdateEntry(ctx, next, old.Rev); err != nil {
			return ImportStats{}, err
		}
		if _, err := q.InsertRevision(ctx, store.Revision{EntryID: old.ID, Rev: next.Rev, Before: old.Doc, Actor: actor, Reason: reason, CreatedAt: now}); err != nil {
			return ImportStats{}, err
		}
	}

	sections := []struct {
		key string
		raw json.RawMessage
	}{
		{store.MetaReadme, doc.Readme},
		{store.MetaMeta, doc.Meta},
		{store.MetaDomains, doc.Domains},
		{store.MetaTours, doc.Tours},
	}
	for _, s := range sections {
		if len(s.raw) == 0 {
			if err := q.DeleteMeta(ctx, s.key); err != nil {
				return ImportStats{}, err
			}
			continue
		}
		body, err := compact(s.raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("%s: %w", s.key, err)
		}
		if err := q.PutMeta(ctx, s.key, body); err != nil {
			return ImportStats{}, err
		}
	}
	return stats, nil
}

// publicJSON renders the /api/catalog body: the export without _readme.
func (c *Catalog) publicJSON(ctx context.Context) ([]byte, error) {
	doc, err := c.Export(ctx)
	if err != nil {
		return nil, err
	}
	doc.Readme = nil
	var buf bytes.Buffer
	if err := doc.Encode(&buf, false); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
