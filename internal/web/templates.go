package web

import (
	"bytes"
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
	pages map[string]*template.Template
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

func parseTemplates(root fs.FS) (*templateSet, error) {
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
			continue // the SPA shell is served as a static file
		}
		files := append([]string{path.Join("templates", name)}, partials...)
		t, err := template.New(name).Funcs(templateFuncs).ParseFS(root, files...)
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
