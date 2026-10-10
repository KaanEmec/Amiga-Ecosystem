package suggest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"amigaconstellation/internal/store"
)

// PageSize is the public list page size.
const PageSize = 20

// similarScanLimit bounds how many recent pending suggestions Similar
// compares per request.
const similarScanLimit = 100

// Public is a suggestion as visitors may see it: no contact, internal
// note, voter id or IP hash. Payload/Snapshot/Final are set by Get only.
type Public struct {
	ID           int64           `json:"id"`
	Type         string          `json:"type"`
	EntryID      string          `json:"entry_id,omitempty"`
	Title        string          `json:"title"`
	Summary      string          `json:"summary"`
	Status       string          `json:"status"`
	Votes        int             `json:"votes"`
	Voted        bool            `json:"voted"`
	CreatedAt    int64           `json:"created_at"`
	DecidedAt    *int64          `json:"decided_at,omitempty"`
	DuplicateOf  *int64          `json:"duplicate_of,omitempty"`
	DecisionNote string          `json:"decision_note,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Sources      []string        `json:"sources,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Snapshot     json.RawMessage `json:"snapshot,omitempty"`
	BaseRev      *int64          `json:"base_rev,omitempty"`
	Final        json.RawMessage `json:"final,omitempty"`
}

func toPublic(s store.Suggestion, full bool) Public {
	p := Public{
		ID: s.ID, Type: s.Type, EntryID: s.EntryID, Title: s.Title, Summary: s.Summary,
		Status: s.Status, Votes: s.Votes, Voted: s.Voted, CreatedAt: s.CreatedAt,
		DecidedAt: s.DecidedAt, DuplicateOf: s.DuplicateOf, DecisionNote: s.DecisionNote,
	}
	if full {
		p.Reason, p.Payload, p.Snapshot, p.BaseRev, p.Final = s.Reason, s.Payload, s.Snapshot, s.BaseRev, s.Final
		_ = json.Unmarshal(s.Sources, &p.Sources)
	}
	return p
}

// Public statuses accepted by the status filter; "accepted" also matches
// accepted_edited.
var publicStatuses = []string{store.StatusPending, store.StatusAccepted, store.StatusAcceptedEdited, store.StatusRejected, store.StatusDuplicate}

// ListParams selects a public list page.
type ListParams struct {
	Query   string
	Type    string // "" or one of Types
	Status  string // "" or one of publicStatuses
	Sort    string // "newest" (default) or "supported"
	Page    int    // 1-based
	VoterID string
}

// ListResult is one public list page.
type ListResult struct {
	Items []Public `json:"items"`
	Total int      `json:"total"`
	Page  int      `json:"page"`
	Pages int      `json:"pages"`
}

// Normalize drops unknown filter values and clamps the page.
func (p ListParams) Normalize() ListParams {
	p.Query = strings.TrimSpace(p.Query)
	if len(p.Query) > 100 {
		p.Query = p.Query[:100]
	}
	if !slices.Contains(Types, p.Type) {
		p.Type = ""
	}
	if !slices.Contains(publicStatuses, p.Status) {
		p.Status = ""
	}
	if p.Sort != "supported" {
		p.Sort = "newest"
	}
	if p.Page < 1 {
		p.Page = 1
	}
	return p
}

// List returns one page of non-hidden suggestions.
func (s *Service) List(ctx context.Context, p ListParams) (ListResult, error) {
	p = p.Normalize()
	f := store.SuggestionFilter{
		Query: p.Query, Type: p.Type, ExcludeHidden: true,
		MostSupported: p.Sort == "supported", VoterID: p.VoterID,
		Limit: PageSize, Offset: (p.Page - 1) * PageSize,
	}
	switch p.Status {
	case "":
	case store.StatusAccepted:
		f.Statuses = []string{store.StatusAccepted, store.StatusAcceptedEdited}
	default:
		f.Statuses = []string{p.Status}
	}
	rows, total, err := s.store.Q().ListSuggestions(ctx, f)
	if err != nil {
		return ListResult{}, err
	}
	res := ListResult{Items: make([]Public, len(rows)), Total: total, Page: p.Page, Pages: max(1, (total+PageSize-1)/PageSize)}
	for i, r := range rows {
		res.Items[i] = toPublic(r, false)
	}
	return res, nil
}

// Get returns one suggestion for public display; hidden ones are
// ErrNotFound.
func (s *Service) Get(ctx context.Context, id int64, voterID string) (Public, error) {
	q := s.store.Q()
	sg, err := q.GetSuggestion(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && sg.Status == store.StatusHidden) {
		return Public{}, ErrNotFound
	}
	if err != nil {
		return Public{}, err
	}
	if voterID != "" {
		if sg.Voted, err = q.HasVote(ctx, id, voterID); err != nil {
			return Public{}, err
		}
	}
	return toPublic(sg, true), nil
}

