// Package web is the HTTP layer: routing, middleware and handlers.
package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
	assets "amigaconstellation/web"
)

// maxBodyBytes is the default request body limit.
const maxBodyBytes = 1 << 20

// shutdownTimeout bounds how long in-flight requests get on shutdown.
const shutdownTimeout = 15 * time.Second

// Server holds the HTTP dependencies.
type Server struct {
	cfg     config.Config
	store   *store.Store
	catalog *catalog.Catalog
	log     *slog.Logger

	assets fs.FS             // embedded static/ and templates/
	static fs.FS             // assets rooted at static/
	etags  map[string]string // embedded path -> quoted sha256 ETag
	media  *os.Root          // media dir, served at /images/

	templates *templateSet // parsed web/templates pages (see templates.go)

	// Feature dependencies, set through Options. Each feature declares its
	// own field here and registers its routes from its own file.
	suggest *suggestDeps // suggest.go (public suggestions, votes, spam)
	admin   *adminDeps   // admin.go (owner auth, private panel)

	pageviewsPruned atomic.Int64 // analytics.go: Unix time of the last prune
}

// Option configures optional server features.
type Option func(*Server)

// New wires the server. It creates the media dir if missing and hashes the
// embedded assets for ETags. Call Close when done.
func New(cfg config.Config, st *store.Store, cat *catalog.Catalog, logger *slog.Logger, opts ...Option) (*Server, error) {
	if err := os.MkdirAll(cfg.MediaDir(), 0o750); err != nil {
		return nil, fmt.Errorf("creating media dir: %w", err)
	}
	media, err := os.OpenRoot(cfg.MediaDir())
	if err != nil {
		return nil, fmt.Errorf("opening media dir: %w", err)
	}
	etags, err := hashFiles(assets.FS)
	if err != nil {
		_ = media.Close()
		return nil, err
	}
	static, err := fs.Sub(assets.FS, "static")
	if err != nil {
		_ = media.Close()
		return nil, fmt.Errorf("embedded static dir: %w", err)
	}
	tmpl, err := parseTemplates(assets.FS, etags)
	if err != nil {
		_ = media.Close()
		return nil, err
	}
	s := &Server{
		templates: tmpl,
		cfg:       cfg,
		store:     st,
		catalog:   cat,
		log:       logger,
		assets:    assets.FS,
		static:    static,
		etags:     etags,
		media:     media,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Close releases the media dir handle.
func (s *Server) Close() error { return s.media.Close() }

// Handler returns the routed handler wrapped in middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", s.staticHandler())
	mux.Handle("GET /images/", s.mediaHandler())
	mux.HandleFunc("GET /api/catalog", s.handleCatalog)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	s.registerSuggestRoutes(mux) // no-op until WithSuggest is used
	s.registerAdminRoutes(mux)   // no-op until WithAdmin is used

	var h http.Handler = mux
	h = s.recordPageviews(h)
	h = limitBody(h, maxBodyBytes)
	h = securityHeaders(h, s.cfg.Production())
	h = recoverPanics(h, s.log)
	h = logRequests(h, s.log)
	return h
}

// ListenAndServe serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		s.log.Info("listening", "addr", s.cfg.Addr, "env", s.cfg.Env)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}
	s.log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// dirVersion returns the first 8 hex chars of a hash over the ETags of every
// embedded file under prefix (e.g. "static/js/"), in path order.
func dirVersion(etags map[string]string, prefix string) string {
	var paths []string
	for p := range etags {
		if strings.HasPrefix(p, prefix) {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s %s\n", p, etags[p])
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// hashFiles computes a strong ETag for every embedded file.
func hashFiles(fsys fs.FS) (map[string]string, error) {
	out := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[p] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hashing embedded assets: %w", err)
	}
	return out, nil
}
