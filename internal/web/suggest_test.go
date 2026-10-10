package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
	"amigaconstellation/internal/turnstile"
)

// fakeVerifier accepts the token "ok" for the expected action.
type fakeVerifier struct{ calls atomic.Int32 }

func (f *fakeVerifier) Verify(_ context.Context, token, _, action string) error {
	f.calls.Add(1)
	if token != "ok" || (action != turnstile.ActionSuggest && action != turnstile.ActionVote) {
		return turnstile.ErrFailed
	}
	return nil
}

type suggestEnv struct {
	srv   *httptest.Server
	st    *store.Store
	clock *atomic.Int64 // unix seconds
}

func newSuggestEnv(t *testing.T, cfg config.Config) *suggestEnv {
	t.Helper()
	cfg.Addr, cfg.DataDir, cfg.Env = "127.0.0.1:0", t.TempDir(), config.EnvDevelopment
	cfg.Secret = []byte(strings.Repeat("s", 32))
	cfg.TurnstileSiteKey = "test-site-key"
	if cfg.RateSuggestPerDay == 0 {
		cfg.RateSuggestPerDay, cfg.RateSuggestPerDayIP, cfg.RateVotePerHour, cfg.RateVotePerHourIP = 5, 25, 30, 150
	}
	if cfg.RateVotePerSuggestionIP == 0 {
		cfg.RateVotePerSuggestionIP = 100
	}
	st, err := store.Open(cfg.DBPath())
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
	clock := &atomic.Int64{}
	clock.Store(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0) }
	svc := suggest.New(st, cat, cfg, now)
	srv, err := New(cfg, st, cat, slog.New(slog.NewTextHandler(io.Discard, nil)), WithSuggest(svc, &fakeVerifier{}, true))
	if err != nil {
		t.Fatal(err)
	}
	srv.suggest.now = now
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &suggestEnv{srv: ts, st: st, clock: clock}
}

func (e *suggestEnv) advance(d time.Duration) { e.clock.Add(int64(d / time.Second)) }

// browser is a cookie-keeping client that does not follow redirects.
func (e *suggestEnv) browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type resp struct {
	code   int
	body   string
	header http.Header
}

func (e *suggestEnv) send(t *testing.T, c *http.Client, method, path string, body io.Reader, header map[string]string) resp {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, e.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp{code: res.StatusCode, body: string(b), header: res.Header}
}

var formTSRe = regexp.MustCompile(`name="form_ts" value="([^"]+)"`)

// openForm loads the form (which sets the cookie) and returns its token.
func (e *suggestEnv) openForm(t *testing.T, c *http.Client) string {
	t.Helper()
	r := e.send(t, c, http.MethodGet, "/suggestions/new", nil, nil)
	m := formTSRe.FindStringSubmatch(r.body)
	if r.code != http.StatusOK || m == nil {
		t.Fatalf("GET /suggestions/new = %d, form_ts found %v", r.code, m != nil)
	}
	return m[1]
}

func (e *suggestEnv) postForm(t *testing.T, c *http.Client, form url.Values, origin string) resp {
	t.Helper()
	if origin == "" {
		origin = e.srv.URL
	}
	return e.send(t, c, http.MethodPost, "/suggestions/new", strings.NewReader(form.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded", "Origin": origin,
	})
}

func newEntryForm(ts string) url.Values {
	return url.Values{
		"type": {"new"}, "f_name": {"Amiga Test Card"}, "f_year": {"1993"}, "f_category": {"hw-classic"},
		"f_kind": {"original"}, "f_status": {"historic"}, "f_arch": {"68k"}, "f_what": {"A test card."},
		"f_why": {"It proves the form works."}, "f_related": {"a2000"}, "reason": {"Missing"},
		"sources": {"https://example.org/card\nhttps://example.org/two"}, "contact": {"secret@example.org"},
		"form_ts": {ts}, "cf-turnstile-response": {"ok"},
	}
}

