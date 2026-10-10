package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// ModerationEvent is one row of moderation_log.
type ModerationEvent struct {
	ID           int64
	SuggestionID *int64
	EntryID      *string
	Action       string
	Detail       json.RawMessage // JSON object; "{}" when empty
	CreatedAt    int64
}

// InsertModeration appends an event to moderation_log.
func (q Queries) InsertModeration(ctx context.Context, ev ModerationEvent) (int64, error) {
	detail := string(ev.Detail)
	if detail == "" {
		detail = "{}"
	}
	res, err := q.db.ExecContext(ctx,
		"INSERT INTO moderation_log (suggestion_id, entry_id, action, detail, created_at) VALUES (?, ?, ?, ?, ?)",
		ev.SuggestionID, ev.EntryID, ev.Action, detail, ev.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("inserting moderation event %q: %w", ev.Action, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inserting moderation event %q: %w", ev.Action, err)
	}
	return id, nil
}

// ListModeration returns the newest events, at most limit (all when
// entryID is empty, otherwise only that entry's).
func (q Queries) ListModeration(ctx context.Context, entryID string, limit int) ([]ModerationEvent, error) {
	query := "SELECT id, suggestion_id, entry_id, action, detail, created_at FROM moderation_log"
	args := []any{}
	if entryID != "" {
		query += " WHERE entry_id = ?"
		args = append(args, entryID)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing moderation log: %w", err)
	}
	defer rows.Close()
	var out []ModerationEvent
	for rows.Next() {
		var ev ModerationEvent
		var detail string
		if err := rows.Scan(&ev.ID, &ev.SuggestionID, &ev.EntryID, &ev.Action, &detail, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning moderation event: %w", err)
		}
		ev.Detail = json.RawMessage(detail)
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating moderation log: %w", err)
	}
	return out, nil
}

// AdminCounts summarises the catalogue for the owner dashboard.
type AdminCounts struct {
	LiveEntries        int
	DeletedEntries     int
	PendingSuggestions int
	Revisions          int
	Images             int
}

// CountForAdmin gathers the dashboard counters in one query.
func (q Queries) CountForAdmin(ctx context.Context) (AdminCounts, error) {
	var c AdminCounts
	err := q.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM entries WHERE deleted_at IS NULL),
		(SELECT COUNT(*) FROM entries WHERE deleted_at IS NOT NULL),
		(SELECT COUNT(*) FROM suggestions WHERE status = 'pending'),
		(SELECT COUNT(*) FROM entry_revisions),
		(SELECT COUNT(*) FROM images)`).
		Scan(&c.LiveEntries, &c.DeletedEntries, &c.PendingSuggestions, &c.Revisions, &c.Images)
	if err != nil {
		return AdminCounts{}, fmt.Errorf("counting for dashboard: %w", err)
	}
	return c, nil
}
