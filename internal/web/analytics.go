package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"amigaconstellation/internal/store"
)

// Cookie-free page view analytics. Each successful GET of a public HTML page
// is one row; the visitor id is a hash that rotates every UTC day.
const (
	pageviewMaxLen     = 200
	pageviewTimeout    = 2 * time.Second
	pageviewRetain     = 400 * 24 * time.Hour
	pageviewPruneEvery = time.Hour
)

// botUA matches User-Agents that are not counted.
var botUA = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|curl|wget|python-requests|headlesschrome|lighthouse`)

// isPageviewPath reports whether path is a public HTML page.
func isPageviewPath(path string) bool {
	return path == "/" || path == "/suggestions" || strings.HasPrefix(path, "/suggestions/")
}

// recordPageviews records successful public page views after the response
// is written. A failed write is logged and never affects the response.
func (s *Server) recordPageviews(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.UserAgent()
		if r.Method != http.MethodGet || !isPageviewPath(r.URL.Path) || ua == "" || botUA.MatchString(ua) {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if (rec.status < 200 || rec.status > 299) && rec.status != http.StatusNotModified {
			return
		}
		_ = http.NewResponseController(w).Flush()

		now := time.Now().UTC()
		day := now.Format(time.DateOnly)
		h := sha256.New()
		h.Write(s.cfg.Secret)
		h.Write([]byte(day + "\x00" + s.visitorIP(r) + "\x00" + ua))
		pv := store.Pageview{
			At:       now.Unix(),
			Day:      day,
			Path:     truncate(r.URL.Path, pageviewMaxLen),
			Visitor:  hex.EncodeToString(h.Sum(nil))[:16],
			Referrer: truncate(s.referrerHost(r), pageviewMaxLen),
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), pageviewTimeout)
		defer cancel()
		q := s.store.Q()
		if err := q.InsertPageview(ctx, pv); err != nil {
			s.log.WarnContext(ctx, "recording pageview", "err", err)
			return
		}
		if last := s.pageviewsPruned.Load(); now.Unix()-last >= int64(pageviewPruneEvery/time.Second) &&
			s.pageviewsPruned.CompareAndSwap(last, now.Unix()) {
			if err := q.PrunePageviews(ctx, now.Add(-pageviewRetain).Unix()); err != nil {
				s.log.WarnContext(ctx, "pruning pageviews", "err", err)
			}
		}
	})
}

// referrerHost is the Referer's host, or "" when absent or our own site.
func (s *Server) referrerHost(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == hostOnly(r.Host) {
		return ""
	}
	if base, err := url.Parse(s.cfg.BaseURL); err == nil && host == strings.ToLower(base.Hostname()) {
		return ""
	}
	return host
}

// hostOnly drops the port from a Host header value and lowercases it.
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		hostport = h
	}
	return strings.ToLower(strings.Trim(hostport, "[]"))
}

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// --- owner page -------------------------------------------------------------

type adminStatsPeriod struct {
	Label string
	store.PageStats
}

type adminStatsDay struct {
	store.DayStats
	Pct int // bar width relative to the busiest day
}

type adminStatsView struct {
	adminView
	Periods   []adminStatsPeriod
	Days      []adminStatsDay
	Pages     []store.TopRow
	Referrers []store.TopRow
}

// statsDays is the window of the daily table and the top lists.
const statsDays = 30

// daysAgo is the Unix time of UTC midnight n days before now.
func daysAgo(now time.Time, n int) int64 {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day()-n, 0, 0, 0, 0, time.UTC).Unix()
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.store.Q()
	now := time.Now()
	v := adminStatsView{adminView: s.adminViewFor(r, "Visitors", "stats")}
	for _, p := range []struct {
		label string
		days  int
	}{{"Today", 1}, {"Last 7 days", 7}, {"Last 30 days", statsDays}} {
		sum, err := q.StatsSummary(ctx, daysAgo(now, p.days-1))
		if err != nil {
			s.adminFail(w, r, err)
			return
		}
		v.Periods = append(v.Periods, adminStatsPeriod{Label: p.label, PageStats: sum})
	}
	days, err := q.DailyStats(ctx, now, statsDays)
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	peak := 1
	for _, d := range days {
		peak = max(peak, d.Pageviews)
	}
	for _, d := range days {
		v.Days = append(v.Days, adminStatsDay{DayStats: d, Pct: d.Pageviews * 100 / peak})
	}
	since := daysAgo(now, statsDays-1)
	if v.Pages, err = q.TopPaths(ctx, since, 20); err != nil {
		s.adminFail(w, r, err)
		return
	}
	if v.Referrers, err = q.TopReferrers(ctx, since, 20); err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "admin_stats.html", v)
}
