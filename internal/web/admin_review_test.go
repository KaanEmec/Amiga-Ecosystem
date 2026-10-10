package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"amigaconstellation/internal/images"
	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
)

func sptr[T any](v T) *T { return &v }

var reviewVisitor = suggest.Visitor{ID: "visitor-1", IPHash: "iphash-1"}

// propose files a suggestion as a visitor, through the suggest service.
func (e *adminEnv) propose(t *testing.T, p suggest.Proposal) int64 {
	t.Helper()
	if p.Reason == "" {
		p.Reason = "test reason"
	}
	id, err := e.svc.Create(t.Context(), p, reviewVisitor)
	if err != nil {
		t.Fatalf("Create(%s): %v", p.Type, err)
	}
	return id
}

func (e *adminEnv) suggestion(t *testing.T, id int64) store.Suggestion {
	t.Helper()
	sg, err := e.st.Q().GetSuggestion(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sg
}

func (e *adminEnv) catalogETag(t *testing.T) string {
	t.Helper()
	return e.request(t, http.MethodGet, "/api/catalog", nil, nil, nil).Header().Get("ETag")
}

// decisionLogged reports whether moderation_log has the decision row.
func (e *adminEnv) decisionLogged(t *testing.T, id int64, status string) bool {
	t.Helper()
	evs, err := e.st.Q().ListModeration(t.Context(), "", 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.SuggestionID != nil && *ev.SuggestionID == id && ev.Action == "suggestion_"+status {
			return true
		}
	}
	return false
}

func (e *adminEnv) revisionCount(t *testing.T, entryID string) int {
	t.Helper()
	revs, err := e.st.Q().ListRevisions(t.Context(), entryID)
	if err != nil {
		t.Fatal(err)
	}
	return len(revs)
}

// published returns entry id from the public catalogue as a generic map
// (nil when absent).
func (e *adminEnv) published(t *testing.T, id string) map[string]any {
	t.Helper()
	for _, raw := range e.publicCatalog(t).Entries {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if m["id"] == id {
			return m
		}
	}
	return nil
}

func newProposalFields() suggest.Fields {
	return suggest.Fields{
		Name: sptr("Amiga Review Card"), Year: sptr(1993), Category: sptr("hw-classic"), Kind: sptr("original"),
		Status: sptr("historic"), Arch: []string{"68k"}, By: sptr("Tester"),
		What: sptr("A card used in review tests."), Why: sptr("It proves acceptance works."), Related: []string{"a2000"},
	}
}

func suggestionPath(id int64, action string) string {
	p := fmt.Sprintf("/admin/suggestions/%d", id)
	if action != "" {
		p += "/" + action
	}
	return p
}

func wantRedirect(t *testing.T, rec *httptest.ResponseRecorder, id int64, ok string) {
	t.Helper()
	want := fmt.Sprintf("/admin/suggestions/%d?ok=%s", id, ok)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Fatalf("status %d, location %q (want %q), body %s", rec.Code, rec.Header().Get("Location"), want, firstError(rec.Body.String()))
	}
}

var flashErrRe = regexp.MustCompile(`(?s)admin-flash-err" role="alert">(.*?)</p>`)

func firstError(body string) string {
	if m := flashErrRe.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	if len(body) > 300 {
		return body[:300]
	}
	return body
}

