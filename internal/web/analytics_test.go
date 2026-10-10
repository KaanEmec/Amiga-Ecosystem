package web

import (
	"net/http"
	"strings"
	"testing"
)

const browserUA = "Mozilla/5.0 (Macintosh) Gecko/20100101 Firefox/140.0"

func TestPageviews_Recording(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	type row struct{ path, visitor, referrer string }
	rows := func() []row {
		t.Helper()
		rs, err := env.st.DB().QueryContext(t.Context(), "SELECT path, visitor, referrer FROM pageviews ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rs.Close()
		var out []row
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.path, &r.visitor, &r.referrer); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}
	get := func(path string, header map[string]string) {
		t.Helper()
		env.request(t, http.MethodGet, path, nil, nil, header)
	}

	// Not counted: assets, API, admin, unknown pages, bots, no User-Agent.
	get("/static/css/styles.css", map[string]string{"User-Agent": browserUA})
	get("/api/catalog", map[string]string{"User-Agent": browserUA})
	get("/admin", map[string]string{"User-Agent": browserUA})
	get("/suggestions/999999", map[string]string{"User-Agent": browserUA})
	get("/", map[string]string{"User-Agent": "Mozilla/5.0 (compatible; Googlebot/2.1)"})
	get("/", map[string]string{"User-Agent": "Mozilla/5.0 HeadlessChrome/120"})
	get("/", nil)
	if got := rows(); len(got) != 0 {
		t.Fatalf("recorded %+v, want nothing", got)
	}

	get("/", map[string]string{"User-Agent": browserUA, "Referer": "https://News.Example.org/item?id=1"})
	get("/suggestions?q=a500", map[string]string{"User-Agent": browserUA, "Referer": "http://example.com/"})
	get("/", map[string]string{"User-Agent": browserUA + " other"})
	got := rows()
	if len(got) != 3 {
		t.Fatalf("recorded %+v, want 3 rows", got)
	}
	if got[0].path != "/" || got[0].referrer != "news.example.org" || len(got[0].visitor) != 16 {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[1].path != "/suggestions" || got[1].referrer != "" || got[1].visitor != got[0].visitor {
		t.Errorf("row 1 = %+v (same visitor as %q, same-site referrer dropped)", got[1], got[0].visitor)
	}
	if got[2].visitor == got[0].visitor {
		t.Error("a different User-Agent got the same visitor id")
	}
	if rec := env.request(t, http.MethodGet, "/", nil, nil, map[string]string{"User-Agent": browserUA}); rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("GET / set a cookie: %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestAdmin_Stats(t *testing.T) {
	t.Parallel()
	env := newAdminEnv(t)
	rec := env.request(t, http.MethodGet, "/admin/stats", nil, nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("anonymous: status %d, location %q", rec.Code, rec.Header().Get("Location"))
	}

	env.request(t, http.MethodGet, "/suggestions", nil, nil, map[string]string{"User-Agent": browserUA, "Referer": "https://forum.example.net/t/1"})
	c := env.login(t)
	rec = c.get(t, "/admin/stats")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Last 30 days", "Top pages", "/suggestions", "forum.example.net", "cookie-free", `aria-current="page">Visitors`, `style="width:100%"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	rec = c.get(t, "/admin")
	if !strings.Contains(rec.Body.String(), `href="/admin/stats"><b class="mono">1</b>`) {
		t.Errorf("dashboard lacks the 7-day visitors tile: %s", rec.Body)
	}
}