var createdRe = regexp.MustCompile(`^/suggestions/(\d+)\?created=1$`)

func TestSuggest_FormSubmit_EachType(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	c := e.browser(t)
	ts := e.openForm(t, c)
	e.advance(10 * time.Second)

	change := url.Values{"type": {"change"}, "entry": {"a2500"}, "f_name": {"Amiga 2500"}, "f_year": {"1990"},
		"f_category": {"hw-classic"}, "f_kind": {"original"}, "f_status": {"historic"}, "f_arch": {"68k"},
		"f_by": {"Commodore"}, "f_what": {"Factory A2000 configurations bundled with an A2620 (68020) or A2630 (68030) accelerator and SCSI."},
		"f_why":     {"It formalised the community's favourite idea: start with an expandable Amiga and grow it into a workstation."},
		"f_related": {"a2000"}, "f_tags": {"bigbox"}, "reason": {"Year"}, "form_ts": {ts}, "cf-turnstile-response": {"ok"}}
	forms := map[string]url.Values{
		"new":    newEntryForm(ts),
		"change": change,
		"remove": {"type": {"remove"}, "entry": {"a2500"}, "reason": {"Not a model"}, "form_ts": {ts}, "cf-turnstile-response": {"ok"}},
		"image": {"type": {"image"}, "entry": {"a2500"}, "image_url": {"https://img.example/a2500.jpg"}, "image_credit": {"CC0"},
			"reason": {"Better photo"}, "form_ts": {ts}, "cf-turnstile-response": {"ok"}},
	}
	ids := map[string]string{}
	for typ, form := range forms {
		r := e.postForm(t, c, form, "")
		m := createdRe.FindStringSubmatch(r.header.Get("Location"))
		if r.code != http.StatusSeeOther || m == nil {
			t.Fatalf("%s: POST = %d %q\n%s", typ, r.code, r.header.Get("Location"), firstErrors(r.body))
		}
		ids[typ] = m[1]
	}

	list := e.send(t, c, http.MethodGet, "/suggestions", nil, nil)
	if list.code != http.StatusOK || strings.Count(list.body, `class="sg-tr" role="row"`) != 4 {
		t.Fatalf("list: %d, rows = %d", list.code, strings.Count(list.body, `class="sg-tr" role="row"`))
	}
	for typ, id := range ids {
		d := e.send(t, c, http.MethodGet, "/suggestions/"+id, nil, nil)
		if d.code != http.StatusOK || !strings.Contains(d.body, "Pending") {
			t.Errorf("%s detail = %d", typ, d.code)
		}
		if strings.Contains(d.body, "secret@example.org") {
			t.Errorf("%s detail leaks contact", typ)
		}
		if typ == "image" {
			if strings.Contains(d.body, "<img") || !strings.Contains(d.body, `href="https://img.example/a2500.jpg"`) {
				t.Errorf("image detail must link, not embed, the image")
			}
		}
		if typ == "change" && (!strings.Contains(d.body, "1989") || !strings.Contains(d.body, "1990")) {
			t.Errorf("change detail lacks the old/new year")
		}
	}
	api := e.send(t, c, http.MethodGet, "/api/suggestions", nil, nil)
	if strings.Contains(api.body, "secret@example.org") || strings.Contains(api.body, "ip_hash") {
		t.Errorf("API leaks private fields: %s", api.body)
	}
}

func firstErrors(body string) string {
	if i := strings.Index(body, `class="sg-errors"`); i >= 0 {
		return body[i:min(len(body), i+600)]
	}
	return ""
}