func TestReview_Unauthenticated(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer img.Close()
	env := newAdminEnvWith(t, images.WithAddrPolicy(func(netip.Addr) error { return nil }))
	id := env.propose(t, suggest.Proposal{Type: suggest.TypeImage, EntryID: "a2500", ImageURL: img.URL + "/a.png", ImageCredit: "CC0"})
	for _, path := range []string{"/admin/queue", suggestionPath(id, ""), "/admin/staged/0123456789abcdef0123456789abcdef.png"} {
		rec := env.request(t, http.MethodGet, path, nil, nil, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
			t.Errorf("GET %s: status %d, location %q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, action := range []string{"preview", "accept", "reject", "duplicate", "hide"} {
		rec := env.request(t, http.MethodPost, suggestionPath(id, action), url.Values{"rev": {"1"}}, nil, sameOrigin)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
			t.Errorf("POST %s: status %d", action, rec.Code)
		}
	}
	// A signed-in owner still needs the CSRF token.
	c := env.login(t)
	rec := env.request(t, http.MethodPost, suggestionPath(id, "preview"), url.Values{}, c.cookie, sameOrigin)
	if rec.Code != http.StatusForbidden {
		t.Errorf("preview without CSRF: status %d", rec.Code)
	}
	if hits.Load() != 0 {
		t.Errorf("image server was contacted %d times", hits.Load())
	}
	if sg := env.suggestion(t, id); sg.Status != store.StatusPending {
		t.Errorf("status %s", sg.Status)
	}
}

func TestReview_QueueAndPanels(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)
	newID := env.propose(t, suggest.Proposal{Type: suggest.TypeNew, Fields: newProposalFields(), Contact: "private@example.org"})
	changeID := env.propose(t, suggest.Proposal{Type: suggest.TypeChange, EntryID: "a2500", Fields: suggest.Fields{Year: sptr(1990)}})
	removeID := env.propose(t, suggest.Proposal{Type: suggest.TypeRemove, EntryID: "cdtv"})
	imageID := env.propose(t, suggest.Proposal{Type: suggest.TypeImage, EntryID: "a2500", ImageURL: "https://img.example.org/a.png", ImageCredit: "CC BY, Someone"})
	if _, err := env.svc.ToggleVote(t.Context(), changeID, suggest.Visitor{ID: "voter-2"}, true); err != nil {
		t.Fatal(err)
	}

	rec := c.get(t, "/admin/queue")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("queue status %d", rec.Code)
	}
	for _, id := range []int64{newID, changeID, removeID, imageID} {
		if !strings.Contains(body, fmt.Sprintf(`href="/admin/suggestions/%d"`, id)) {
			t.Errorf("queue lacks #%d", id)
		}
	}
	// Oldest first by default; most support puts the voted change first.
	if strings.Index(body, suggestionPath(newID, "")+`"`) > strings.Index(body, suggestionPath(imageID, "")+`"`) {
		t.Error("default order is not oldest first")
	}
	body = c.get(t, "/admin/queue?sort=support").Body.String()
	if strings.Index(body, suggestionPath(changeID, "")+`"`) > strings.Index(body, suggestionPath(newID, "")+`"`) {
		t.Error("support sort does not put the voted suggestion first")
	}
	body = c.get(t, "/admin/queue?type=change").Body.String()
	if !strings.Contains(body, suggestionPath(changeID, "")) || strings.Contains(body, suggestionPath(removeID, "")+`"`) {
		t.Error("type filter")
	}
	body = c.get(t, "/admin/queue?entry=cdtv").Body.String()
	if !strings.Contains(body, suggestionPath(removeID, "")) || strings.Contains(body, suggestionPath(changeID, "")+`"`) {
		t.Error("entry filter")
	}
	if body = c.get(t, "/admin/queue?status=rejected").Body.String(); !strings.Contains(body, "No suggestions match") {
		t.Error("status filter")
	}
	if !strings.Contains(c.get(t, "/admin").Body.String(), `href="/admin/queue"`) {
		t.Error("dashboard does not link the queue")
	}

	// Three panels; private contact only in the admin view.
	body = c.get(t, suggestionPath(newID, "")).Body.String()
	for _, want := range []string{"Current entry", "Proposed differences", "Final preview", "private@example.org", "Amiga Review Card", `name="weight"`, "Edit &amp; accept"} {
		if !strings.Contains(body, want) {
			t.Errorf("new review page lacks %q", want)
		}
	}
	if pub := env.request(t, http.MethodGet, fmt.Sprintf("/suggestions/%d", newID), nil, nil, nil).Body.String(); strings.Contains(pub, "private@example.org") {
		t.Error("public page shows the contact")
	}
	body = c.get(t, suggestionPath(changeID, "")).Body.String()
	if !strings.Contains(body, "1990") || !strings.Contains(body, "admin-before") {
		t.Error("change review lacks the field diff")
	}
	body = c.get(t, suggestionPath(removeID, "")).Body.String()
	if !strings.Contains(body, "lineage link") || !strings.Contains(body, `name="confirm"`) {
		t.Error("remove review lacks the dependency summary or confirmation")
	}
	body = c.get(t, suggestionPath(imageID, "")).Body.String()
	if strings.Contains(body, "<img") || !strings.Contains(body, `href="https://img.example.org/a.png"`) || !strings.Contains(body, "Preview image") {
		t.Error("image review must show the URL as a link with a preview button and no <img>")
	}
	if rec := c.get(t, "/admin/suggestions/999"); rec.Code != http.StatusNotFound {
		t.Errorf("missing suggestion: %d", rec.Code)
	}
}

