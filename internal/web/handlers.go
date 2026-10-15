package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"

	"amigaconstellation/internal/catalog"
)

// handleIndex serves the SPA shell (rendered at startup with versioned
// asset links). It is revalidated on every load so a deploy is picked up
// immediately.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", s.templates.indexETag)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(s.templates.index))
}

// staticVersioned matches the version segment of /static/v-<8 hex>/<path>.
var staticVersioned = regexp.MustCompile(`^/static/v-[0-9a-f]{8}/`)

// staticHandler serves embedded static/ files. /static/v-<8 hex>/<path>
// (what the "asset" template function links) serves the same file as
// /static/<path> and is cacheable forever: the version is in the path, so a
// CDN that ignores query strings still keys on it. Requests carrying a
// version query (?v=...) are also cacheable forever; others for a day,
// revalidated by ETag afterwards.
func (s *Server) staticHandler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(filesOnly{s.static}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versioned := false
		if loc := staticVersioned.FindStringIndex(r.URL.Path); loc != nil {
			r2 := new(http.Request)
			*r2 = *r
			u := *r.URL
			u.Path, u.RawPath = "/static/"+r.URL.Path[loc[1]:], ""
			r2.URL = &u
			r, versioned = r2, true
		}
		if versioned || r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		if etag, ok := s.etags["static/"+strings.TrimPrefix(r.URL.Path, "/static/")]; ok {
			w.Header().Set("ETag", etag)
		}
		files.ServeHTTP(w, r)
	})
}

// mediaHandler serves published images from the media dir. os.Root keeps
// lookups (including symlinks) inside the directory; listings are refused.
func (s *Server) mediaHandler() http.Handler {
	files := http.StripPrefix("/images/", http.FileServerFS(filesOnly{s.media.FS()}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}

// handleCatalog serves the cached catalogue JSON with ETag revalidation.
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	body, etag, err := s.catalog.Cache().Get(r.Context())
	if err != nil {
		if errors.Is(err, catalog.ErrEmpty) {
			http.Error(w, "catalogue not initialised", http.StatusServiceUnavailable)
			return
		}
		s.log.ErrorContext(r.Context(), "building catalogue", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

// handleHealth reports whether the database answers.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status, code := "ok", http.StatusOK
	if err := s.store.Ping(ctx); err != nil {
		s.log.ErrorContext(ctx, "health check", "err", err)
		status, code = "unavailable", http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, code, map[string]string{"status": status})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// etagMatches implements the weak comparison If-None-Match uses for GET.
func etagMatches(header, etag string) bool {
	for part := range strings.SplitSeq(header, ",") {
		p := strings.TrimSpace(part)
		if p == "*" || strings.TrimPrefix(p, "W/") == etag {
			return true
		}
	}
	return false
}

// filesOnly hides directories so file servers never list them.
type filesOnly struct{ fs fs.FS }

func (f filesOnly) Open(name string) (fs.File, error) {
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := file.Stat()
	if err != nil || st.IsDir() {
		_ = file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}