func TestSuggest_FormRejections(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	tests := []struct {
		name   string
		mutate func(url.Values)
		origin string
		wait   time.Duration
		code   int
		want   string
	}{
		{"honeypot", func(f url.Values) { f.Set("website", "http://spam") }, "", 10 * time.Second, http.StatusBadRequest, "could not be accepted"},
		{"too fast", nil, "", time.Second, http.StatusBadRequest, "That was quick"},
		{"expired", nil, "", 25 * time.Hour, http.StatusBadRequest, "expired"},
		{"forged token", func(f url.Values) { f.Set("form_ts", "1.AAAA") }, "", 10 * time.Second, http.StatusBadRequest, "incomplete"},
		{"turnstile failure", func(f url.Values) { f.Set("cf-turnstile-response", "bad") }, "", 10 * time.Second, http.StatusForbidden, "anti-spam check failed"},
		{"validation", func(f url.Values) { f.Set("f_category", "nope") }, "", 10 * time.Second, http.StatusUnprocessableEntity, "Unknown category"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// not parallel: subtests share the clock
			c := e.browser(t)
			form := newEntryForm(e.openForm(t, c))
			form.Set("f_name", "Preserved Name "+tt.name)
			if tt.mutate != nil {
				tt.mutate(form)
			}
			e.advance(tt.wait)
			r := e.postForm(t, c, form, tt.origin)
			if r.code != tt.code || !strings.Contains(r.body, tt.want) {
				t.Fatalf("POST = %d, want %d containing %q\n%s", r.code, tt.code, tt.want, firstErrors(r.body))
			}
			if !strings.Contains(r.body, `value="Preserved Name `+tt.name+`"`) || !strings.Contains(r.body, "A test card.") {
				t.Error("input not preserved")
			}
			if !strings.Contains(r.body, `data-sitekey="test-site-key"`) || !formTSRe.MatchString(r.body) {
				t.Error("re-rendered form lacks a fresh widget or token")
			}
		})
	}
	list := e.send(t, e.browser(t), http.MethodGet, "/api/suggestions", nil, nil)
	if !strings.Contains(list.body, `"total":0`) {
		t.Errorf("rejected submissions were stored: %s", list.body)
	}
}

func TestSuggest_MutationsRequireOriginAndCookie(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	c := e.browser(t)
	ts := e.openForm(t, c)
	e.advance(10 * time.Second)

	if r := e.postForm(t, c, newEntryForm(ts), "https://evil.example"); r.code != http.StatusForbidden {
		t.Errorf("cross-origin form POST = %d", r.code)
	}
	r := e.send(t, c, http.MethodPost, "/suggestions/new", strings.NewReader(newEntryForm(ts).Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "cross-site"})
	if r.code != http.StatusForbidden {
		t.Errorf("Sec-Fetch-Site cross-site = %d", r.code)
	}
	if r := e.send(t, c, http.MethodPost, "/suggestions/new", strings.NewReader(newEntryForm(ts).Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); r.code != http.StatusForbidden {
		t.Errorf("POST without Origin = %d", r.code)
	}
	noCookie := &http.Client{CheckRedirect: c.CheckRedirect}
	if r := e.postForm(t, noCookie, newEntryForm(ts), ""); r.code != http.StatusForbidden || !strings.Contains(r.body, "cookie") {
		t.Errorf("POST without cookie = %d", r.code)
	}

	// The JSON create endpoint is gone; suggestions come only through the form.
	if r := e.send(t, c, http.MethodPost, "/api/suggestions", strings.NewReader(`{"type":"remove"}`), map[string]string{
		"Content-Type": "application/json", "Origin": e.srv.URL, "X-Requested-With": "fetch",
	}); r.code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/suggestions = %d, want 405", r.code)
	}
}

func TestSuggest_VisitorCookie(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	c := e.browser(t)
	r := e.send(t, c, http.MethodGet, "/suggestions", nil, nil)
	set := r.header.Get("Set-Cookie")
	if !strings.HasPrefix(set, visitorCookie+"=") || !strings.Contains(set, "HttpOnly") || !strings.Contains(set, "SameSite=Lax") || strings.Contains(set, "Secure") {
		t.Errorf("Set-Cookie = %q", set)
	}
	if r := e.send(t, c, http.MethodGet, "/suggestions", nil, nil); r.header.Get("Set-Cookie") != "" {
		t.Error("cookie re-issued although valid")
	}
	if r := e.send(t, c, http.MethodGet, "/api/suggestions", nil, nil); r.header.Get("Set-Cookie") != "" {
		t.Error("API GET set a cookie")
	}
}