func TestReview_AcceptEachType(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)
	ctx := t.Context()

	t.Run("new", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeNew, Fields: newProposalFields()})
		etag := env.catalogETag(t)
		rec := c.post(t, suggestionPath(id, "accept"), url.Values{"weight": {"3"}})
		wantRedirect(t, rec, id, store.StatusAccepted)
		if e := env.published(t, "amiga-review-card"); e == nil || e["name"] != "Amiga Review Card" || e["weight"] != 3.0 {
			t.Fatalf("published entry = %v", e)
		}
		sg := env.suggestion(t, id)
		if sg.Status != store.StatusAccepted || !strings.Contains(string(sg.Final), `"weight":3`) || strings.Contains(string(sg.Payload), "weight") {
			t.Errorf("suggestion = %s, final %s, payload %s", sg.Status, sg.Final, sg.Payload)
		}
		if !env.decisionLogged(t, id, store.StatusAccepted) {
			t.Error("no moderation row")
		}
		if env.catalogETag(t) == etag {
			t.Error("catalogue ETag unchanged")
		}
		revs, _ := env.st.Q().ListRevisions(ctx, "amiga-review-card")
		if len(revs) != 1 || revs[0].SuggestionID == nil || *revs[0].SuggestionID != id {
			t.Errorf("revisions = %+v", revs)
		}
	})

	t.Run("change", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeChange, EntryID: "a2500", Fields: suggest.Fields{Year: sptr(1990), Version: sptr("rev 6")}})
		before := env.entry(t, "a2500")
		etag := env.catalogETag(t)
		rec := c.post(t, suggestionPath(id, "accept"), url.Values{"rev": {itoa(before.Rev)}, "weight": {"2"}})
		wantRedirect(t, rec, id, store.StatusAccepted)
		if e := env.published(t, "a2500"); e["year"] != 1990.0 || e["version"] != "rev 6" {
			t.Errorf("entry = %v", e)
		}
		after := env.entry(t, "a2500")
		if after.Rev != before.Rev+1 || !strings.Contains(string(after.Doc), `"imageFit"`) {
			t.Errorf("rev %d -> %d, doc %s", before.Rev, after.Rev, after.Doc)
		}
		sg := env.suggestion(t, id)
		if sg.Status != store.StatusAccepted || !reviewSameDoc(sg.Final, after.Doc) {
			t.Errorf("status %s, final %s", sg.Status, sg.Final)
		}
		if !env.decisionLogged(t, id, store.StatusAccepted) || env.catalogETag(t) == etag {
			t.Error("missing log row or ETag change")
		}
	})

	t.Run("remove strips dependencies", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeRemove, EntryID: "cdtv"})
		before := env.entry(t, "cdtv")
		rec := c.post(t, suggestionPath(id, "accept"), url.Values{"rev": {itoa(before.Rev)}})
		if rec.Code != http.StatusUnprocessableEntity || env.suggestion(t, id).Status != store.StatusPending {
			t.Fatalf("removal without confirm: status %d", rec.Code)
		}
		rec = c.post(t, suggestionPath(id, "accept"), url.Values{"rev": {itoa(before.Rev)}, "confirm": {"1"}})
		wantRedirect(t, rec, id, store.StatusAccepted)
		doc := env.publicCatalog(t)
		if _, ok := catalogHas(t, doc, "cdtv"); ok {
			t.Error("cdtv still published")
		}
		for _, raw := range doc.Entries {
			if bytes.Contains(raw, []byte(`"cdtv"`)) {
				t.Errorf("dangling link: %s", raw)
			}
		}
		sg := env.suggestion(t, id)
		if sg.Status != store.StatusAccepted || sg.Final != nil {
			t.Errorf("status %s, final %s", sg.Status, sg.Final)
		}
		if !env.decisionLogged(t, id, store.StatusAccepted) {
			t.Error("no moderation row")
		}
	})
}

