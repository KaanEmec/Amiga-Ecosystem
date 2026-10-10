package suggest

import (
	"context"
	"time"

	"amigaconstellation/internal/store"
)

// pruneAfter is how long rate events are kept: the longest window.
const pruneAfter = 24 * time.Hour

// voteSuggestionKind counts +1s per (hashed IP, suggestion) over a day.
const voteSuggestionKind = "vote_suggestion"

// rateLimit is one limit applied per browser id and per hashed IP. Both
// check and record must run in the same transaction as the action.
type rateLimit struct {
	kind       string
	window     time.Duration
	perVisitor int
	perIP      int
}

type rateKey struct {
	key   string
	limit int
}

func (l rateLimit) keys(v Visitor) []rateKey {
	keys := []rateKey{{"v:" + v.ID, l.perVisitor}}
	if v.IPHash != "" {
		keys = append(keys, rateKey{"ip:" + v.IPHash, l.perIP})
	}
	return keys
}

// check returns ErrRateLimited when any key is at its limit.
func (l rateLimit) check(ctx context.Context, q store.Queries, v Visitor, now time.Time) error {
	since := now.Add(-l.window).Unix()
	for _, k := range l.keys(v) {
		n, err := q.CountRateEvents(ctx, k.key, l.kind, since)
		if err != nil {
			return err
		}
		if n >= k.limit {
			return ErrRateLimited
		}
	}
	return nil
}

// record stores one event per key and prunes expired ones.
func (l rateLimit) record(ctx context.Context, q store.Queries, v Visitor, now time.Time) error {
	if err := q.PruneRateEvents(ctx, now.Add(-pruneAfter).Unix()); err != nil {
		return err
	}
	for _, k := range l.keys(v) {
		if err := q.InsertRateEvent(ctx, k.key, l.kind, now.Unix()); err != nil {
			return err
		}
	}
	return nil
}
