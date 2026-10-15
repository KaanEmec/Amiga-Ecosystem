package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
)

// Bulk decisions on the review queue and the image triage page. Every item
// goes through the same decision functions as the single review page; items
// that would need a confirmation there are skipped, never forced.

const (
	reviewBulkMax        = 100              // ids per bulk request
	reviewImagePageSize  = 30               // cards per triage page
	reviewBulkBudget     = 25 * time.Second // + one worst-case fetch with retry (~30 s) stays under the 60 s write timeout
	reviewTriagePath     = "/admin/queue/images"
	reviewBulkQueueRedir = "/admin/queue"
)

// --- "back" redirects -------------------------------------------------------------

// reviewBack returns the form's "back" value when it is a same-site path
// under /admin/ (query kept, fragment dropped), "" otherwise.
func reviewBack(r *http.Request) string {
	raw := r.PostFormValue("back")
	if raw == "" || !strings.HasPrefix(raw, "/admin/") || strings.ContainsAny(raw, "\\\r\n\t") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return ""
	}
	if p := path.Clean(u.Path); p != u.Path || !strings.HasPrefix(p, "/admin/") {
		return ""
	}
	return (&url.URL{Path: u.Path, RawQuery: u.Query().Encode()}).String()
}

// reviewWithParams returns back (validated by reviewBack) with the result
// parameters of an earlier action replaced by set.
func reviewWithParams(back string, set url.Values) string {
	u, err := url.Parse(back)
	if err != nil {
		return "/admin"
	}
	q := u.Query()
	for _, k := range []string{"ok", "bulk", "n", "skip"} {
		q.Del(k)
	}
	for k, v := range set {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// --- bulk results ------------------------------------------------------------------

// reviewSkipReasons are the fixed texts of skip codes in bulk notices; the
// notice is rebuilt from ids and codes, so no submitted text is reflected.
var reviewSkipReasons = map[string]string{
	"stale":    "stale (the entry changed meanwhile; apply again)",
	"conflict": "conflict (the proposed fields changed since; open to review)",
	"confirm":  "needs confirmation",
	"decided":  "already decided",
	"hidden":   "already hidden",
	"invalid":  "does not validate",
	"exists":   "id already exists",
	"removed":  "entry removed",
	"missing":  "not found",
	"fetch":    "image not fetched",
	"publish":  "image not published",
	"type":     "unsupported type",
	"time":     "not attempted (time limit; apply again)",
	"error":    "failed (see the log)",
}

var reviewBulkVerbs = map[string]string{
	store.StatusAccepted: "accepted",
	store.StatusRejected: "rejected",
	store.StatusHidden:   "hidden",
}

type reviewBulkResult struct {
	verb  string
	done  int
	skips []string // "id.code"
}

func (b *reviewBulkResult) skip(id int64, code string) {
	b.skips = append(b.skips, fmt.Sprintf("%d.%s", id, code))
}

// fail records a per-item error under its skip code; unexpected errors are logged.
func (s *Server) reviewBulkFail(ctx context.Context, b *reviewBulkResult, id int64, err error) {
	code := reviewSkipCode(err)
	if code == "error" {
		s.log.ErrorContext(ctx, "bulk review", "suggestion", id, "err", err)
	}
	b.skip(id, code)
}

func (b *reviewBulkResult) params() url.Values {
	v := url.Values{"bulk": {b.verb}, "n": {strconv.Itoa(b.done)}}
	if len(b.skips) > 0 {
		v.Set("skip", strings.Join(b.skips, ","))
	}
	return v
}

func reviewSkipCode(err error) string {
	var rf *reviewRefusal
	var ve *catalog.ValidationError
	switch {
	case errors.As(err, &rf):
		return rf.code
	case errors.Is(err, store.ErrNotPending):
		return "decided"
	case errors.Is(err, catalog.ErrStale):
		return "stale"
	case errors.As(err, &ve):
		return "invalid"
	case errors.Is(err, catalog.ErrExists):
		return "exists"
	case errors.Is(err, catalog.ErrDeleted):
		return "removed"
	case errors.Is(err, catalog.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return "missing"
	}
	return "error"
}

// reviewBulkNotice rebuilds a bulk result notice from the redirect's query.
func reviewBulkNotice(q url.Values) string {
	verb, ok := reviewBulkVerbs[q.Get("bulk")]
	if !ok {
		return ""
	}
	n, _ := strconv.Atoi(q.Get("n"))
	// Group the skipped ids by reason, in order of first appearance.
	var order []string
	byReason := map[string][]string{}
	skipped := 0
	for part := range strings.SplitSeq(q.Get("skip"), ",") {
		raw, code, found := strings.Cut(part, ".")
		id, err := strconv.ParseInt(raw, 10, 64)
		reason, known := reviewSkipReasons[code]
		if !found || err != nil || !known {
			continue
		}
		if _, seen := byReason[reason]; !seen {
			order = append(order, reason)
		}
		byReason[reason] = append(byReason[reason], fmt.Sprintf("#%d", id))
		skipped++
	}
	msg := fmt.Sprintf("%d %s", max(n, 0), verb)
	if skipped > 0 {
		groups := make([]string, len(order))
		for i, reason := range order {
			groups[i] = strings.Join(byReason[reason], ", ") + " " + reason
		}
		msg += fmt.Sprintf(", %d skipped: %s", skipped, strings.Join(groups, "; "))
	}
	return msg + "."
}

// reviewBulkIDs reads the selected ids (deduplicated, in form order). ok is
// false when more than reviewBulkMax are sent.
func reviewBulkIDs(r *http.Request) (ids []int64, ok bool) {
	raw := append(r.PostForm["ids"], r.PostForm["ids[]"]...)
	if len(raw) > reviewBulkMax {
		return nil, false
	}
	seen := map[int64]bool{}
	for _, v := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil && id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, true
}

// --- POST /admin/queue/bulk -----------------------------------------------------------

// reviewBulk applies Accept, Reject or Hide to the selected suggestions and
// redirects back with a notice. Accept is plain Accept with the owner
// defaults; anything that would need a confirmation is skipped.
func (s *Server) reviewBulk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	back := reviewBack(r)
	if back == "" {
		back = reviewBulkQueueRedir
	}
	action := r.PostFormValue("action")
	verb := map[string]string{"accept": store.StatusAccepted, "reject": store.StatusRejected, "hide": store.StatusHidden}[action]
	if verb == "" {
		http.Error(w, "unknown bulk action", http.StatusBadRequest)
		return
	}
	ids, ok := reviewBulkIDs(r)
	if !ok {
		http.Error(w, fmt.Sprintf("too many suggestions selected (at most %d)", reviewBulkMax), http.StatusRequestEntityTooLarge)
		return
	}
	note := strings.TrimSpace(r.PostFormValue("decision_note"))
	res := reviewBulkResult{verb: verb}
	deadline := time.Now().Add(reviewBulkBudget)
	for _, id := range ids {
		if time.Now().After(deadline) {
			res.skip(id, "time")
			continue
		}
		sg, err := s.store.Q().GetSuggestion(ctx, id)
		if err != nil {
			s.reviewBulkFail(ctx, &res, id, err)
			continue
		}
		switch {
		case action == "hide" && sg.Status == store.StatusHidden:
			res.skip(id, "hidden")
			continue
		case action != "hide" && sg.Status != store.StatusPending:
			res.skip(id, "decided")
			continue
		}
		d := store.Decision{DecisionNote: note, At: time.Now().Unix()}
		switch action {
		case "accept":
			_, err = s.reviewAcceptDecide(ctx, sg, reviewAcceptOptions{Decision: d, Defaults: true})
		case "reject":
			err = s.reviewRejectDecide(ctx, sg, d)
		case "hide":
			err = s.reviewHideDecide(ctx, id, "")
		}
		if err != nil {
			s.reviewBulkFail(ctx, &res, id, err)
			continue
		}
		res.done++
	}
	http.Redirect(w, r, reviewWithParams(back, res.params()), http.StatusSeeOther)
}

// --- GET /admin/queue/images ------------------------------------------------------

type reviewImageCard struct {
	ID         int64
	EntryID    string
	EntryName  string
	EntryRev   int64
	EntryState string // live, removed, missing
	Current    string // src of the live entry's image, "" for none
	Image      imageView
	Reason     string
	Conflict   bool
}

type reviewImagesView struct {
	adminView
	Cards              []reviewImageCard
	Total, Page, Pages int
	PrevURL, NextURL   string
	Self               string // this page, the "back" of every form
	BulkHide           bool   // always false: triage offers Accept and Reject
}

func reviewImagesLink(page int) string {
	return reviewTriagePath + "?" + url.Values{"page": {strconv.Itoa(page)}}.Encode()
}

// reviewCurrentImage returns the src of a live entry's published image: a
// local "images/..." path under /images/, or the https URL the entry
// already uses publicly; "" when there is none or it is neither.
func reviewCurrentImage(doc json.RawMessage) string {
	var m struct {
		Image string `json:"image"`
	}
	_ = json.Unmarshal(doc, &m)
	p := m.Image
	if strings.HasPrefix(p, "images/") && !strings.Contains(p, "..") && !strings.ContainsAny(p, "?#\\") {
		return "/" + p
	}
	if u, err := url.Parse(p); err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
		return u.String()
	}
	return ""
}

func reviewExcerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n])) + "…"
}

