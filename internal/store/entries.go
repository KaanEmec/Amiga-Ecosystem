package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Entry is one row of the entries table.
type Entry struct {
	ID        string
	Doc       json.RawMessage
	Rev       int64
	Ord       int64
	DeletedAt *int64 // nil while the entry is live
	CreatedAt int64
	UpdatedAt int64
}

// Revision is one row of entry_revisions. Before is nil for a creation and
// After is nil for a deletion.
type Revision struct {
	ID           int64
	EntryID      string
	Rev          int64
	Before       json.RawMessage
	After        json.RawMessage
	Actor        string
	Reason       string
	SuggestionID *int64
	CreatedAt    int64
}

const entryCols = "id, doc, rev, ord, deleted_at, created_at, updated_at"

func scanEntry(sc interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	var doc string
	if err := sc.Scan(&e.ID, &doc, &e.Rev, &e.Ord, &e.DeletedAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return Entry{}, err
	}
	e.Doc = json.RawMessage(doc)
	return e, nil
}

// CountEntries counts entry rows, including deleted ones.
func (q Queries) CountEntries(ctx context.Context) (int, error) {
	var n int
	if err := q.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&n); err != nil {
		return 0, fmt.Errorf("counting entries: %w", err)
	}
	return n, nil
}

// ListEntries returns entries in document order. Deleted entries are
// included only when includeDeleted is true.
func (q Queries) ListEntries(ctx context.Context, includeDeleted bool) ([]Entry, error) {
	query := "SELECT " + entryCols + " FROM entries"
	if !includeDeleted {
		query += " WHERE deleted_at IS NULL"
	}
	query += " ORDER BY ord, id"
	rows, err := q.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing entries: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating entries: %w", err)
	}
	return out, nil
}

// GetEntry returns one entry (deleted or not), or ErrNotFound.
func (q Queries) GetEntry(ctx context.Context, id string) (Entry, error) {
	row := q.db.QueryRowContext(ctx, "SELECT "+entryCols+" FROM entries WHERE id = ?", id)
	e, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, fmt.Errorf("entry %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return Entry{}, fmt.Errorf("getting entry %q: %w", id, err)
	}
	return e, nil
}

// MaxEntryOrd returns the largest ord in use, or -1 when there are none.
func (q Queries) MaxEntryOrd(ctx context.Context) (int64, error) {
	var n int64
	if err := q.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(ord), -1) FROM entries").Scan(&n); err != nil {
		return 0, fmt.Errorf("reading max entry ord: %w", err)
	}
	return n, nil
}

// InsertEntry inserts a new entry row.
func (q Queries) InsertEntry(ctx context.Context, e Entry) error {
	_, err := q.db.ExecContext(ctx,
		"INSERT INTO entries ("+entryCols+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		e.ID, string(e.Doc), e.Rev, e.Ord, e.DeletedAt, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("inserting entry %q: %w", e.ID, err)
	}
	return nil
}

// UpdateEntry overwrites doc, rev, ord, deleted_at and updated_at of an
// existing entry, but only if its current rev equals prevRev (optimistic
// concurrency). It returns ErrNotFound when no row matched.
func (q Queries) UpdateEntry(ctx context.Context, e Entry, prevRev int64) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE entries SET doc = ?, rev = ?, ord = ?, deleted_at = ?, updated_at = ?
		 WHERE id = ? AND rev = ?`,
		string(e.Doc), e.Rev, e.Ord, e.DeletedAt, e.UpdatedAt, e.ID, prevRev)
	if err != nil {
		return fmt.Errorf("updating entry %q: %w", e.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("updating entry %q: %w", e.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("entry %q at rev %d: %w", e.ID, prevRev, ErrNotFound)
	}
	return nil
}

// InsertRevision records one publication of an entry and returns its id.
func (q Queries) InsertRevision(ctx context.Context, r Revision) (int64, error) {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO entry_revisions (entry_id, rev, before, after, actor, reason, suggestion_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.EntryID, r.Rev, nullJSON(r.Before), nullJSON(r.After), r.Actor, r.Reason, r.SuggestionID, r.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("inserting revision %q rev %d: %w", r.EntryID, r.Rev, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inserting revision %q rev %d: %w", r.EntryID, r.Rev, err)
	}
	return id, nil
}

// ListRevisions returns an entry's revisions, newest first.
func (q Queries) ListRevisions(ctx context.Context, entryID string) ([]Revision, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, entry_id, rev, before, after, actor, reason, suggestion_id, created_at
		 FROM entry_revisions WHERE entry_id = ? ORDER BY rev DESC`, entryID)
	if err != nil {
		return nil, fmt.Errorf("listing revisions of %q: %w", entryID, err)
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		var r Revision
		var before, after sql.NullString
		if err := rows.Scan(&r.ID, &r.EntryID, &r.Rev, &before, &after, &r.Actor, &r.Reason, &r.SuggestionID, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning revision: %w", err)
		}
		if before.Valid {
			r.Before = json.RawMessage(before.String)
		}
		if after.Valid {
			r.After = json.RawMessage(after.String)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating revisions: %w", err)
	}
	return out, nil
}

// CountRevisions counts all revision rows.
func (q Queries) CountRevisions(ctx context.Context) (int, error) {
	var n int
	if err := q.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entry_revisions").Scan(&n); err != nil {
		return 0, fmt.Errorf("counting revisions: %w", err)
	}
	return n, nil
}

// nullJSON maps an empty message to SQL NULL.
func nullJSON(m json.RawMessage) any {
	if len(m) == 0 {
		return nil
	}
	return string(m)
}
