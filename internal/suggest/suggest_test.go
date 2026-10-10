package suggest

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
)

var testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newTestService(t *testing.T, cfg config.Config) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "amiga.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(st)
	doc, err := catalog.LoadDocument("../../data/amiga-data.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Import(t.Context(), doc, "test"); err != nil {
		t.Fatal(err)
	}
	if cfg.RateSuggestPerDay == 0 {
		cfg.RateSuggestPerDay, cfg.RateSuggestPerDayIP, cfg.RateVotePerHour, cfg.RateVotePerHourIP = 5, 25, 30, 150
	}
	if cfg.RateVotePerSuggestionIP == 0 {
		cfg.RateVotePerSuggestionIP = 100
	}
	return New(st, cat, cfg, func() time.Time { return testNow }), st
}

func ptr[T any](v T) *T { return &v }

var alice = Visitor{ID: "alice", IPHash: "iphash-1"}

func newProposal() Proposal {
	return Proposal{
		Type: TypeNew,
		Fields: Fields{
			Name: ptr("Amiga Test Card"), Year: ptr(1993), Category: ptr("hw-classic"), Kind: ptr("original"),
			Status: ptr("historic"), Arch: []string{"68k"}, By: ptr("Tester"),
			What: ptr("A card used in tests."), Why: ptr("It proves the form works."), Related: []string{"a2000"},
		},
		Reason:  "Missing from the catalogue",
		Sources: []string{"https://example.org/card"},
		Contact: "me@example.org",
	}
}

func TestService_Create_EachType(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t, config.Config{})
	ctx := t.Context()
	tests := []struct {
		name  string
		p     Proposal
		title string
	}{
		{"new", newProposal(), "Amiga Test Card"},
		{"change", Proposal{Type: TypeChange, EntryID: "a2500", Fields: Fields{Year: ptr(1990)}, Reason: "Released in 1990"}, "Amiga 2500"},
		{"remove", Proposal{Type: TypeRemove, EntryID: "a2500", Reason: "Not a separate model"}, "Amiga 2500"},
		{"image", Proposal{Type: TypeImage, EntryID: "a2500", ImageURL: "https://example.org/a.png", ImageCredit: "CC BY-SA, Someone", Reason: "Clear photo"}, "Amiga 2500"},
	}
	for _, tt := range tests {
		id, err := svc.Create(ctx, tt.p, alice)
		if err != nil {
			t.Fatalf("%s: Create() error = %v", tt.name, err)
		}
		got, err := svc.Get(ctx, id, "")
		if err != nil {
			t.Fatalf("%s: Get() error = %v", tt.name, err)
		}
		if got.Status != store.StatusPending || got.Type != tt.p.Type || got.Title != tt.title || got.Summary == "" {
			t.Errorf("%s: got %+v", tt.name, got)
		}
	}
	list, err := svc.List(ctx, ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != len(tests) || len(list.Items) != len(tests) {
		t.Fatalf("List total = %d, items = %d", list.Total, len(list.Items))
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), "me@example.org") || strings.Contains(string(b), "iphash") {
		t.Errorf("public list leaks private data: %s", b)
	}
	for _, typ := range Types {
		res, err := svc.List(ctx, ListParams{Type: typ})
		if err != nil || res.Total != 1 {
			t.Errorf("List(type=%s) total = %d, err = %v", typ, res.Total, err)
		}
	}
}