type voteResp struct {
	Votes int  `json:"votes"`
	Voted bool `json:"voted"`
}

func (e *suggestEnv) vote(t *testing.T, c *http.Client, id int64, on bool) (int, voteResp) {
	t.Helper()
	method := http.MethodPut
	if !on {
		method = http.MethodDelete
	}
	r := e.send(t, c, method, fmt.Sprintf("/api/suggestions/%d/vote", id), nil, map[string]string{
		"Origin": e.srv.URL, "X-Requested-With": "fetch", "X-Turnstile-Token": "ok",
	})
	var v voteResp
	_ = json.Unmarshal([]byte(r.body), &v)
	return r.code, v
}

// create submits a remove proposal through the HTML form and returns its id.
func (e *suggestEnv) create(t *testing.T, c *http.Client, entry, reason string) int64 {
	t.Helper()
	r := e.submitRemove(t, c, entry, reason)
	m := createdRe.FindStringSubmatch(r.header.Get("Location"))
	if r.code != http.StatusSeeOther || m == nil {
		t.Fatalf("create = %d %s", r.code, firstErrors(r.body))
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *suggestEnv) submitRemove(t *testing.T, c *http.Client, entry, reason string) resp {
	t.Helper()
	ts := e.openForm(t, c)
	e.advance(10 * time.Second)
	return e.postForm(t, c, url.Values{"type": {"remove"}, "entry": {entry}, "reason": {reason},
		"form_ts": {ts}, "cf-turnstile-response": {"ok"}}, "")
}

func TestSuggest_Votes(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	c := e.browser(t)
	e.send(t, c, http.MethodGet, "/suggestions", nil, nil) // cookie
	id := e.create(t, c, "a2500", "r")

	if code, v := e.vote(t, c, id, true); code != http.StatusOK || v.Votes != 1 || !v.Voted {
		t.Fatalf("vote on = %d %+v", code, v)
	}
	if code, v := e.vote(t, c, id, false); code != http.StatusOK || v.Votes != 0 || v.Voted {
		t.Fatalf("vote off = %d %+v", code, v)
	}

	// 20 concurrent +1s from one browser count once; 20 browsers count 20.
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { e.vote(t, c, id, true) })
	}
	wg.Wait()
	if _, v := e.vote(t, c, id, true); v.Votes != 1 {
		t.Fatalf("same browser x20: votes = %d", v.Votes)
	}
	others := make([]*http.Client, 20)
	for i := range others {
		others[i] = e.browser(t)
		e.send(t, others[i], http.MethodGet, "/suggestions", nil, nil)
	}
	for _, o := range others {
		wg.Go(func() {
			if code, _ := e.vote(t, o, id, true); code != http.StatusOK {
				t.Errorf("vote = %d", code)
			}
		})
	}
	wg.Wait()
	if _, v := e.vote(t, c, id, true); v.Votes != 21 {
		t.Fatalf("21 browsers: votes = %d", v.Votes)
	}
	page := e.send(t, c, http.MethodGet, fmt.Sprintf("/suggestions/%d", id), nil, nil)
	if !strings.Contains(page.body, `aria-pressed="true"`) {
		t.Error("detail page does not show the visitor's +1")
	}

	if err := e.st.Q().DecideSuggestion(t.Context(), id, store.Decision{Status: store.StatusAccepted, At: 1}); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.vote(t, c, id, false); code != http.StatusConflict {
		t.Errorf("unvote after resolution = %d", code)
	}
	if list := e.send(t, c, http.MethodGet, "/api/suggestions", nil, nil); !strings.Contains(list.body, `"votes":21`) {
		t.Errorf("resolved total not kept: %s", list.body)
	}
	if code, _ := e.vote(t, c, 999, true); code != http.StatusNotFound {
		t.Errorf("vote on missing = %d", code)
	}
	noCookie := &http.Client{}
	if code, _ := e.vote(t, noCookie, id, true); code != http.StatusForbidden {
		t.Errorf("vote without cookie = %d", code)
	}
}

