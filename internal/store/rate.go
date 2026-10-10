package store

import (
	"context"
	"fmt"
)

// CountRateEvents counts events of kind for key at or after since.
func (q Queries) CountRateEvents(ctx context.Context, key, kind string, since int64) (int, error) {
	var n int
	err := q.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM rate_events WHERE key = ? AND kind = ? AND at >= ?", key, kind, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counting rate events: %w", err)
	}
	return n, nil
}

// InsertRateEvent records one event.
func (q Queries) InsertRateEvent(ctx context.Context, key, kind string, at int64) error {
	if _, err := q.db.ExecContext(ctx,
		"INSERT INTO rate_events (key, kind, at) VALUES (?, ?, ?)", key, kind, at); err != nil {
		return fmt.Errorf("inserting rate event: %w", err)
	}
	return nil
}

// PruneRateEvents deletes events older than before.
func (q Queries) PruneRateEvents(ctx context.Context, before int64) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM rate_events WHERE at < ?", before); err != nil {
		return fmt.Errorf("pruning rate events: %w", err)
	}
	return nil
}
