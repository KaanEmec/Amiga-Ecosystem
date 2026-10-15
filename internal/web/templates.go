package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// Templates live in web/templates. Files starting with "_" are partials
// (layouts, fragments) parsed into every page; every other *.html is a page.
// A page declares its layout by calling it at top level, e.g.
//
//	{{define "content"}}...{{end}}
//	{{template "public_layout" .}}
//
// and is rendered by its file name via Server.render.
type templateSet struct {
	pages     map[string]*template.Template
	index     []byte // the SPA shell (index.html), rendered once at startup
	indexETag string
}

// assetFunc returns the template function "asset": the static path with a
// short content version, e.g. /static/css/styles.css?v=1a2b3c4d, so a deploy
// busts browser caches (versioned requests are cached as immutable). An
// unknown path is an error, so a typo fails rendering instead of shipping.
func assetFunc(etags map[string]string) func(string) (string, error) {
	return func(p string) (string, error) {
		e := strings.Trim(etags[strings.TrimPrefix(p, "/")], `"`)
		if !strings.HasPrefix(p, "/static/") || len(e) < 8 {
			return "", fmt.Errorf("asset %q: not an embedded static file", p)
		}
		return p + "?v=" + e[:8], nil
	}
}

var templateFuncs = template.FuncMap{
	"date": func(ts int64) string { return time.Unix(ts, 0).UTC().Format("2 Jan 2006") },
	"datetime": func(ts int64) string {
		return time.Unix(ts, 0).UTC().Format("2 Jan 2006 15:04 UTC")
	},
	"title": func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + strings.ReplaceAll(s[1:], "_", " ")
	},
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	},
	"add": func(a, b int) int { return a + b },
}

func parseTemplates(root fs.FS, etags map[string]string) (*templateSet, error) {
	jsVersion := dirVersion(etags, "static/js/") // data.js versions the scripts it loads
	funcs := template.FuncMap{"asset": assetFunc(etags), "assetVersion": func() string { return jsVersion }}
	entries, err := fs.ReadDir(root, "templates")
	if err != nil {
		return nil, fmt.Errorf("reading templates dir: %w", err)
	}
	var partials, pages []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".html") {
			continue
		}
		if strings.HasPrefix(name, "_") {
			partials = append(partials, path.Join("templates", name))
		} else {
			pages = append(pages, name)
		}
	}
	set := &templateSet{pages: make(map[string]*template.Template, len(pages))}
	for _, name := range pages {
		if name == "index.html" {
			// The SPA shell has no per-request data: render it once.
			t, err := template.New(name).Funcs(templateFuncs).Funcs(funcs).ParseFS(root, path.Join("templates", name))
			if err != nil {
				return nil, fmt.Errorf("parsing template %s: %w", name, err)
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, nil); err != nil {
				return nil, fmt.Errorf("rendering %s: %w", name, err)
			}
			sum := sha256.Sum256(buf.Bytes())
			set.index, set.indexETag = buf.Bytes(), `"`+hex.EncodeToString(sum[:16])+`"`
			continue
		}
		files := append([]string{path.Join("templates", name)}, partials...)
		t, err := template.New(name).Funcs(templateFuncs).Funcs(funcs).ParseFS(root, files...)
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", name, err)
		}
		set.pages[name] = t
	}
	return set, nil
}

// render executes the named page template with data. Output is buffered so a
// template error yields a 500 instead of a half-written page.
func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := s.templates.pages[page]
	if !ok {
		s.log.Error("unknown template", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, page, data); err != nil {
		s.log.Error("rendering template", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
