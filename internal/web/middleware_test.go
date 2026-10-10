package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	h := securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"frame-ancestors 'none'",
		"https://fonts.googleapis.com",
		"https://fonts.gstatic.com",
		"script-src 'self' https://challenges.cloudflare.com",
		"frame-src https://challenges.cloudflare.com",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
	if rec.Header().Get("Referrer-Policy") == "" || rec.Header().Get("Strict-Transport-Security") == "" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestRecoverPanics(t *testing.T) {
	t.Parallel()
	h := recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestLimitBody(t *testing.T) {
	t.Parallel()
	var readErr error
	h := limitBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}), 4)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("too long"))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if readErr == nil {
		t.Error("oversized body was read without error")
	}
}