// reviewImages lists pending image suggestions as cards, newest first. The
// proposed image's <img> points at /admin/staged/suggestion/{id}, which
// stages it on demand; the remote URL is never put in the page's <img>.
func (s *Server) reviewImages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	v := reviewImagesView{adminView: s.adminViewFor(r, "Image triage", "queue")}
	if msg := reviewBulkNotice(q); msg != "" {
		v.Notice = msg
	}
	v.Page, _ = strconv.Atoi(q.Get("page"))
	v.Page = max(v.Page, 1)
	st := s.store.Q()
	rows, total, err := st.ListSuggestions(ctx, store.SuggestionFilter{
		Type: suggest.TypeImage, Statuses: []string{store.StatusPending},
		Limit: reviewImagePageSize, Offset: (v.Page - 1) * reviewImagePageSize,
	})
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	v.Total = total
	v.Pages = max((total+reviewImagePageSize-1)/reviewImagePageSize, 1)
	v.Self = reviewImagesLink(v.Page)
	if v.Page > 1 {
		v.PrevURL = reviewImagesLink(v.Page - 1)
	}
	if v.Page < v.Pages {
		v.NextURL = reviewImagesLink(v.Page + 1)
	}
	entries := map[string]*store.Entry{}
	for _, sg := range rows {
		c := reviewImageCard{
			ID: sg.ID, EntryID: sg.EntryID, EntryName: sg.Title, Image: reviewImagePayload(sg),
			Reason: sg.Reason, EntryState: "missing",
		}
		e, seen := entries[sg.EntryID]
		if !seen {
			row, err := st.GetEntry(ctx, sg.EntryID)
			switch {
			case errors.Is(err, store.ErrNotFound):
			case err != nil:
				s.adminFail(w, r, err)
				return
			default:
				e = &row
			}
			entries[sg.EntryID] = e
		}
		if e != nil {
			c.EntryRev, c.EntryState = e.Rev, "live"
			if e.DeletedAt != nil {
				c.EntryState = "removed"
			}
			var doc struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(e.Doc, &doc) == nil && doc.Name != "" {
				c.EntryName = doc.Name
			}
			c.Current = reviewCurrentImage(e.Doc)
			c.Conflict = reviewConflict(sg, *e)
		}
		v.Cards = append(v.Cards, c)
	}
	s.render(w, http.StatusOK, "admin_images.html", v)
}
