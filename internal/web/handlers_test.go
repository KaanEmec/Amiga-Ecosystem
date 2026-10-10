package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestServer_handleIndex(t *testing.T) {
	t.Parallel()
	_, h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestServer_staticHandler(t *testing.T) {
	t.Parallel()
	_, h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/static/css/styles.css", nil)
	etag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || etag == "" {
		t.Fatalf("status = %d, etag = %q", rec.Code, etag)
	}
	if rec := do(t, h, http.MethodGet, "/static/css/styles.css", map[string]string{"If-None-Match": etag}); rec.Code != http.StatusNotModified {
		t.Errorf("revalidation status = %d, want 304", rec.Code)
	}
	if got := do(t, h, http.MethodGet, "/static/css/styles.css?v=abc", nil).Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("versioned Cache-Control = %q, want immutable", got)
	}
}

func TestServer_handleCatalog(t *testing.T) {
	t.Parallel()
	_, h := newTestServer(t)

	rec := do(t, h, http.MethodGet, "/api/catalog", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	var doc struct {
		Readme  json.RawMessage   `json:"_readme"`
		Meta    json.RawMessage   `json:"meta"`
		Domains json.RawMessage   `json:"domains"`
		Tours   json.RawMessage   `json:"tours"`
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 332 || doc.Meta == nil || doc.Domains == nil || doc.Tours == nil || doc.Readme != nil {
		t.Errorf("catalogue shape: %d entries, meta=%v domains=%v tours=%v readme=%v",
			len(doc.Entries), doc.Meta != nil, doc.Domains != nil, doc.Tours != nil, doc.Readme != nil)
	}

	for _, inm := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		rec := do(t, h, http.MethodGet, "/api/catalog", map[string]string{"If-None-Match": inm})
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
			t.Errorf("If-None-Match %s: status = %d, body = %d bytes; want 304, empty", inm, rec.Code, rec.Body.Len())
		}
	}
	if rec := do(t, h, http.MethodGet, "/api/catalog", map[string]string{"If-None-Match": `"stale"`}); rec.Code != http.StatusOK {
		t.Errorf("stale If-None-Match: status = %d, want 200", rec.Code)
	}
}

func TestServer_handleHealth(t *testing.T) {
	t.Parallel()
	_, h := newTestServer(t)
	rec := do(t, h, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != "ok" {
		t.Errorf("body = %s, err = %v", rec.Body.String(), err)
	}
}

func TestServer_handleHealth_DBDown(t *testing.T) {
	t.Parallel()
	srv, h := newTestServer(t)
	_ = srv.store.Close()
	if rec := do(t, h, http.MethodGet, "/healthz", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