func TestSuggest_HiddenAndDuplicate(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	c := e.browser(t)
	e.send(t, c, http.MethodGet, "/suggestions", nil, nil)
	canonical := e.create(t, c, "a2500", "r")
	dup := e.create(t, c, "a2500", "r")
	spam := e.create(t, c, "a2000", "BUY PILLS")
	q := e.st.Q()
	if err := q.DecideSuggestion(t.Context(), dup, store.Decision{Status: store.StatusDuplicate, DuplicateOf: &canonical, At: 1}); err != nil {
		t.Fatal(err)
	}
	if err := q.HideSuggestion(t.Context(), spam, "", 1); err != nil {
		t.Fatal(err)
	}
	if r := e.send(t, c, http.MethodGet, fmt.Sprintf("/suggestions/%d", spam), nil, nil); r.code != http.StatusNotFound {
		t.Errorf("hidden detail = %d", r.code)
	}
	for _, path := range []string{"/suggestions", "/api/suggestions", "/api/entries/search?q=amiga+2000"} {
		if r := e.send(t, c, http.MethodGet, path, nil, nil); strings.Contains(r.body, "BUY PILLS") || strings.Contains(r.body, fmt.Sprintf(`"id":%d,`, spam)) {
			t.Errorf("%s shows the hidden suggestion", path)
		}
	}
	r := e.send(t, c, http.MethodGet, fmt.Sprintf("/suggestions/%d", dup), nil, nil)
	if !strings.Contains(r.body, fmt.Sprintf(`href="/suggestions/%d"`, canonical)) {
		t.Error("duplicate page lacks a link to the canonical suggestion")
	}
}

func TestSuggest_PausesAndRateLimits(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{RateSuggestPerDay: 1, RateSuggestPerDayIP: 10, RateVotePerHour: 30, RateVotePerHourIP: 150})
	c := e.browser(t)
	e.send(t, c, http.MethodGet, "/suggestions", nil, nil)
	id := e.create(t, c, "a2500", "r")
	if r := e.submitRemove(t, c, "a2500", "r"); r.code != http.StatusTooManyRequests {
		t.Errorf("second suggestion = %d, want 429", r.code)
	}

	q := e.st.Q()
	if err := q.SetSetting(t.Context(), store.SettingSubmissionsPaused, "1"); err != nil {
		t.Fatal(err)
	}
	if err := q.SetSetting(t.Context(), store.SettingVotingPaused, "1"); err != nil {
		t.Fatal(err)
	}
	other := e.browser(t)
	ts := e.openForm(t, other)
	e.advance(10 * time.Second)
	if r := e.postForm(t, other, newEntryForm(ts), ""); r.code != http.StatusServiceUnavailable || !strings.Contains(r.body, "paused") {
		t.Errorf("form while paused = %d", r.code)
	}
	if code, _ := e.vote(t, other, id, true); code != http.StatusServiceUnavailable {
		t.Errorf("vote while paused = %d", code)
	}
	if r := e.send(t, other, http.MethodGet, "/suggestions", nil, nil); !strings.Contains(r.body, "Voting is paused") {
		t.Error("list page does not announce the voting pause")
	}
}

func TestSuggest_EntrySearch(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	r := e.send(t, e.browser(t), http.MethodGet, "/api/entries/search?q=amiga%202500", nil, nil)
	var h suggest.Hints
	if err := json.Unmarshal([]byte(r.body), &h); err != nil || len(h.Entries) == 0 || h.Entries[0].ID != "a2500" {
		t.Errorf("search = %d %s (%v)", r.code, r.body, err)
	}
}

