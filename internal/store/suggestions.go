package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Suggestion statuses.
const (
	StatusPending        = "pending"
	StatusAccepted       = "accepted"
	StatusAcceptedEdited = "accepted_edited"
	StatusRejected       = "rejected"
	StatusDuplicate      = "duplicate"
	StatusHidden         = "hidden"
)

// ErrNotPending is returned by guarded status transitions when the
// suggestion exists but is no longer pending.
var ErrNotPending = errors.New("suggestion is not pending")

// Suggestion is one row of the suggestions table, including the private
// columns (contact, internal_note, voter_id, ip_hash). Votes is filled by
// list queries only.
type Suggestion struct {
	ID           int64
	Type         string
	EntryID      string // "" for type new
	Title        string
	Summary      string
	Payload      json.RawMessage
	Snapshot     json.RawMessage // nil for type new
	BaseRev      *int64
	Reason       string
	Sources      json.RawMessage // JSON array of URLs
	Contact      string
	Status       string
	DuplicateOf  *int64
	DecisionNote string
	InternalNote string
	Final        json.RawMessage
	VoterID      string
	IPHash       string
	CreatedAt    int64
	DecidedAt    *int64

	Votes int  // COUNT(*) of votes; list and get queries
	Voted bool // the filter's VoterID has voted; list queries
}

const suggestionCols = `s.id, s.type, s.entry_id, s.title, s.summary, s.payload, s.snapshot, s.base_rev,
	s.reason, s.sources, s.contact, s.status, s.duplicate_of, s.decision_note, s.internal_note,
	s.final, s.voter_id, s.ip_hash, s.created_at, s.decided_at,
	(SELECT COUNT(*) FROM votes v WHERE v.suggestion_id = s.id)`

func scanSuggestion(sc interface{ Scan(...any) error }, extra ...any) (Suggestion, error) {
	var s Suggestion
	var entryID, snapshot, final sql.NullString
	var payload, sources string
	dest := []any{&s.ID, &s.Type, &entryID, &s.Title, &s.Summary, &payload, &snapshot, &s.BaseRev,
		&s.Reason, &sources, &s.Contact, &s.Status, &s.DuplicateOf, &s.DecisionNote, &s.InternalNote,
		&final, &s.VoterID, &s.IPHash, &s.CreatedAt, &s.DecidedAt, &s.Votes}
	if err := sc.Scan(append(dest, extra...)...); err != nil {
		return Suggestion{}, err
	}
	s.EntryID = entryID.String
	s.Payload = json.RawMessage(payload)
	s.Sources = json.RawMessage(sources)
	if snapshot.Valid {
		s.Snapshot = json.RawMessage(snapshot.String)
	}
	if final.Valid {
		s.Final = json.RawMessage(final.String)
	}
	return s, nil
}

// InsertSuggestion stores a new suggestion and returns its id. Status
// defaults to pending when empty.
func (q Queries) InsertSuggestion(ctx context.Context, s Suggestion) (int64, error) {
	if s.Status == "" {
		s.Status = StatusPending
	}
	var entryID any
	if s.EntryID != "" {
		entryID = s.EntryID
	}
	sources := s.Sources
	if len(sources) == 0 {
		sources = json.RawMessage("[]")
	}
	payload := s.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO suggestions (type, entry_id, title, summary, payload, snapshot, base_rev, reason,
		 sources, contact, status, voter_id, ip_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Type, entryID, s.Title, s.Summary, string(payload), nullJSON(s.Snapshot), s.BaseRev, s.Reason,
		string(sources), s.Contact, s.Status, s.VoterID, s.IPHash, s.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("inserting suggestion: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inserting suggestion: %w", err)
	}
	return id, nil
}

// GetSuggestion returns one suggestion (any status, private columns
// included) with its vote count, or ErrNotFound.
func (q Queries) GetSuggestion(ctx context.Context, id int64) (Suggestion, error) {
	row := q.db.QueryRowContext(ctx, "SELECT "+suggestionCols+" FROM suggestions s WHERE s.id = ?", id)
	s, err := scanSuggestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Suggestion{}, fmt.Errorf("suggestion %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Suggestion{}, fmt.Errorf("getting suggestion %d: %w", id, err)
	}
	return s, nil
}

// SuggestionStatus returns the status of one suggestion, or ErrNotFound.
func (q Queries) SuggestionStatus(ctx context.Context, id int64) (string, error) {
	var st string
	err := q.db.QueryRowContext(ctx, "SELECT status FROM suggestions WHERE id = ?", id).Scan(&st)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("suggestion %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("getting suggestion %d status: %w", id, err)
	}
	return st, nil
}

