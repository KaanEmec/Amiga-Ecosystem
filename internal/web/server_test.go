package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
)

// newTestServer builds a server over a fresh data dir with the seed
// catalogue imported (no images) and one image in the media dir.
func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{Addr: "127.0.0.1:0", DataDir: dir, Env: config.EnvDevelopment}

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

	if err := os.MkdirAll(filepath.Join(cfg.MediaDir(), "essentials"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.MediaDir(), "essentials", "x.png"), []byte("\x89PNG\r\n\x1a\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("secret"), 0o640); err != nil {
		t.Fatal(err)
	}

	srv, err := New(cfg, st, cat, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, srv.Handler()
}

func do(t *testing.T, h http.Handler, method, target string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestServer_Handler_Routes(t *testing.T) {
	t.Parallel()
	_, h := newTestServer(t)
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"index", http.MethodGet, "/", http.StatusOK},
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound},
		{"static file", http.MethodGet, "/static/css/styles.css", http.StatusOK},
		{"static dir listing", http.MethodGet, "/static/js/", http.StatusNotFound},
		{"static root listing", http.MethodGet, "/static/", http.StatusNotFound},
		{"image", http.MethodGet, "/images/essentials/x.png", http.StatusOK},
		{"image dir listing", http.MethodGet, "/images/essentials/", http.StatusNotFound},
		// The mux redirects uncleaned paths to their clean form (/outside.txt).
		{"image traversal", http.MethodGet, "/images/../outside.txt", http.StatusTemporaryRedirect},
		{"image encoded traversal", http.MethodGet, "/images/%2e%2e/outside.txt", http.StatusNotFound},
		{"post catalog", http.MethodPost, "/api/catalog", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := do(t, h, tt.method, tt.path, nil)
			if rec.Code != tt.want {
				t.Errorf("%s %s = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
			}
			if strings.Contains(rec.Body.String(), "secret") {
				t.Errorf("%s %s leaked a file outside the media dir", tt.method, tt.path)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q", got)
			}
		})
	}
}