func TestService_Create_NewEntry(t *testing.T) {
	t.Parallel()
	svc, st := newTestService(t, config.Config{})
	ctx := t.Context()

	// weight and unverified arrive from a hostile client and are dropped.
	raw := `{"type":"new","fields":{"name":"Amiga Test Card","year":1993,"category":"hw-classic","kind":"original",
	  "status":"historic","arch":["68k"],"what":"A card.","why":"Tests.","weight":5,"unverified":true,"image":"https://x.example/i.png"}}`
	var p Proposal
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	id, err := svc.Create(ctx, p, alice)
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.Q().GetSuggestion(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"amiga-test-card","name":"Amiga Test Card","year":1993,"category":"hw-classic","kind":"original","status":"historic","arch":["68k"],"what":"A card.","why":"Tests."}`
	if string(row.Payload) != want {
		t.Errorf("payload = %s\nwant      %s", row.Payload, want)
	}
	if row.EntryID != "" || row.Snapshot != nil || row.BaseRev != nil {
		t.Errorf("new suggestion has entry columns: %+v", row)
	}

	// The id (given or derived from the name) must not collide.
	dup := newProposal()
	dup.Fields.ID = ptr("a2500")
	if _, err := svc.Create(ctx, dup, alice); !isValidation(err, "already exists") {
		t.Errorf("colliding id: error = %v", err)
	}
	again := newProposal() // derives amiga-test-card, which is only pending, not an entry
	if _, err := svc.Create(ctx, again, alice); err != nil {
		t.Errorf("pending ids do not collide: %v", err)
	}
}

func TestService_Create_ChangeStoresOnlyChangedFields(t *testing.T) {
	t.Parallel()
	svc, st := newTestService(t, config.Config{})
	ctx := t.Context()
	cur, err := st.Q().GetEntry(ctx, "a2500")
	if err != nil {
		t.Fatal(err)
	}
	// Submit every field as the form does, with only year and tags edited.
	p := Proposal{Type: TypeChange, EntryID: "a2500", Reason: "Correct year", Fields: Fields{
		ID: ptr("hijack"), Name: ptr("Amiga 2500"), Year: ptr(1988), Category: ptr("hw-classic"),
		Kind: ptr("original"), Status: ptr("historic"), Arch: []string{"68k"}, By: ptr("Commodore"),
		What:    ptr("Factory A2000 configurations bundled with an A2620 (68020) or A2630 (68030) accelerator and SCSI."),
		Why:     ptr("It formalised the community's favourite idea: start with an expandable Amiga and grow it into a workstation."),
		Related: []string{"a2000"}, Tags: []string{"bigbox", "workstation"}, URL: ptr(""), Version: ptr(""),
	}}
	id, err := svc.Create(ctx, p, alice)
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.Q().GetSuggestion(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"year":1988,"tags":["bigbox","workstation"]}`; string(row.Payload) != want {
		t.Errorf("payload = %s, want %s", row.Payload, want)
	}
	if string(row.Snapshot) != string(cur.Doc) || row.BaseRev == nil || *row.BaseRev != cur.Rev {
		t.Errorf("snapshot/base_rev not recorded: rev=%v", row.BaseRev)
	}
	if !strings.Contains(row.Summary, "year") {
		t.Errorf("summary = %q", row.Summary)
	}

	unchanged := Proposal{Type: TypeChange, EntryID: "a2500", Fields: Fields{Year: ptr(1989)}}
	if _, err := svc.Create(ctx, unchanged, alice); !isValidation(err, "Nothing was changed") {
		t.Errorf("no-op change: error = %v", err)
	}
}

func TestService_Create_Validation(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t, config.Config{})
	tests := []struct {
		name string
		p    Proposal
		want string
	}{
		{"no type", Proposal{}, "Choose what kind"},
		{"unknown entry", Proposal{Type: TypeChange, EntryID: "nope"}, "existing entry"},
		{"remove without reason", Proposal{Type: TypeRemove, EntryID: "a2500"}, "why the entry should be removed"},
		{"image without reason", Proposal{Type: TypeImage, EntryID: "a2500", ImageURL: "https://example.org/a.png", ImageCredit: "x"}, "why this image"},
		{"image bad url", Proposal{Type: TypeImage, EntryID: "a2500", ImageURL: "javascript:alert(1)", ImageCredit: "x", Reason: "r"}, "image URL"},
		{"bad source", func() Proposal { p := newProposal(); p.Sources = []string{"ftp://x"}; return p }(), "Source"},
		{"unknown category", func() Proposal { p := newProposal(); p.Fields.Category = ptr("nope"); return p }(), "unknown category"},
		{"unknown related", func() Proposal { p := newProposal(); p.Fields.Related = []string{"zzz"}; return p }(), "related \"zzz\""},
		{"missing what", func() Proposal { p := newProposal(); p.Fields.What = nil; return p }(), "What is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := svc.Create(t.Context(), tt.p, alice); !isValidation(err, tt.want) {
				t.Errorf("Create() error = %v, want validation containing %q", err, tt.want)
			}
		})
	}
}