func TestReview_EditAccept(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)
	id := env.propose(t, suggest.Proposal{Type: suggest.TypeChange, EntryID: "a2500", Fields: suggest.Fields{Year: sptr(1990)}})
	orig := env.suggestion(t, id).Payload
	row := env.entry(t, "a2500")
	merged, problems := reviewMerge(env.suggestion(t, id), row.Doc, "", false)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	form := docForm(t, merged, row.Rev)
	form.Set("edit", "1")
	form.Set("name", "Amiga 2500 (owner edit)")
	form.Set("weight", "4")
	form.Set("decision_note", "Thanks, adjusted the name too.")
	rec := c.post(t, suggestionPath(id, "accept"), form)
	wantRedirect(t, rec, id, store.StatusAcceptedEdited)

	sg := env.suggestion(t, id)
	if sg.Status != store.StatusAcceptedEdited || string(sg.Payload) != string(orig) {
		t.Errorf("status %s, payload %s (was %s)", sg.Status, sg.Payload, orig)
	}
	if !strings.Contains(string(sg.Final), "owner edit") || !strings.Contains(string(sg.Final), `"year":1990`) || !strings.Contains(string(sg.Final), `"weight":4`) {
		t.Errorf("final = %s", sg.Final)
	}
	if e := env.published(t, "a2500"); e["name"] != "Amiga 2500 (owner edit)" || e["weight"] != 4.0 {
		t.Errorf("published = %v", e)
	}
	pub := env.request(t, http.MethodGet, fmt.Sprintf("/suggestions/%d", id), nil, nil, nil).Body.String()
	if !strings.Contains(pub, "Accepted with edits") || !strings.Contains(pub, "Thanks, adjusted") {
		t.Error("public page does not show the edited acceptance")
	}

	// Edit & accept with nothing changed but the owner-only weight counts as
	// a plain acceptance.
	id2 := env.propose(t, suggest.Proposal{Type: suggest.TypeChange, EntryID: "a3000", Fields: suggest.Fields{Version: sptr("rev 9")}})
	row = env.entry(t, "a3000")
	merged, _ = reviewMerge(env.suggestion(t, id2), row.Doc, "", false)
	form = docForm(t, merged, row.Rev)
	form.Set("edit", "1")
	form.Set("weight", "3")
	wantRedirect(t, c.post(t, suggestionPath(id2, "accept"), form), id2, store.StatusAccepted)
}

