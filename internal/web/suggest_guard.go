package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"amigaconstellation/internal/suggest"
)

// visitorCookie carries the signed browser id used for votes and limits.
const visitorCookie = "amiga_vid"

const visitorCookieMaxAge = 365 * 24 * time.Hour

// visitorID returns the verified browser id from the request cookie.
func (s *Server) visitorID(r *http.Request) (string, bool) {
	c, err := r.Cookie(visitorCookie)
	if err != nil {
		return "", false
	}
	return s.suggest.signer.VisitorID(c.Value)
}

// ensureVisitor returns the browser id, issuing a new signed cookie when the
// request has none (or an invalid one). Called on public suggestion pages.
func (s *Server) ensureVisitor(w http.ResponseWriter, r *http.Request) string {
	if id, ok := s.visitorID(r); ok {
		return id
	}
	token, id, err := s.suggest.signer.NewVisitor()
	if err != nil {
		s.log.ErrorContext(r.Context(), "issuing visitor cookie", "err", err)
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     visitorCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(visitorCookieMaxAge / time.Second),
		HttpOnly: true,
		Secure:   s.cfg.Production(),
		SameSite: http.SameSiteLaxMode,
	})
	return id
}

// visitor builds the acting visitor (browser id + salted IP hash).
func (s *Server) visitor(r *http.Request, id string) suggest.Visitor {
	return suggest.Visitor{ID: id, IPHash: s.suggest.signer.HashIP(rateIP(s.visitorIP(r)))}
}

// allowHeaderless lets sameOriginMutation pass requests that carry neither
// Sec-Fetch-Site nor Origin (non-browser clients). Only the owner panel
// uses it: its forms also need the session-bound CSRF token, and login
// has no session to ride on.
const allowHeaderless = true

// sameOriginMutation checks that a state-changing request comes from our
// own pages: Sec-Fetch-Site, when sent, must be same-origin, and Origin,
// when sent, must equal AMIGA_BASE_URL (or the request's own origin in
// development without a base URL). A request with neither header is
// refused unless headerless is set.
func (s *Server) sameOriginMutation(r *http.Request, headerless bool) bool {
	sfs := r.Header.Get("Sec-Fetch-Site")
	if sfs != "" && sfs != "same-origin" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return sfs == "same-origin" || headerless
	}
	want := s.cfg.BaseURL
	if want == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		want = scheme + "://" + r.Host
	}
	return strings.EqualFold(origin, want)
}

// visitorIP is the peer address, or when AMIGA_TRUST_PROXY=1 the
// AMIGA_CLIENT_IP_HEADER value or the last X-Forwarded-For hop (the one our
// reverse proxy appended).
func (s *Server) visitorIP(r *http.Request) string {
	if s.cfg.TrustProxyForwarding {
		if h := s.cfg.ClientIPHeader; h != "" {
			if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get(h))); err == nil {
				return ip.Unmap().String()
			}
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			hops := strings.Split(xff, ",")
			if ip, err := netip.ParseAddr(strings.TrimSpace(hops[len(hops)-1])); err == nil {
				return ip.Unmap().String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String()
	}
	return host
}

// rateIP groups IPv6 clients by /64, since one subscriber usually holds a
// whole /64; IPv4 addresses are used as is.
func rateIP(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Is4() {
		return ip
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return ip
	}
	return p.String()
}

// Entry-search limits: the endpoint is read-only and called while typing
// (debounced), so it is limited in memory instead of with a database write
// per request. The IP ceiling covers clients that rotate browser ids.
const (
	searchWindow     = time.Minute
	searchPerVisitor = 60
	searchPerIP      = 600
)

// windowLimiter counts requests per key in fixed windows. The whole map
// is dropped when a window ends, so it holds at most one window's keys.
type windowLimiter struct {
	mu     sync.Mutex
	window time.Duration
	start  time.Time
	counts map[string]int
}

func newWindowLimiter(window time.Duration) *windowLimiter {
	return &windowLimiter{window: window, counts: map[string]int{}}
}

type keyLimit struct {
	key   string
	limit int
}

// allow counts one request against every key and reports whether all of
// them were under their limit.
func (l *windowLimiter) allow(now time.Time, keys ...keyLimit) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.start) >= l.window || now.Before(l.start) {
		l.start, l.counts = now, map[string]int{}
	}
	ok := true
	for _, k := range keys {
		l.counts[k.key]++
		if l.counts[k.key] > k.limit {
			ok = false
		}
	}
	return ok
}

// allowSearch applies the entry-search limits to the request.
func (s *Server) allowSearch(r *http.Request) bool {
	keys := []keyLimit{{"ip:" + rateIP(s.visitorIP(r)), searchPerIP}}
	if vid, ok := s.visitorID(r); ok {
		keys = append(keys, keyLimit{"v:" + vid, searchPerVisitor})
	}
	return s.suggest.search.allow(s.suggest.now(), keys...)
}
