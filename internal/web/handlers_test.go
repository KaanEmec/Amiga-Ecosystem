package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
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
	body := rec.Body.String()
	for _, p := range []string{"/static/css/styles.css", "/static/css/workbench.css", "/static/js/theme.js", "/static/js/data.js"} {
		if !regexp.MustCompile(regexp.QuoteMeta(p) + `\?v=[0-9a-f]{8}"`).MatchString(body) {
			t.Errorf("index.html links %s without an 8-hex version query", p)
		}
	}
	if !regexp.MustCompile(`<script src="/static/js/data\.js\?v=[0-9a-f]{8}" data-v="[0-9a-f]{8}"`).MatchString(body) {
		t.Error("the data.js tag lacks data-v with 8 hex chars")
	}
	etag := rec.Header().Get("ETag")
	if rec := do(t, h, http.MethodGet, "/", map[string]string{"If-None-Match": etag}); etag == "" || rec.Code != http.StatusNotModified {
		t.Errorf("index revalidation: etag %q, status %d", etag, rec.Code)
	}
}

func TestServer_assetVersionServesSameBytes(t *testing.T) {
	t.Parallel()
	_, h := newTestServer(t)
	m := regexp.MustCompile(`href="(/static/css/styles\.css)\?v=([0-9a-f]{8})"`).FindStringSubmatch(do(t, h, http.MethodGet, "/", nil).Body.String())
	if m == nil {
		t.Fatal("index.html has no versioned stylesheet link")
	}
	plain := do(t, h, http.MethodGet, m[1], nil).Body.Bytes()
	for _, q := range []string{"?v=" + m[2], "?v=00000000"} {
		if got := do(t, h, http.MethodGet, m[1]+q, nil).Body.Bytes(); len(plain) == 0 || !bytes.Equal(got, plain) {
			t.Errorf("%s%s serves different bytes", m[1], q)
		}
	}
	if _, err := assetFunc(map[string]string{})("/static/css/nope.css"); err == nil {
		t.Error("asset accepted an unknown file")
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