func TestReview_RejectDuplicateHide(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)
	etag := env.catalogETag(t)

	rej := env.propose(t, suggest.Proposal{Type: suggest.TypeChange, EntryID: "a2500", Fields: suggest.Fields{Year: sptr(1991)}})
	wantRedirect(t, c.post(t, suggestionPath(rej, "reject"), url.Values{"decision_note": {"Sources disagree."}, "internal_note": {"checked manual"}}), rej, store.StatusRejected)
	sg := env.suggestion(t, rej)
	if sg.Status != store.StatusRejected || sg.DecisionNote != "Sources disagree." || sg.InternalNote != "checked manual" || sg.Final != nil {
		t.Errorf("rejected = %+v", sg)
	}
	if !env.decisionLogged(t, rej, store.StatusRejected) {
		t.Error("no moderation row for reject")
	}
	pub := env.request(t, http.MethodGet, fmt.Sprintf("/suggestions/%d", rej), nil, nil, nil).Body.String()
	if !strings.Contains(pub, "Sources disagree.") || strings.Contains(pub, "checked manual") {
		t.Error("public page: decision note missing or internal note leaked")
	}
	if rec := c.post(t, suggestionPath(rej, "reject"), nil); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Already decided") {
		t.Errorf("second reject: %d", rec.Code)
	}

	// Duplicates: the original must exist, differ and not be a duplicate.
	canonical := env.propose(t, suggest.Proposal{Type: suggest.TypeRemove, EntryID: "a2500"})
	dup := env.propose(t, suggest.Proposal{Type: suggest.TypeRemove, EntryID: "a2500"})
	dup2 := env.propose(t, suggest.Proposal{Type: suggest.TypeRemove, EntryID: "a2500"})
	for _, v := range []string{"", "abc", itoa(dup), "9999"} {
		if rec := c.post(t, suggestionPath(dup, "duplicate"), url.Values{"duplicate_of": {v}}); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("duplicate_of=%q: status %d", v, rec.Code)
		}
	}
	for _, v := range []string{"voter-a", "voter-b"} {
		if _, err := env.svc.ToggleVote(t.Context(), dup, suggest.Visitor{ID: v}, true); err != nil {
			t.Fatal(err)
		}
	}
	wantRedirect(t, c.post(t, suggestionPath(dup, "duplicate"), url.Values{"duplicate_of": {"#" + itoa(canonical)}}), dup, store.StatusDuplicate)
	sg = env.suggestion(t, dup)
	if sg.Status != store.StatusDuplicate || sg.DuplicateOf == nil || *sg.DuplicateOf != canonical || sg.Votes != 2 {
		t.Errorf("duplicate = %+v", sg)
	}
	if env.suggestion(t, canonical).Votes != 0 {
		t.Error("votes were merged into the original")
	}
	if rec := c.post(t, suggestionPath(dup2, "duplicate"), url.Values{"duplicate_of": {itoa(dup)}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("duplicate of a duplicate: %d", rec.Code)
	}

	// Hide works from any status, here an already rejected one.
	wantRedirect(t, c.post(t, suggestionPath(rej, "hide"), url.Values{"internal_note": {"spam wave"}}), rej, store.StatusHidden)
	if sg := env.suggestion(t, rej); sg.Status != store.StatusHidden || sg.InternalNote != "spam wave" {
		t.Errorf("hidden = %+v", sg)
	}
	if !env.decisionLogged(t, rej, store.StatusHidden) {
		t.Error("no moderation row for hide")
	}
	if rec := env.request(t, http.MethodGet, fmt.Sprintf("/suggestions/%d", rej), nil, nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("hidden public detail: %d", rec.Code)
	}
	api := env.request(t, http.MethodGet, "/api/suggestions?status=all", nil, nil, nil).Body.String()
	if strings.Contains(api, fmt.Sprintf(`"id":%d,`, rej)) || !strings.Contains(api, fmt.Sprintf(`"id":%d,`, dup)) {
		t.Errorf("api list: %s", api)
	}
	if rec := c.get(t, suggestionPath(rej, "")); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spam wave") {
		t.Errorf("admin view of hidden: %d", rec.Code)
	}
	if !strings.Contains(c.get(t, "/admin/queue?status=hidden").Body.String(), suggestionPath(rej, "")) {
		t.Error("hidden filter")
	}
	settings := c.get(t, "/admin/settings").Body.String()
	if !strings.Contains(settings, "Hidden spam") || !strings.Contains(settings, suggestionPath(rej, "")) {
		t.Error("settings lacks the hidden list")
	}
	if env.catalogETag(t) != etag {
		t.Error("decisions without publication changed the catalogue")
	}
}