func TestSuggest_EntrySearchRateLimit(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	c := e.browser(t)
	e.send(t, c, http.MethodGet, "/suggestions", nil, nil) // cookie
	for i := range searchPerVisitor {
		if r := e.send(t, c, http.MethodGet, "/api/entries/search?q=a500", nil, nil); r.code != http.StatusOK {
			t.Fatalf("search %d = %d", i, r.code)
		}
	}
	if r := e.send(t, c, http.MethodGet, "/api/entries/search?q=a500", nil, nil); r.code != http.StatusTooManyRequests {
		t.Errorf("search over the limit = %d, want 429", r.code)
	}
	// Another browser on the same IP still has its own allowance.
	if r := e.send(t, e.browser(t), http.MethodGet, "/api/entries/search?q=a500", nil, nil); r.code != http.StatusOK {
		t.Errorf("other browser = %d", r.code)
	}
	e.advance(searchWindow)
	if r := e.send(t, c, http.MethodGet, "/api/entries/search?q=a500", nil, nil); r.code != http.StatusOK {
		t.Errorf("after the window = %d", r.code)
	}
}

func TestWindowLimiter_IPCeiling(t *testing.T) {
	t.Parallel()
	l := newWindowLimiter(time.Minute)
	now := time.Unix(1000, 0)
	for i := range 3 {
		if !l.allow(now, keyLimit{"ip:x", 3}, keyLimit{fmt.Sprintf("v:%d", i), 60}) {
			t.Fatalf("request %d refused", i)
		}
	}
	if l.allow(now, keyLimit{"ip:x", 3}, keyLimit{"v:fresh", 60}) {
		t.Error("fresh browser id bypassed the IP ceiling")
	}
}

func TestSuggest_FormPrefill(t *testing.T) {
	t.Parallel()
	e := newSuggestEnv(t, config.Config{})
	r := e.send(t, e.browser(t), http.MethodGet, "/suggestions/new?type=change&entry=a2500", nil, nil)
	for _, want := range []string{`value="Amiga 2500"`, `value="1989"`, `value="change" checked`, `value="a2000" checked`} {
		if !strings.Contains(r.body, want) {
			t.Errorf("prefilled form lacks %s", want)
		}
	}
}

func TestSuggest_sameOriginMutation(t *testing.T) {
	t.Parallel()
	s := &Server{cfg: config.Config{BaseURL: "https://amiga.example"}}
	tests := []struct {
		name   string
		header map[string]string
		want   bool
	}{
		{"matching origin", map[string]string{"Origin": "https://amiga.example"}, true},
		{"other origin", map[string]string{"Origin": "https://evil.example"}, false},
		{"null origin", map[string]string{"Origin": "null"}, false},
		{"same-origin fetch metadata only", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"same-site", map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://amiga.example"}, false},
		{"no headers", nil, false},
	}
	// The owner panel also accepts header-less requests (its CSRF token is
	// checked separately), and nothing else changes.
	headerless := map[string]bool{"no headers": true, "same-origin fetch metadata only": true, "matching origin": true}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/suggestions/new", nil)
			for k, v := range tt.header {
				r.Header.Set(k, v)
			}
			if got := s.sameOriginMutation(r, false); got != tt.want {
				t.Errorf("sameOriginMutation(false) = %v, want %v", got, tt.want)
			}
			if got := s.sameOriginMutation(r, allowHeaderless); got != headerless[tt.name] {
				t.Errorf("sameOriginMutation(allowHeaderless) = %v, want %v", got, headerless[tt.name])
			}
		})
	}
}

func TestRateIP(t *testing.T) {
	t.Parallel()
	if got := rateIP("2001:db8:1:2:3:4:5:6"); got != "2001:db8:1:2::/64" {
		t.Errorf("rateIP(v6) = %q", got)
	}
	if got := rateIP("203.0.113.7"); got != "203.0.113.7" {
		t.Errorf("rateIP(v4) = %q", got)
	}
}
