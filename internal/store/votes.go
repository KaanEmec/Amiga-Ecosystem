package store

import (
	"context"
	"fmt"
)

// InsertVote adds a +1 and reports whether a row was added (false when the
// voter had already voted). The (suggestion_id, voter_id) primary key makes
// this safe under concurrent requests.
func (q Queries) InsertVote(ctx context.Context, suggestionID int64, voterID string, at int64) (bool, error) {
	res, err := q.db.ExecContext(ctx,
		"INSERT INTO votes (suggestion_id, voter_id, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		suggestionID, voterID, at)
	if err != nil {
		return false, fmt.Errorf("inserting vote on %d: %w", suggestionID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inserting vote on %d: %w", suggestionID, err)
	}
	return n == 1, nil
}

// DeleteVote removes a +1 and reports whether a row was removed.
func (q Queries) DeleteVote(ctx context.Context, suggestionID int64, voterID string) (bool, error) {
	res, err := q.db.ExecContext(ctx,
		"DELETE FROM votes WHERE suggestion_id = ? AND voter_id = ?", suggestionID, voterID)
	if err != nil {
		return false, fmt.Errorf("deleting vote on %d: %w", suggestionID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("deleting vote on %d: %w", suggestionID, err)
	}
	return n == 1, nil
}

// CountVotes returns the number of +1s on a suggestion.
func (q Queries) CountVotes(ctx context.Context, suggestionID int64) (int, error) {
	var n int
	if err := q.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM votes WHERE suggestion_id = ?", suggestionID).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting votes on %d: %w", suggestionID, err)
	}
	return n, nil
}

// HasVote reports whether voterID has a +1 on the suggestion.
func (q Queries) HasVote(ctx context.Context, suggestionID int64, voterID string) (bool, error) {
	var one int
	err := q.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM votes WHERE suggestion_id = ? AND voter_id = ?", suggestionID, voterID).Scan(&one)
	if err != nil {
		return false, fmt.Errorf("reading vote on %d: %w", suggestionID, err)
	}
	return one > 0, nil
}