func TestReview_DoubleAcceptAndStale(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)
	ctx := t.Context()

	t.Run("double accept", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeChange, EntryID: "a2500", Fields: suggest.Fields{Version: sptr("rev 4")}})
		rev := env.entry(t, "a2500").Rev
		revs := env.revisionCount(t, "a2500")
		form := url.Values{"rev": {itoa(rev)}, "weight": {"2"}}
		wantRedirect(t, c.post(t, suggestionPath(id, "accept"), form), id, store.StatusAccepted)
		rec := c.post(t, suggestionPath(id, "accept"), form)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Already decided") {
			t.Errorf("second accept: %d %s", rec.Code, firstError(rec.Body.String()))
		}
		if got := env.revisionCount(t, "a2500"); got != revs+1 {
			t.Errorf("revisions %d -> %d", revs, got)
		}
		// The in-transaction guard: a decision recorded meanwhile rolls the
		// publication back.
		id2 := env.propose(t, suggest.Proposal{Type: suggest.TypeNew, Fields: newProposalFields()})
		sg := env.suggestion(t, id2)
		_, err := env.cat.Create(ctx, mustMerge(t, sg), adminActor, "", &sg.ID,
			reviewDecideHook(sg, "amiga-review-card", store.Decision{Status: store.StatusAccepted, At: 1}),
			reviewDecideHook(sg, "amiga-review-card", store.Decision{Status: store.StatusAccepted, At: 1}))
		if err == nil || !strings.Contains(err.Error(), store.ErrNotPending.Error()) {
			t.Fatalf("double hook error = %v", err)
		}
		if _, err := env.st.Q().GetEntry(ctx, "amiga-review-card"); err == nil {
			t.Error("rolled-back publication left the entry")
		}
		if env.suggestion(t, id2).Status != store.StatusPending {
			t.Error("rolled-back decision kept")
		}
	})

	t.Run("stale change needs review", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeChange, EntryID: "a3000", Fields: suggest.Fields{Year: sptr(1991)}})
		// The owner edits the entry directly after the proposal.
		form := entryForm(t, env, "a3000")
		form.Set("by", "Commodore (owner)")
		if rec := c.post(t, "/admin/entries/a3000", form); rec.Code != http.StatusSeeOther {
			t.Fatalf("direct edit: %d", rec.Code)
		}
		cur := env.entry(t, "a3000")
		page := c.get(t, suggestionPath(id, "")).Body.String()
		if !strings.Contains(page, "Conflict: the entry changed") || !strings.Contains(page, `name="reviewed"`) || !strings.Contains(page, "Commodore (owner)") {
			t.Error("review page lacks the conflict notice")
		}
		accept := url.Values{"rev": {itoa(cur.Rev)}, "weight": {"4"}}
		rec := c.post(t, suggestionPath(id, "accept"), accept)
		if rec.Code != http.StatusConflict || env.entry(t, "a3000").Rev != cur.Rev || env.suggestion(t, id).Status != store.StatusPending {
			t.Fatalf("stale accept without review: %d", rec.Code)
		}
		accept.Set("reviewed", "1")
		wantRedirect(t, c.post(t, suggestionPath(id, "accept"), accept), id, store.StatusAccepted)
		if e := env.published(t, "a3000"); e["year"] != 1991.0 || e["by"] != "Commodore (owner)" {
			t.Errorf("merged entry = %v", e)
		}
	})
}

