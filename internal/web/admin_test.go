package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"amigaconstellation/internal/auth"
	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/config"
	"amigaconstellation/internal/images"
	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
)

const adminTestPassword = "test passphrase 42"

type adminClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *adminClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *adminClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type adminEnv struct {
	h      http.Handler
	srv    *Server
	st     *store.Store
	svc    *suggest.Service
	cat    *catalog.Catalog
	cfg    config.Config
	clock  *adminClock
	secret string // TOTP secret
	codes  []string
}

func newAdminEnv(t *testing.T) *adminEnv { return newAdminEnvWith(t) }

// newAdminEnvWith builds the owner panel together with the public
// suggestion pages; fetcherOpts adjust the image fetcher.
func newAdminEnvWith(t *testing.T, fetcherOpts ...images.Option) *adminEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{Addr: "127.0.0.1:0", DataDir: dir, Env: config.EnvDevelopment, Secret: bytes.Repeat([]byte{7}, 32),
		RateSuggestPerDay: 100, RateSuggestPerDayIP: 100, RateVotePerHour: 100, RateVotePerHourIP: 100, RateVotePerSuggestionIP: 100}
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
	clock := &adminClock{t: time.Date(2026, 10, 10, 9, 0, 5, 0, time.UTC)}
	a := auth.New(st, cfg, clock.Now, auth.WithHashParams(auth.HashParams{MemoryKiB: 64, Time: 1, Threads: 1}))
	en, err := a.InitOwner(t.Context(), "owner", adminTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	svc := suggest.New(st, cat, cfg, clock.Now)
	srv, err := New(cfg, st, cat, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAdmin(a, images.New(cfg, st, cfg.MediaDir(), fetcherOpts...)), WithSuggest(svc, &fakeVerifier{}, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return &adminEnv{h: srv.Handler(), srv: srv, st: st, svc: svc, cat: cat, cfg: cfg, clock: clock, secret: en.Secret, codes: en.RecoveryCodes}
}

func (e *adminEnv) code(t *testing.T) string {
	t.Helper()
	c, err := totp.GenerateCode(e.secret, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// request sends a request; form values are form-encoded for POST.
func (e *adminEnv) request(t *testing.T, method, target string, form url.Values, cookie *http.Cookie, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(t.Context(), method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

var sameOrigin = map[string]string{"Origin": "http://example.com", "Sec-Fetch-Site": "same-origin"}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

// login signs in with a fresh TOTP step and returns the session client.
func (e *adminEnv) login(t *testing.T) *adminClient {
	t.Helper()
	e.clock.Advance(30 * time.Second)
	rec := e.request(t, http.MethodPost, "/admin/login",
		url.Values{"username": {"owner"}, "password": {adminTestPassword}, "code": {e.code(t)}}, nil, sameOrigin)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("login: status %d, location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	c := sessionCookie(rec)
	if c == nil {
		t.Fatal("login set no session cookie")
	}
	return &adminClient{env: e, cookie: c}
}

type adminClient struct {
	env    *adminEnv
	cookie *http.Cookie
	csrf   string
}

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (c *adminClient) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := c.env.request(t, http.MethodGet, target, nil, c.cookie, nil)
	if m := csrfPattern.FindStringSubmatch(rec.Body.String()); m != nil {
		c.csrf = m[1]
	}
	return rec
}

func (c *adminClient) post(t *testing.T, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	if c.csrf == "" {
		c.get(t, "/admin")
	}
	if form == nil {
		form = url.Values{}
	}
	if !form.Has("csrf") {
		form.Set("csrf", c.csrf)
	}
	return c.env.request(t, http.MethodPost, target, form, c.cookie, sameOrigin)
}

func (e *adminEnv) entry(t *testing.T, id string) store.Entry {
	t.Helper()
	row, err := e.st.Q().GetEntry(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// publicCatalog fetches /api/catalog anonymously.
func (e *adminEnv) publicCatalog(t *testing.T) *catalog.Document {
	t.Helper()
	rec := e.request(t, http.MethodGet, "/api/catalog", nil, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/catalog status %d", rec.Code)
	}
	doc, err := catalog.DecodeDocument(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func catalogHas(t *testing.T, doc *catalog.Document, id string) (catalog.Entry, bool) {
	t.Helper()
	for _, raw := range doc.Entries {
		e, err := catalog.ParseEntry(raw)
		if err != nil {
			t.Fatal(err)
		}
		if e.ID == id {
			return e, true
		}
	}
	return catalog.Entry{}, false
}

func TestAdmin_Unauthenticated(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	for _, path := range []string{"/admin", "/admin/", "/admin/entries", "/admin/entries/a500", "/admin/entries/new",
		"/admin/entries/a500/remove", "/admin/settings", "/admin/export", "/admin/import", "/admin/unknown/path"} {
		t.Run("GET "+path, func(t *testing.T) {
			rec := env.request(t, http.MethodGet, path, nil, nil, nil)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
				t.Fatalf("status %d, location %q", rec.Code, rec.Header().Get("Location"))
			}
			body := rec.Body.String()
			for _, leak := range []string{"Amiga 500", "a500", "csrf", "entries"} {
				if strings.Contains(body, leak) {
					t.Errorf("redirect body leaks %q: %s", leak, body)
				}
			}
			if rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Header().Get("X-Robots-Tag"), "noindex") {
				t.Errorf("headers = %v", rec.Header())
			}
		})
	}
	before := env.entry(t, "a500")
	rec := env.request(t, http.MethodPost, "/admin/entries/a500", url.Values{"name": {"pwned"}, "rev": {"1"}}, nil, sameOrigin)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous POST status %d", rec.Code)
	}
	if got := env.entry(t, "a500"); got.Rev != before.Rev {
		t.Error("anonymous POST changed the entry")
	}
	// A forged cookie is no better than none.
	rec = env.request(t, http.MethodGet, "/admin", nil, &http.Cookie{Name: auth.CookieName, Value: "forged"}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("forged cookie status %d", rec.Code)
	}
}

func TestAdmin_Login(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)

	rec := env.request(t, http.MethodGet, "/admin/login", nil, nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="code"`) {
		t.Fatalf("login page: %d", rec.Code)
	}
	if rec.Header().Get("X-Robots-Tag") == "" {
		t.Error("login page indexable")
	}

	rec = env.request(t, http.MethodPost, "/admin/login",
		url.Values{"username": {"owner"}, "password": {"wrong password"}, "code": {env.code(t)}}, nil, sameOrigin)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Sign-in failed") || sessionCookie(rec) != nil {
		t.Errorf("bad password: status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "wrong password") {
		t.Error("password echoed back")
	}

	rec = env.request(t, http.MethodPost, "/admin/login",
		url.Values{"username": {"owner"}, "password": {adminTestPassword}, "code": {env.code(t)}}, nil,
		map[string]string{"Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin login: status %d", rec.Code)
	}

	c := env.login(t)
	ck := c.cookie
	if !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/admin" || ck.Secure {
		t.Errorf("cookie = %+v (Secure only in production)", ck)
	}
	if rec := c.get(t, "/admin"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Dashboard") {
		t.Fatalf("dashboard: %d", rec.Code)
	}

	// Logging in again with the old cookie rotates the session.
	env.clock.Advance(30 * time.Second)
	rec = env.request(t, http.MethodPost, "/admin/login",
		url.Values{"username": {"owner"}, "password": {adminTestPassword}, "code": {env.code(t)}}, ck, sameOrigin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("second login: %d", rec.Code)
	}
	if nc := sessionCookie(rec); nc == nil || nc.Value == ck.Value {
		t.Error("session not rotated")
	}
	if rec := env.request(t, http.MethodGet, "/admin", nil, ck, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("old session still valid: %d", rec.Code)
	}

	// Recovery code in the code field.
	rec = env.request(t, http.MethodPost, "/admin/login",
		url.Values{"username": {"owner"}, "password": {adminTestPassword}, "code": {env.codes[0]}}, nil, sameOrigin)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "ok=recovery") {
		t.Errorf("recovery login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdmin_Login_Throttle(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	for range auth.MaxIPFailures {
		env.request(t, http.MethodPost, "/admin/login",
			url.Values{"username": {"owner"}, "password": {"nope nope nope"}, "code": {"123456"}}, nil, sameOrigin)
	}
	rec := env.request(t, http.MethodPost, "/admin/login",
		url.Values{"username": {"owner"}, "password": {adminTestPassword}, "code": {env.code(t)}}, nil, sameOrigin)
	if rec.Code != http.StatusTooManyRequests || sessionCookie(rec) != nil {
		t.Errorf("throttled login: status %d", rec.Code)
	}
}

func TestAdmin_SessionExpiry_And_Logout(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)
	env.clock.Advance(auth.IdleTimeout + time.Minute)
	if rec := c.get(t, "/admin"); rec.Code != http.StatusSeeOther {
		t.Errorf("idle session: status %d", rec.Code)
	}

	c = env.login(t)
	if rec := c.post(t, "/admin/logout", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := c.get(t, "/admin"); rec.Code != http.StatusSeeOther {
		t.Errorf("after logout: status %d", rec.Code)
	}
}

func TestAdmin_CSRF_And_Origin(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)
	c.get(t, "/admin/settings")
	paused := func() bool {
		v, err := env.st.Q().SettingBool(t.Context(), store.SettingSubmissionsPaused)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	form := func(csrf string) url.Values {
		return url.Values{"csrf": {csrf}, store.SettingSubmissionsPaused: {"1"}}
	}
	tests := []struct {
		name   string
		form   url.Values
		header map[string]string
	}{
		{"missing token", url.Values{store.SettingSubmissionsPaused: {"1"}, "csrf": {""}}, sameOrigin},
		{"wrong token", form("AAAA"), sameOrigin},
		{"cross-site fetch", form(c.csrf), map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{"foreign origin", form(c.csrf), map[string]string{"Origin": "https://evil.example"}},
		{"null origin", form(c.csrf), map[string]string{"Origin": "null"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := env.request(t, http.MethodPost, "/admin/settings", tt.form, c.cookie, tt.header)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status %d, want 403", rec.Code)
			}
			if paused() {
				t.Fatal("rejected request changed a setting")
			}
		})
	}
	if rec := c.post(t, "/admin/settings", form(c.csrf)); rec.Code != http.StatusSeeOther || !paused() {
		t.Errorf("valid settings POST: status %d, paused %v", rec.Code, paused())
	}
	// Another session's token is not accepted.
	other := env.login(t)
	rec := env.request(t, http.MethodPost, "/admin/settings", url.Values{"csrf": {c.csrf}}, other.cookie, sameOrigin)
	if rec.Code != http.StatusForbidden {
		t.Errorf("token from another session: status %d", rec.Code)
	}
}

// entryForm loads the edit page values for id as a POST form.
func entryForm(t *testing.T, env *adminEnv, id string) url.Values {
	t.Helper()
	row := env.entry(t, id)
	return docForm(t, row.Doc, row.Rev)
}

// docForm turns an entry document into the entry form's values.
func docForm(t *testing.T, doc json.RawMessage, rev int64) url.Values {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(doc, &m); err != nil {
		t.Fatal(err)
	}
	f := url.Values{"rev": {itoa(rev)}}
	for k, v := range m {
		switch x := v.(type) {
		case string:
			f.Set(k, x)
		case float64:
			f.Set(k, itoa(int64(x)))
		case bool:
			if x {
				f.Set(k, "1")
			}
		case []any:
			for _, it := range x {
				if k == "tags" {
					continue
				}
				f.Add(k, it.(string))
			}
			if k == "tags" {
				var tags []string
				for _, it := range x {
					tags = append(tags, it.(string))
				}
				f.Set("tags", strings.Join(tags, ", "))
			}
		}
	}
	return f
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestAdmin_EntryLifecycle(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)

	if rec := c.get(t, "/admin/entries?q=amiga+500"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/admin/entries/a500") {
		t.Fatalf("search: %d", rec.Code)
	}
	if rec := c.get(t, "/admin/entries/a500"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Revision history") {
		t.Fatalf("edit page: %d", rec.Code)
	}

	t.Run("create", func(t *testing.T) {
		c.get(t, "/admin/entries/new")
		form := url.Values{
			"id": {"test-board"}, "name": {"Test Board"}, "year": {"2025"}, "category": {"hw-classic"},
			"kind": {"original"}, "status": {"shipping"}, "arch": {"68k"}, "weight": {"2"},
			"what": {"A <b>test</b> board."}, "why": {"Testing."}, "related": {"a500"}, "tags": {"test, board"},
		}
		rec := c.post(t, "/admin/entries/new", form)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/entries/test-board?ok=created" {
			t.Fatalf("create: %d %s", rec.Code, rec.Body)
		}
		e, ok := catalogHas(t, env.publicCatalog(t), "test-board")
		if !ok || e.What != "A <b>test</b> board." || !slices.Equal(e.Related, []string{"a500"}) {
			t.Errorf("created entry = %+v, %v", e, ok)
		}
		form.Set("category", "nope")
		form.Set("id", "test-board-2")
		if rec := c.post(t, "/admin/entries/new", form); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "unknown category") {
			t.Errorf("invalid create: %d", rec.Code)
		}
	})

	t.Run("edit and stale", func(t *testing.T) {
		form := entryForm(t, env, "a500")
		stale := entryForm(t, env, "a500")
		form.Set("name", "Amiga 500 (edited)")
		form.Set("reason", "test edit")
		rec := c.post(t, "/admin/entries/a500", form)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("edit: %d %s", rec.Code, rec.Body)
		}
		if e, _ := catalogHas(t, env.publicCatalog(t), "a500"); e.Name != "Amiga 500 (edited)" {
			t.Errorf("name = %q", e.Name)
		}
		stale.Set("name", "Lost update")
		rec = c.post(t, "/admin/entries/a500", stale)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "changed since you opened it") {
			t.Errorf("stale edit: %d", rec.Code)
		}
		if e, _ := catalogHas(t, env.publicCatalog(t), "a500"); e.Name != "Amiga 500 (edited)" {
			t.Errorf("stale edit applied: %q", e.Name)
		}
	})

	t.Run("image fetch refused for private address", func(t *testing.T) {
		img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) }))
		defer img.Close()
		before := env.entry(t, "a500")
		rec := c.post(t, "/admin/entries/a500/image", url.Values{"rev": {itoa(before.Rev)}, "image_url": {img.URL + "/a.png"}})
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "non-public") {
			t.Errorf("status %d", rec.Code)
		}
		if got := env.entry(t, "a500"); got.Rev != before.Rev {
			t.Error("refused fetch changed the entry")
		}
		rec = c.post(t, "/admin/entries/a500/image", url.Values{"rev": {itoa(before.Rev)}, "image_url": {"file:///etc/passwd"}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("file URL: status %d", rec.Code)
		}
	})

	t.Run("remove with dependencies and restore", func(t *testing.T) {
		rec := c.get(t, "/admin/entries/a500/remove")
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "lineage link") || !strings.Contains(body, "guided-route stop") ||
			!strings.Contains(body, "Featured placement") || !strings.Contains(body, "Test Board") {
			t.Fatalf("remove page: %d", rec.Code)
		}
		rev := itoa(env.entry(t, "a500").Rev)
		if rec := c.post(t, "/admin/entries/a500/remove", url.Values{"rev": {rev}}); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("remove without confirm: %d", rec.Code)
		}
		if _, ok := catalogHas(t, env.publicCatalog(t), "a500"); !ok {
			t.Fatal("unconfirmed remove applied")
		}
		rec = c.post(t, "/admin/entries/a500/remove", url.Values{"rev": {rev}, "confirm": {"1"}, "reason": {"test"}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("remove: %d %s", rec.Code, rec.Body)
		}
		doc := env.publicCatalog(t)
		if _, ok := catalogHas(t, doc, "a500"); ok {
			t.Error("removed entry still public")
		}
		if e, _ := catalogHas(t, doc, "test-board"); slices.Contains(e.Related, "a500") {
			t.Error("dependent entry still links to a500")
		}
		if bytes.Contains(doc.Tours, []byte(`"id":"a500"`)) {
			t.Error("tours still reference a500")
		}
		if rec := c.get(t, "/admin/entries/a500"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "This entry is removed") {
			t.Errorf("removed entry page: %d", rec.Code)
		}
		if rec := c.post(t, "/admin/entries/a500/restore", url.Values{"reason": {"undo"}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("restore: %d %s", rec.Code, rec.Body)
		}
		if _, ok := catalogHas(t, env.publicCatalog(t), "a500"); !ok {
			t.Error("restored entry not public")
		}
	})

	t.Run("revert to earlier revision", func(t *testing.T) {
		row := env.entry(t, "a500")
		rec := c.post(t, "/admin/entries/a500/revert", url.Values{"rev": {itoa(row.Rev)}, "to": {"1"}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("revert: %d %s", rec.Code, rec.Body)
		}
		if e, _ := catalogHas(t, env.publicCatalog(t), "a500"); e.Name != "Amiga 500" {
			t.Errorf("name after revert = %q", e.Name)
		}
	})
}

func TestAdmin_ExportImport(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	c := env.login(t)

	rec := c.get(t, "/admin/export")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d", rec.Code)
	}
	doc, err := catalog.DecodeDocument(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Validate(doc); err != nil {
		t.Fatalf("exported catalogue invalid: %v", err)
	}
	// Drop the last entry that is not a tour stop, and links to it.
	var last catalog.Entry
	for i := len(doc.Entries) - 1; i >= 0; i-- {
		e, _ := catalog.ParseEntry(doc.Entries[i])
		if !bytes.Contains(doc.Tours, []byte(`"`+e.ID+`"`)) {
			last = e
			doc.Entries = slices.Delete(doc.Entries, i, i+1)
			break
		}
	}
	for i, raw := range doc.Entries {
		e, _ := catalog.ParseEntry(raw)
		if slices.Contains(e.Related, last.ID) {
			kept := slices.DeleteFunc(slices.Clone(e.Related), func(s string) bool { return s == last.ID })
			doc.Entries[i], _ = catalog.Patch(raw, catalog.Field{Key: "related", Value: kept})
		}
	}
	var file bytes.Buffer
	if err := doc.Encode(&file, true); err != nil {
		t.Fatal(err)
	}

	upload := func(confirm bool, content []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("csrf", c.csrf)
		if confirm {
			_ = mw.WriteField("confirm", "1")
		}
		fw, _ := mw.CreateFormFile("catalog", "amiga-data.json")
		_, _ = fw.Write(content)
		_ = mw.Close()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/import", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Origin", "http://example.com")
		req.AddCookie(c.cookie)
		rr := httptest.NewRecorder()
		env.h.ServeHTTP(rr, req)
		return rr
	}
	c.get(t, "/admin/import")
	if rec := upload(false, file.Bytes()); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("unconfirmed import: %d", rec.Code)
	}
	if rec := upload(true, []byte(`{"meta":{}}`)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid import: %d", rec.Code)
	}
	if _, ok := catalogHas(t, env.publicCatalog(t), last.ID); !ok {
		t.Fatal("rejected import changed the catalogue")
	}
	rec = upload(true, file.Bytes())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1 removed") {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	if _, ok := catalogHas(t, env.publicCatalog(t), last.ID); ok {
		t.Error("imported catalogue still has the dropped entry")
	}
}