// ForEntry returns the pending suggestions about one entry (newest first).
func (s *Service) ForEntry(ctx context.Context, entryID string) ([]Public, error) {
	rows, _, err := s.store.Q().ListSuggestions(ctx, store.SuggestionFilter{
		EntryID: entryID, Statuses: []string{store.StatusPending}, Limit: 10,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Public, len(rows))
	for i, r := range rows {
		out[i] = toPublic(r, false)
	}
	return out, nil
}

// Hints are likely duplicates of what a visitor is about to propose.
type Hints struct {
	Entries     []EntryRef `json:"entries"`
	Suggestions []Public   `json:"suggestions"`
}

// Similar returns live entries whose name or id resembles q, and pending
// suggestions about those entries or with a similar title.
func (s *Service) Similar(ctx context.Context, q string) (Hints, error) {
	hints := Hints{Entries: []EntryRef{}, Suggestions: []Public{}}
	nq := normalize(q)
	if len([]rune(nq)) < 2 {
		return hints, nil
	}
	if len(nq) > 100 {
		nq = nq[:100]
	}
	idx, err := s.index(ctx)
	if err != nil {
		return Hints{}, err
	}
	qTokens := strings.Fields(nq)
	slug := strings.ReplaceAll(nq, " ", "-")
	type scored struct {
		ref   EntryRef
		score int
	}
	var hits []scored
	for _, e := range idx.entries {
		sc := similarity(nq, qTokens, e.norm, e.tokens)
		if e.ID == slug {
			sc = 100
		} else if sc == 0 && len(slug) >= 3 && strings.Contains(e.ID, slug) {
			sc = 40
		}
		if sc > 0 {
			hits = append(hits, scored{e, sc})
		}
	}
	slices.SortStableFunc(hits, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(a.ref.norm, b.ref.norm)
	})
	matched := map[string]bool{}
	for i, h := range hits {
		if i == 10 {
			break
		}
		hints.Entries = append(hints.Entries, h.ref)
		matched[h.ref.ID] = true
	}

	pending, _, err := s.store.Q().ListSuggestions(ctx, store.SuggestionFilter{
		Statuses: []string{store.StatusPending}, Limit: similarScanLimit,
	})
	if err != nil {
		return Hints{}, err
	}
	for _, p := range pending {
		nt := normalize(p.Title)
		if matched[p.EntryID] || similarity(nq, qTokens, nt, strings.Fields(nt)) >= 50 {
			hints.Suggestions = append(hints.Suggestions, toPublic(p, false))
			if len(hints.Suggestions) == 6 {
				break
			}
		}
	}
	return hints, nil
}

// ToggleVote adds (on) or removes a +1 from visitor v and returns the new
// count. Only pending suggestions accept changes; one vote per (suggestion,
// browser id) is enforced by the votes primary key, and the check, change
// and rate accounting share one transaction.
func (s *Service) ToggleVote(ctx context.Context, suggestionID int64, v Visitor, on bool) (int, error) {
	if v.ID == "" {
		return 0, ErrNoVisitor
	}
	if paused, err := s.store.Q().SettingBool(ctx, store.SettingVotingPaused); err != nil {
		return 0, err
	} else if paused {
		return 0, ErrVotingPaused
	}
	now := s.now()
	lim := rateLimit{kind: "vote", window: time.Hour, perVisitor: s.cfg.RateVotePerHour, perIP: s.cfg.RateVotePerHourIP}
	// Cookies are free to mint, so +1s from one IP to one suggestion are
	// capped per day as well.
	var perSuggestionKey string
	if on && v.IPHash != "" {
		perSuggestionKey = fmt.Sprintf("ip:%s:s:%d", v.IPHash, suggestionID)
	}
	var count int
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		st, err := q.SuggestionStatus(ctx, suggestionID)
		if errors.Is(err, store.ErrNotFound) || st == store.StatusHidden {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if st != store.StatusPending {
			return ErrClosed
		}
		if err := lim.check(ctx, q, v, now); err != nil {
			return err
		}
		if perSuggestionKey != "" {
			n, err := q.CountRateEvents(ctx, perSuggestionKey, voteSuggestionKind, now.Add(-24*time.Hour).Unix())
			if err != nil {
				return err
			}
			if n >= s.cfg.RateVotePerSuggestionIP {
				return ErrRateLimited
			}
		}
		var changed bool
		if on {
			changed, err = q.InsertVote(ctx, suggestionID, v.ID, now.Unix())
		} else {
			changed, err = q.DeleteVote(ctx, suggestionID, v.ID)
		}
		if err != nil {
			return err
		}
		if changed {
			if err := lim.record(ctx, q, v, now); err != nil {
				return err
			}
			if perSuggestionKey != "" {
				if err := q.InsertRateEvent(ctx, perSuggestionKey, voteSuggestionKind, now.Unix()); err != nil {
					return err
				}
			}
		}
		count, err = q.CountVotes(ctx, suggestionID)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("voting on %d: %w", suggestionID, err)
	}
	return count, nil
}