func isValidation(err error, contains string) bool {
	var ve *ValidationError
	return errors.As(err, &ve) && strings.Contains(strings.ToLower(ve.Error()), strings.ToLower(contains))
}

func TestService_ToggleVote(t *testing.T) {
	t.Parallel()
	svc, st := newTestService(t, config.Config{})
	ctx := t.Context()
	id, err := svc.Create(ctx, newProposal(), alice)
	if err != nil {
		t.Fatal(err)
	}
	bob := Visitor{ID: "bob", IPHash: "iphash-2"}
	steps := []struct {
		v    Visitor
		on   bool
		want int
	}{{bob, true, 1}, {bob, true, 1}, {alice, true, 2}, {bob, false, 1}, {bob, false, 1}}
	for i, s := range steps {
		n, err := svc.ToggleVote(ctx, id, s.v, s.on)
		if err != nil || n != s.want {
			t.Fatalf("step %d: ToggleVote() = %d, %v; want %d", i, n, err, s.want)
		}
	}
	got, err := svc.Get(ctx, id, "alice")
	if err != nil || !got.Voted || got.Votes != 1 {
		t.Errorf("Get() = %+v, %v", got, err)
	}

	// Resolution freezes the count.
	if err := st.Q().DecideSuggestion(ctx, id, store.Decision{Status: store.StatusRejected, At: testNow.Unix()}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ToggleVote(ctx, id, bob, true); !errors.Is(err, ErrClosed) {
		t.Errorf("vote after resolution: error = %v, want ErrClosed", err)
	}
	if _, err := svc.ToggleVote(ctx, id, alice, false); !errors.Is(err, ErrClosed) {
		t.Errorf("unvote after resolution: error = %v, want ErrClosed", err)
	}
	if got, _ := svc.Get(ctx, id, ""); got.Votes != 1 {
		t.Errorf("votes after resolution = %d, want 1", got.Votes)
	}
	if _, err := svc.ToggleVote(ctx, 9999, bob, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown suggestion: error = %v", err)
	}
}

func TestService_ToggleVote_Concurrent(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t, config.Config{})
	ctx := t.Context()
	id, err := svc.Create(ctx, newProposal(), alice)
	if err != nil {
		t.Fatal(err)
	}
	run := func(voter func(i int) Visitor) {
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Go(func() {
				if _, err := svc.ToggleVote(ctx, id, voter(i), true); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
	}
	run(func(int) Visitor { return Visitor{ID: "same", IPHash: "ip"} })
	if got, _ := svc.Get(ctx, id, ""); got.Votes != 1 {
		t.Fatalf("same voter x20: votes = %d, want 1", got.Votes)
	}
	run(func(i int) Visitor { return Visitor{ID: fmt.Sprintf("v%d", i), IPHash: "ip"} })
	if got, _ := svc.Get(ctx, id, ""); got.Votes != 21 {
		t.Fatalf("20 voters: votes = %d, want 21", got.Votes)
	}
}

func TestService_HiddenAndDuplicate(t *testing.T) {
	t.Parallel()
	svc, st := newTestService(t, config.Config{})
	ctx := t.Context()
	canonical, err := svc.Create(ctx, newProposal(), alice)
	if err != nil {
		t.Fatal(err)
	}
	dupP := newProposal()
	dupP.Fields.Name = ptr("Amiga Test Card Two")
	dup, err := svc.Create(ctx, dupP, alice)
	if err != nil {
		t.Fatal(err)
	}
	spamP := newProposal()
	spamP.Fields.Name = ptr("Spam spam spam")
	spam, err := svc.Create(ctx, spamP, alice)
	if err != nil {
		t.Fatal(err)
	}
	q := st.Q()
	if err := q.DecideSuggestion(ctx, dup, store.Decision{Status: store.StatusDuplicate, DuplicateOf: &canonical, At: testNow.Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := q.HideSuggestion(ctx, spam, "spam", testNow.Unix()); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(ctx, ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 {
		t.Errorf("list total = %d, want 2 (hidden excluded)", list.Total)
	}
	for _, it := range list.Items {
		if it.ID == spam {
			t.Error("hidden suggestion listed")
		}
	}
	if _, err := svc.Get(ctx, spam, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(hidden) error = %v", err)
	}
	if _, err := svc.ToggleVote(ctx, spam, alice, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("vote on hidden: error = %v", err)
	}
	h, err := svc.Similar(ctx, "Spam spam")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Suggestions) != 0 {
		t.Errorf("hidden suggestion in hints: %+v", h.Suggestions)
	}
	got, err := svc.Get(ctx, dup, "")
	if err != nil || got.DuplicateOf == nil || *got.DuplicateOf != canonical {
		t.Errorf("duplicate = %+v, %v", got, err)
	}
	// A decided suggestion cannot be decided again.
	if err := q.DecideSuggestion(ctx, dup, store.Decision{Status: store.StatusAccepted, At: 1}); !errors.Is(err, store.ErrNotPending) {
		t.Errorf("second decision error = %v", err)
	}
}

func TestService_RateLimits(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t, config.Config{RateSuggestPerDay: 2, RateSuggestPerDayIP: 3, RateVotePerHour: 2, RateVotePerHourIP: 100})
	ctx := t.Context()
	mk := func(i int) Proposal {
		p := newProposal()
		p.Fields.Name = ptr(fmt.Sprintf("Rate Card %d", i))
		return p
	}
	for i := range 2 {
		if _, err := svc.Create(ctx, mk(i), alice); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Create(ctx, mk(9), alice); !errors.Is(err, ErrRateLimited) {
		t.Errorf("third suggestion from one browser: error = %v", err)
	}
	// Same IP, new browser: the shared ceiling (3) allows one more.
	carol := Visitor{ID: "carol", IPHash: alice.IPHash}
	if _, err := svc.Create(ctx, mk(3), carol); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, mk(4), carol); !errors.Is(err, ErrRateLimited) {
		t.Errorf("IP ceiling: error = %v", err)
	}

	list, _ := svc.List(ctx, ListParams{})
	id := list.Items[0].ID
	bob := Visitor{ID: "bob", IPHash: "other"}
	for _, on := range []bool{true, false} {
		if _, err := svc.ToggleVote(ctx, id, bob, on); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ToggleVote(ctx, id, bob, true); !errors.Is(err, ErrRateLimited) {
		t.Errorf("third vote change: error = %v", err)
	}
}

func TestService_VotesPerSuggestionPerIP(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t, config.Config{RateSuggestPerDay: 5, RateSuggestPerDayIP: 25, RateVotePerHour: 30,
		RateVotePerHourIP: 150, RateVotePerSuggestionIP: 3})
	ctx := t.Context()
	id, err := svc.Create(ctx, newProposal(), alice)
	if err != nil {
		t.Fatal(err)
	}
	other := newProposal()
	other.Fields.Name = ptr("Another Test Card")
	id2, err := svc.Create(ctx, other, alice)
	if err != nil {
		t.Fatal(err)
	}
	// Fresh browser ids from one IP: only 3 +1s reach one suggestion.
	voter := func(i int) Visitor { return Visitor{ID: fmt.Sprintf("cookie-%d", i), IPHash: "shared-ip"} }
	for i := range 3 {
		if _, err := svc.ToggleVote(ctx, id, voter(i), true); err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
	}
	if _, err := svc.ToggleVote(ctx, id, voter(3), true); !errors.Is(err, ErrRateLimited) {
		t.Errorf("4th vote from one IP on one suggestion: error = %v", err)
	}
	// Removing a vote does not count against the cap and frees nothing.
	if _, err := svc.ToggleVote(ctx, id, voter(0), false); err != nil {
		t.Errorf("unvote: %v", err)
	}
	if _, err := svc.ToggleVote(ctx, id, voter(4), true); !errors.Is(err, ErrRateLimited) {
		t.Errorf("vote after unvote: error = %v", err)
	}
	// Other suggestions and other IPs are unaffected.
	if _, err := svc.ToggleVote(ctx, id2, voter(5), true); err != nil {
		t.Errorf("same IP, other suggestion: %v", err)
	}
	if n, err := svc.ToggleVote(ctx, id, Visitor{ID: "elsewhere", IPHash: "other-ip"}, true); err != nil || n != 3 {
		t.Errorf("other IP: votes %d, err %v", n, err)
	}
}

func TestService_Pauses(t *testing.T) {
	t.Parallel()
	svc, st := newTestService(t, config.Config{})
	ctx := t.Context()
	id, err := svc.Create(ctx, newProposal(), alice)
	if err != nil {
		t.Fatal(err)
	}
	q := st.Q()
	if err := q.SetSetting(ctx, store.SettingSubmissionsPaused, "1"); err != nil {
		t.Fatal(err)
	}
	if err := q.SetSetting(ctx, store.SettingVotingPaused, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, newProposal(), alice); !errors.Is(err, ErrSubmissionsPaused) {
		t.Errorf("Create while paused: %v", err)
	}
	if _, err := svc.ToggleVote(ctx, id, alice, true); !errors.Is(err, ErrVotingPaused) {
		t.Errorf("vote while paused: %v", err)
	}
	if sub, vote, err := svc.Paused(ctx); !sub || !vote || err != nil {
		t.Errorf("Paused() = %v %v %v", sub, vote, err)
	}
}

func TestService_Similar(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t, config.Config{})
	ctx := t.Context()
	if _, err := svc.Create(ctx, Proposal{Type: TypeRemove, EntryID: "a2500", Reason: "dup"}, alice); err != nil {
		t.Fatal(err)
	}
	h, err := svc.Similar(ctx, "amiga 2500")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Entries) == 0 || h.Entries[0].ID != "a2500" {
		t.Errorf("entries = %+v", h.Entries)
	}
	if len(h.Suggestions) != 1 || h.Suggestions[0].EntryID != "a2500" {
		t.Errorf("suggestions = %+v", h.Suggestions)
	}
	if h, _ := svc.Similar(ctx, "x"); len(h.Entries) != 0 {
		t.Errorf("one-letter query matched %d entries", len(h.Entries))
	}
}

func TestSigner(t *testing.T) {
	t.Parallel()
	s := NewSigner([]byte(strings.Repeat("k", 32)))
	tok, id, err := s.NewVisitor()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := s.VisitorID(tok); !ok || got != id {
		t.Errorf("VisitorID() = %q, %v", got, ok)
	}
	tampered := []byte(tok)
	tampered[3] ^= 1
	if _, ok := s.VisitorID(string(tampered)); ok {
		t.Error("tampered cookie accepted")
	}
	if _, ok := NewSigner([]byte(strings.Repeat("x", 32))).VisitorID(tok); ok {
		t.Error("cookie accepted under another secret")
	}

	ft := s.FormToken(testNow)
	tests := []struct {
		name string
		tok  string
		at   time.Time
		want error
	}{
		{"ok", ft, testNow.Add(10 * time.Second), nil},
		{"too fast", ft, testNow.Add(time.Second), ErrFormTooFast},
		{"expired", ft, testNow.Add(25 * time.Hour), ErrFormExpired},
		{"forged", "1.abc", testNow, ErrFormToken},
		{"empty", "", testNow, ErrFormToken},
	}
	for _, tt := range tests {
		if err := s.CheckFormToken(tt.tok, tt.at, 3*time.Second, 24*time.Hour); !errors.Is(err, tt.want) {
			t.Errorf("%s: CheckFormToken() = %v, want %v", tt.name, err, tt.want)
		}
	}
	if s.HashIP("203.0.113.1") == s.HashIP("203.0.113.2") || len(s.HashIP("203.0.113.1")) != 64 {
		t.Error("HashIP does not distinguish IPs")
	}
}