// SuggestionFilter selects suggestions for ListSuggestions. Zero values
// mean "no constraint"; Limit 0 means no limit.
type SuggestionFilter struct {
	Query         string   // substring of title or summary (case-insensitive)
	Type          string   // new, change, remove, image
	Statuses      []string // any of
	ExcludeHidden bool
	EntryID       string
	MostSupported bool   // order by votes, then newest; default newest first
	OldestFirst   bool   // order by age, oldest first (ignored with MostSupported)
	VoterID       string // fills Suggestion.Voted
	Limit         int
	Offset        int
}

// ListSuggestions returns one page of matching suggestions and the total
// number of matches.
func (q Queries) ListSuggestions(ctx context.Context, f SuggestionFilter) ([]Suggestion, int, error) {
	var where []string
	var args []any
	if f.Query != "" {
		like := "%" + escapeLike(strings.ToLower(f.Query)) + "%"
		where = append(where, `(lower(s.title) LIKE ? ESCAPE '\' OR lower(s.summary) LIKE ? ESCAPE '\' OR s.entry_id LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	if f.Type != "" {
		where = append(where, "s.type = ?")
		args = append(args, f.Type)
	}
	if len(f.Statuses) > 0 {
		where = append(where, "s.status IN (?"+strings.Repeat(", ?", len(f.Statuses)-1)+")")
		for _, st := range f.Statuses {
			args = append(args, st)
		}
	}
	if f.ExcludeHidden {
		where = append(where, "s.status <> 'hidden'")
	}
	if f.EntryID != "" {
		where = append(where, "s.entry_id = ?")
		args = append(args, f.EntryID)
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := q.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM suggestions s"+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting suggestions: %w", err)
	}

	order := " ORDER BY s.created_at DESC, s.id DESC"
	if f.OldestFirst {
		order = " ORDER BY s.created_at ASC, s.id ASC"
	}
	if f.MostSupported {
		order = " ORDER BY 21 DESC, s.created_at DESC, s.id DESC" // column 21 is the vote count
	}
	// The voted flag is a primary-key probe; anonymous lists skip it.
	voted, listArgs := "0", args
	if f.VoterID != "" {
		voted = "EXISTS (SELECT 1 FROM votes v WHERE v.suggestion_id = s.id AND v.voter_id = ?)"
		listArgs = append([]any{f.VoterID}, args...)
	}
	query := "SELECT " + suggestionCols + ", " + voted + " FROM suggestions s" + cond + order
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		listArgs = append(listArgs, f.Limit, f.Offset)
	}
	rows, err := q.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing suggestions: %w", err)
	}
	defer rows.Close()
	var out []Suggestion
	for rows.Next() {
		var didVote bool
		s, err := scanSuggestion(rows, &didVote)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning suggestion: %w", err)
		}
		s.Voted = didVote
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating suggestions: %w", err)
	}
	return out, total, nil
}

// Decision is an owner decision on a pending suggestion.
type Decision struct {
	Status       string // accepted, accepted_edited, rejected, duplicate, hidden
	DecisionNote string // public
	InternalNote string // private
	Final        json.RawMessage
	DuplicateOf  *int64
	At           int64
}

// DecideSuggestion records a decision, but only while the suggestion is
// pending (so a second click is a no-op). It returns ErrNotPending when the
// suggestion exists but was already decided, ErrNotFound when it does not
// exist. Run it in the same transaction as the publication it describes.
func (q Queries) DecideSuggestion(ctx context.Context, id int64, d Decision) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE suggestions SET status = ?, decision_note = ?, internal_note = ?, final = ?,
		 duplicate_of = ?, decided_at = ? WHERE id = ? AND status = 'pending'`,
		d.Status, d.DecisionNote, d.InternalNote, nullJSON(d.Final), d.DuplicateOf, d.At, id)
	if err != nil {
		return fmt.Errorf("deciding suggestion %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("deciding suggestion %d: %w", id, err)
	}
	if n == 1 {
		return nil
	}
	if _, err := q.SuggestionStatus(ctx, id); err != nil {
		return err
	}
	return fmt.Errorf("suggestion %d: %w", id, ErrNotPending)
}

// HideSuggestion marks a suggestion of any status as hidden (spam). Votes
// are kept. It returns ErrNotFound when the suggestion does not exist.
func (q Queries) HideSuggestion(ctx context.Context, id int64, internalNote string, at int64) error {
	res, err := q.db.ExecContext(ctx,
		`UPDATE suggestions SET status = 'hidden', internal_note = ?, decided_at = COALESCE(decided_at, ?)
		 WHERE id = ?`, internalNote, at, id)
	if err != nil {
		return fmt.Errorf("hiding suggestion %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("hiding suggestion %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("suggestion %d: %w", id, ErrNotFound)
	}
	return nil
}

// escapeLike escapes LIKE wildcards for use with ESCAPE '\'.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