func mustMerge(t *testing.T, sg store.Suggestion) json.RawMessage {
	t.Helper()
	doc, problems := reviewMerge(sg, nil, "2", false)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return doc
}

func reviewPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	img.Set(3, 3, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var stagedSrcRe = regexp.MustCompile(`src="/admin/staged/([0-9a-f]{32}\.png)"`)

func TestReview_ImageSuggestions(t *testing.T) {
	t.Parallel()
	pngData := reviewPNG(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/a.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngData)
		case "/page.html":
			_, _ = w.Write([]byte("<html>not an image</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	env := newAdminEnvWith(t, images.WithAddrPolicy(func(a netip.Addr) error {
		if a.IsLoopback() {
			return nil
		}
		return images.DefaultAddrPolicy(a)
	}))
	c := env.login(t)
	staging := env.cfg.StagingDir()

	t.Run("preview then accept", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeImage, EntryID: "a2500", ImageURL: srv.URL + "/a.png",
			ImageCredit: "Photo: Tester, CC0", ImageSourcePage: "https://example.org/photos"})
		if hits.Load() != 0 {
			t.Fatal("proposal fetched the image")
		}
		wantRedirect(t, c.post(t, suggestionPath(id, "preview"), nil), id, "staged")
		page := c.get(t, suggestionPath(id, "")).Body.String()
		m := stagedSrcRe.FindStringSubmatch(page)
		if m == nil || !strings.Contains(page, "40×30") {
			t.Fatalf("review page lacks the staged preview")
		}
		rec := c.get(t, "/admin/staged/"+m[1])
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), pngData) {
			t.Errorf("staged file: %d %s", rec.Code, rec.Header().Get("Content-Type"))
		}
		if rec := env.request(t, http.MethodGet, "/admin/staged/"+m[1], nil, nil, nil); rec.Code != http.StatusSeeOther {
			t.Errorf("anonymous staged: %d", rec.Code)
		}
		if rec := env.request(t, http.MethodGet, "/images/../staging/"+m[1], nil, nil, nil); rec.Code == http.StatusOK {
			t.Error("staged file reachable through /images")
		}
		if rec := c.get(t, "/admin/staged/..%2Famiga.db"); rec.Code != http.StatusNotFound {
			t.Errorf("traversal: %d", rec.Code)
		}

		before := env.entry(t, "a2500")
		etag := env.catalogETag(t)
		wantRedirect(t, c.post(t, suggestionPath(id, "accept"), url.Values{"rev": {itoa(before.Rev)}}), id, store.StatusAccepted)
		e := env.published(t, "a2500")
		imgPath, _ := e["image"].(string)
		if !strings.HasPrefix(imgPath, "images/suggested/a2500-") || !strings.HasSuffix(imgPath, ".png") || e["imageCredit"] != "Photo: Tester, CC0" {
			t.Errorf("entry image %q credit %v", imgPath, e["imageCredit"])
		}
		if _, err := os.Stat(filepath.Join(env.cfg.MediaDir(), strings.TrimPrefix(imgPath, "images/"))); err != nil {
			t.Errorf("published file: %v", err)
		}
		if rec := env.request(t, http.MethodGet, "/"+imgPath, nil, nil, nil); rec.Code != http.StatusOK {
			t.Errorf("published image served: %d", rec.Code)
		}
		ims, err := env.st.Q().ListImagesByEntry(t.Context(), "a2500")
		if err != nil || len(ims) != 1 || ims[0].SuggestionID == nil || *ims[0].SuggestionID != id ||
			ims[0].SourceURL != srv.URL+"/a.png" || ims[0].SourcePage != "https://example.org/photos" {
			t.Errorf("images = %+v, %v", ims, err)
		}
		sg := env.suggestion(t, id)
		var final map[string]string
		_ = json.Unmarshal(sg.Final, &final)
		if sg.Status != store.StatusAccepted || final["image"] != imgPath || final["imageCredit"] != "Photo: Tester, CC0" {
			t.Errorf("status %s final %s", sg.Status, sg.Final)
		}
		if !env.decisionLogged(t, id, store.StatusAccepted) || env.catalogETag(t) == etag {
			t.Error("missing log row or ETag change")
		}
		if left, _ := os.ReadDir(staging); len(left) != 0 {
			t.Errorf("staging not empty: %v", left)
		}
		if rec := c.get(t, "/admin/staged/"+m[1]); rec.Code != http.StatusNotFound {
			t.Errorf("published staged file still served: %d", rec.Code)
		}
	})

	t.Run("failed fetch keeps pending", func(t *testing.T) {
		for _, path := range []string{"/missing.png", "/page.html"} {
			id := env.propose(t, suggest.Proposal{Type: suggest.TypeImage, EntryID: "a3000", ImageURL: srv.URL + path, ImageCredit: "CC0"})
			before := env.entry(t, "a3000")
			rec := c.post(t, suggestionPath(id, "preview"), nil)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Image not fetched") {
				t.Errorf("%s preview: %d", path, rec.Code)
			}
			rec = c.post(t, suggestionPath(id, "accept"), url.Values{"rev": {itoa(before.Rev)}})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s accept: %d", path, rec.Code)
			}
			if sg := env.suggestion(t, id); sg.Status != store.StatusPending {
				t.Errorf("%s status %s", path, sg.Status)
			}
			if after := env.entry(t, "a3000"); after.Rev != before.Rev {
				t.Errorf("%s changed the entry", path)
			}
		}
	})

	t.Run("failed publication leaves nothing public", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeImage, EntryID: "a3000", ImageURL: srv.URL + "/a.png", ImageCredit: "CC0"})
		// The handler loaded the entry, then another tab edited it: the rev
		// check before the fetch passes, the catalogue update is stale.
		live := env.entry(t, "a3000")
		form := entryForm(t, env, "a3000")
		form.Set("by", "Commodore (other tab)")
		if rec := c.post(t, "/admin/entries/a3000", form); rec.Code != http.StatusSeeOther {
			t.Fatalf("direct edit: %d", rec.Code)
		}
		sg := env.suggestion(t, id)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, suggestionPath(id, "accept"), nil)
		rec := httptest.NewRecorder()
		d := reviewDecision(req, store.StatusAccepted)
		if env.srv.reviewAcceptImage(rec, req, sg, live, live.Rev, false, &d, "accepted") {
			t.Fatal("stale image accept succeeded")
		}
		if rec.Code != http.StatusConflict {
			t.Errorf("stale image accept: %d", rec.Code)
		}
		published, _ := filepath.Glob(filepath.Join(env.cfg.MediaDir(), "suggested", "a3000-*"))
		if len(published) != 0 {
			t.Errorf("orphaned public files: %v", published)
		}
		if ims, err := env.st.Q().ListImagesByEntry(t.Context(), "a3000"); err != nil || len(ims) != 0 {
			t.Errorf("orphaned images rows: %+v, %v", ims, err)
		}
		if env.suggestion(t, id).Status != store.StatusPending {
			t.Error("suggestion decided by a failed publication")
		}
	})

	t.Run("reject discards the staged file", func(t *testing.T) {
		id := env.propose(t, suggest.Proposal{Type: suggest.TypeImage, EntryID: "a3000", ImageURL: srv.URL + "/a.png", ImageCredit: "CC0"})
		wantRedirect(t, c.post(t, suggestionPath(id, "preview"), nil), id, "staged")
		if left, _ := os.ReadDir(staging); len(left) != 1 {
			t.Fatalf("staging = %v", left)
		}
		wantRedirect(t, c.post(t, suggestionPath(id, "reject"), nil), id, store.StatusRejected)
		if left, _ := os.ReadDir(staging); len(left) != 0 {
			t.Errorf("staging not empty after reject: %v", left)
		}
	})
}
