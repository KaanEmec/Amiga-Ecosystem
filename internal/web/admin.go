package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"amigaconstellation/internal/auth"
	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/images"
	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
)

// adminDeps holds the owner authentication and private panel feature.
type adminDeps struct {
	auth   *auth.Service
	images *images.Fetcher
	staged reviewStaging // image previews fetched for pending suggestions
}

// WithAdmin enables /admin: owner sign-in, catalogue management and the
// owner-triggered image fetcher.
func WithAdmin(a *auth.Service, f *images.Fetcher) Option {
	return func(s *Server) { s.admin = &adminDeps{auth: a, images: f} }
}

// adminActor is recorded as the actor of every owner publication.
const adminActor = "owner"

// registerAdminRoutes mounts /admin routes.
func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	if s.admin == nil {
		return
	}
	open := func(h http.HandlerFunc) http.Handler { return adminHeaders(h) }
	owner := func(h http.HandlerFunc) http.Handler { return adminHeaders(s.requireOwner(h)) }

	mux.Handle("GET /admin/login", open(s.adminLoginPage))
	mux.Handle("POST /admin/login", open(s.adminLogin))
	mux.Handle("POST /admin/logout", owner(s.adminLogout))
	mux.Handle("GET /admin", owner(s.adminDashboard))
	mux.Handle("GET /admin/{$}", owner(s.adminDashboard))
	mux.Handle("GET /admin/entries", owner(s.adminEntries))
	mux.Handle("GET /admin/entries/new", owner(s.adminNewEntry))
	mux.Handle("POST /admin/entries/new", owner(s.adminCreateEntry))
	mux.Handle("GET /admin/entries/{id}", owner(s.adminEditEntry))
	mux.Handle("POST /admin/entries/{id}", owner(s.adminSaveEntry))
	mux.Handle("POST /admin/entries/{id}/image", owner(s.adminAttachImage))
	mux.Handle("POST /admin/entries/{id}/revert", owner(s.adminRevertEntry))
	mux.Handle("GET /admin/entries/{id}/remove", owner(s.adminRemovePage))
	mux.Handle("POST /admin/entries/{id}/remove", owner(s.adminRemoveEntry))
	mux.Handle("POST /admin/entries/{id}/restore", owner(s.adminRestoreEntry))
	mux.Handle("GET /admin/queue", owner(s.reviewQueue))
	mux.Handle("GET /admin/suggestions/import", owner(s.adminSuggestImportPage))
	mux.Handle("POST /admin/suggestions/import", owner(s.adminSuggestImport))
	mux.Handle("GET /admin/suggestions/{id}", owner(s.reviewPage))
	mux.Handle("POST /admin/suggestions/{id}/accept", owner(s.reviewAccept))
	mux.Handle("POST /admin/suggestions/{id}/reject", owner(s.reviewReject))
	mux.Handle("POST /admin/suggestions/{id}/duplicate", owner(s.reviewDuplicate))
	mux.Handle("POST /admin/suggestions/{id}/hide", owner(s.reviewHide))
	mux.Handle("POST /admin/suggestions/{id}/preview", owner(s.reviewPreview))
	mux.Handle("GET /admin/staged/{name}", owner(s.reviewStagedFile))
	mux.Handle("GET /admin/stats", owner(s.adminStats))
	mux.Handle("GET /admin/settings", owner(s.adminSettingsPage))
	mux.Handle("POST /admin/settings", owner(s.adminSaveSettings))
	mux.Handle("GET /admin/export", owner(s.adminExport))
	mux.Handle("GET /admin/import", owner(s.adminImportPage))
	mux.Handle("POST /admin/import", owner(s.adminImport))
	// Anything else under /admin/ still requires the owner, so unknown
	// paths reveal nothing to anonymous visitors.
	mux.Handle("/admin/", owner(http.NotFound))
}

// adminHeaders marks every admin response private and unindexable.
func adminHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

type adminCtxKey struct{}

type adminSession struct {
	sess auth.Session
	csrf string
}

// requireOwner lets a request through only with a valid session; others
// are redirected to /admin/login. Mutations additionally need a same-origin
// request and the session's CSRF form token.
func (s *Server) requireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := s.admin.auth
		sess, err := a.Session(r.Context(), auth.TokenFromRequest(r))
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				s.log.ErrorContext(r.Context(), "admin session", "err", err)
			}
			a.ClearCookie(w)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		as := adminSession{sess: sess, csrf: a.CSRFToken(sess)}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.sameOriginMutation(r, allowHeaderless) || !a.ValidCSRF(sess, r.FormValue("csrf")) {
				http.Error(w, "forbidden: cross-site or expired form; reload the page and try again", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminCtxKey{}, as)))
	})
}

func adminSessionFrom(r *http.Request) adminSession {
	as, _ := r.Context().Value(adminCtxKey{}).(adminSession)
	return as
}

// adminView is the data every admin page shares with the layout.
type adminView struct {
	Title    string
	Section  string
	CSRF     string
	SignedIn bool
	Notice   string
	Error    string
	Problems []string
}

// adminNotices maps ?ok= codes to fixed messages (no reflected text).
var adminNotices = map[string]string{
	"created":  "Entry created and published.",
	"saved":    "Saved and published as a new revision.",
	"removed":  "Entry removed. Related links and tour stops were cleaned up.",
	"restored": "Entry restored as a new revision.",
	"reverted": "Earlier version published as a new revision.",
	"image":    "Image fetched, stored locally and attached.",
	"settings": "Settings saved.",
	// Review decisions (admin_review.go).
	"accepted":        "Suggestion accepted and published.",
	"accepted_edited": "Suggestion accepted with your edits and published.",
	"rejected":        "Suggestion rejected.",
	"duplicate":       "Suggestion marked as a duplicate.",
	"hidden":          "Suggestion hidden as spam. It no longer appears on public pages.",
	"staged":          "Image fetched and staged privately. Review it below before accepting.",
}

func (s *Server) adminViewFor(r *http.Request, title, section string) adminView {
	as := adminSessionFrom(r)
	v := adminView{Title: title, Section: section, CSRF: as.csrf, SignedIn: as.csrf != ""}
	ok := r.URL.Query().Get("ok")
	if msg, found := adminNotices[ok]; found {
		v.Notice = msg
	}
	if ok == "recovery" {
		left, _ := strconv.Atoi(r.URL.Query().Get("left"))
		v.Notice = fmt.Sprintf("Signed in with a recovery code; %d left. Regenerate them with `amiga owner recovery-codes`.", max(left, 0))
	}
	return v
}

// adminFail logs an unexpected error and shows a generic page.
func (s *Server) adminFail(w http.ResponseWriter, r *http.Request, err error) {
	s.log.ErrorContext(r.Context(), "admin", "path", r.URL.Path, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// adminProblems turns publication errors into a message and problem list
// for the page, with the HTTP status to use. ok is false for unexpected
// errors.
func adminProblems(err error) (status int, msg string, problems []string, ok bool) {
	var ve *catalog.ValidationError
	var se *catalog.StaleError
	switch {
	case errors.As(err, &ve):
		return http.StatusUnprocessableEntity, "Not published: the catalogue would not validate.", ve.Problems, true
	case errors.As(err, &se):
		return http.StatusConflict, fmt.Sprintf("Not published: this entry changed since you opened it (now rev %d, you edited rev %d). Reload it, review the newer version and reapply your edits.", se.Current, se.Expected), nil, true
	case errors.Is(err, catalog.ErrExists):
		return http.StatusConflict, "Not published: an entry with this id already exists (it may be removed; restore it instead).", nil, true
	case errors.Is(err, catalog.ErrDeleted):
		return http.StatusConflict, "Not published: this entry is removed. Restore it first.", nil, true
	case errors.Is(err, catalog.ErrNotDeleted):
		return http.StatusConflict, "This entry is not removed.", nil, true
	case errors.Is(err, catalog.ErrNotFound):
		return http.StatusNotFound, "No such entry.", nil, true
	}
	return 0, "", nil, false
}

// --- sign-in -----------------------------------------------------------

type adminLoginView struct {
	adminView
	Username string
	NoOwner  bool
}

func (s *Server) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	// An already signed-in owner goes straight to the dashboard.
	if _, err := s.admin.auth.Session(r.Context(), auth.TokenFromRequest(r)); err == nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.adminRenderLogin(w, r, http.StatusOK, "", "")
}

func (s *Server) adminRenderLogin(w http.ResponseWriter, r *http.Request, status int, username, msg string) {
	has, err := s.admin.auth.HasOwner(r.Context())
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	v := adminLoginView{adminView: adminView{Title: "Sign in", Error: msg}, Username: username, NoOwner: !has}
	s.render(w, status, "admin_login.html", v)
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.sameOriginMutation(r, allowHeaderless) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := r.PostFormValue("username")
	res, err := s.admin.auth.Login(r.Context(), username, r.PostFormValue("password"), r.PostFormValue("code"), s.visitorIP(r))
	switch {
	case errors.Is(err, auth.ErrThrottled):
		s.adminRenderLogin(w, r, http.StatusTooManyRequests, username, "Too many failed attempts. Wait 15 minutes and try again.")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.adminRenderLogin(w, r, http.StatusUnauthorized, username, "Sign-in failed. Check your username, password and code.")
		return
	case err != nil:
		s.adminFail(w, r, err)
		return
	}
	// Rotate: any session presented with this request is ended.
	if old := auth.TokenFromRequest(r); old != "" {
		if err := s.admin.auth.Logout(r.Context(), old); err != nil {
			s.log.ErrorContext(r.Context(), "ending previous session", "err", err)
		}
	}
	s.admin.auth.SetCookie(w, res.Token)
	target := "/admin"
	if res.UsedRecoveryCode {
		target = "/admin?ok=recovery&left=" + strconv.Itoa(res.RecoveryCodesLeft)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.admin.auth.Logout(r.Context(), auth.TokenFromRequest(r)); err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.admin.auth.ClearCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// --- dashboard -----------------------------------------------------------

type adminEvent struct {
	When         int64
	Action       string
	EntryID      string
	SuggestionID int64
	Detail       string
}

type adminDashboardView struct {
	adminView
	Counts            store.AdminCounts
	Visitors7         int // unique visitors over the last 7 days (analytics.go)
	Events            []adminEvent
	SubmissionsPaused bool
	VotingPaused      bool
}

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.store.Q()
	counts, err := q.CountForAdmin(ctx)
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	log, err := q.ListModeration(ctx, "", 15)
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	week, err := q.StatsSummary(ctx, daysAgo(time.Now(), 6))
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	v := adminDashboardView{adminView: s.adminViewFor(r, "Dashboard", "dashboard"), Counts: counts, Visitors7: week.Visitors}
	for _, ev := range log {
		e := adminEvent{When: ev.CreatedAt, Action: ev.Action, Detail: string(ev.Detail)}
		if ev.EntryID != nil {
			e.EntryID = *ev.EntryID
		}
		if ev.SuggestionID != nil {
			e.SuggestionID = *ev.SuggestionID
		}
		v.Events = append(v.Events, e)
	}
	if v.SubmissionsPaused, err = q.SettingBool(ctx, store.SettingSubmissionsPaused); err != nil {
		s.adminFail(w, r, err)
		return
	}
	if v.VotingPaused, err = q.SettingBool(ctx, store.SettingVotingPaused); err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "admin_dashboard.html", v)
}

// --- settings -------------------------------------------------------------

type adminSettingsView struct {
	adminView
	SubmissionsPaused bool
	VotingPaused      bool
	HiddenCount       int
	Hidden            []store.Suggestion // most recently hidden first
}

func (s *Server) adminSettingsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.store.Q()
	v := adminSettingsView{adminView: s.adminViewFor(r, "Settings", "settings")}
	var err error
	if v.SubmissionsPaused, err = q.SettingBool(ctx, store.SettingSubmissionsPaused); err != nil {
		s.adminFail(w, r, err)
		return
	}
	if v.VotingPaused, err = q.SettingBool(ctx, store.SettingVotingPaused); err != nil {
		s.adminFail(w, r, err)
		return
	}
	if v.Hidden, v.HiddenCount, err = q.ListSuggestions(ctx, store.SuggestionFilter{
		Statuses: []string{store.StatusHidden}, Limit: 10,
	}); err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "admin_settings.html", v)
}

func (s *Server) adminSaveSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.store.Q()
	for _, key := range []string{store.SettingSubmissionsPaused, store.SettingVotingPaused} {
		val := "0"
		if r.PostFormValue(key) == "1" {
			val = "1"
		}
		if err := q.SetSetting(ctx, key, val); err != nil {
			s.adminFail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/settings?ok=settings", http.StatusSeeOther)
}

// --- export / import --------------------------------------------------------

func (s *Server) adminExport(w http.ResponseWriter, r *http.Request) {
	doc, err := s.catalog.Export(r.Context())
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	name := "amiga-data-" + time.Now().UTC().Format("20060102-150405") + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if err := doc.Encode(w, true); err != nil {
		s.log.ErrorContext(r.Context(), "export", "err", err)
	}
}

type adminImportView struct {
	adminView
	Stats *catalog.ImportStats
}

func (s *Server) adminImportPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "admin_import.html", adminImportView{adminView: s.adminViewFor(r, "Import", "data")})
}

func (s *Server) adminImport(w http.ResponseWriter, r *http.Request) {
	v := adminImportView{adminView: s.adminViewFor(r, "Import", "data")}
	fail := func(status int, msg string, problems []string) {
		v.Error, v.Problems = msg, problems
		s.render(w, status, "admin_import.html", v)
	}
	if r.PostFormValue("confirm") != "1" {
		fail(http.StatusUnprocessableEntity, "Tick the confirmation box: import replaces the whole catalogue.", nil)
		return
	}
	file, _, err := r.FormFile("catalog")
	if err != nil {
		fail(http.StatusUnprocessableEntity, "Choose a catalogue JSON file (it must be under 1 MB).", nil)
		return
	}
	defer file.Close()
	doc, err := catalog.DecodeDocument(file)
	if err != nil {
		fail(http.StatusUnprocessableEntity, "Not imported: "+err.Error(), nil)
		return
	}
	stats, err := s.catalog.ImportReplace(r.Context(), doc, adminActor)
	if err != nil {
		if status, msg, problems, ok := adminProblems(err); ok {
			fail(status, msg, problems)
			return
		}
		s.adminFail(w, r, err)
		return
	}
	v.Stats = &stats
	v.Notice = "Catalogue imported and published."
	s.render(w, http.StatusOK, "admin_import.html", v)
}

// --- suggestion import --------------------------------------------------------

type adminSuggestImportRow struct {
	Index    int // 1-based position in the file
	Type     string
	Target   string // entry id, or the proposed name for a new entry
	ID       int64  // created suggestion; 0 when not created
	Problems []string
}

type adminSuggestImportView struct {
	adminView
	Rows             []adminSuggestImportRow
	Created, Refused int
}

func (s *Server) adminSuggestImportPage(w http.ResponseWriter, r *http.Request) {
	if s.suggest == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, http.StatusOK, "admin_suggestions_import.html", adminSuggestImportView{adminView: s.adminViewFor(r, "Import suggestions", "queue")})
}

// adminSuggestImport files every proposal of an uploaded JSON array as a
// pending suggestion (suggest.CreateOwner), reporting each item's outcome.
func (s *Server) adminSuggestImport(w http.ResponseWriter, r *http.Request) {
	if s.suggest == nil {
		http.NotFound(w, r)
		return
	}
	v := adminSuggestImportView{adminView: s.adminViewFor(r, "Import suggestions", "queue")}
	fail := func(msg string) {
		v.Error = msg
		s.render(w, http.StatusUnprocessableEntity, "admin_suggestions_import.html", v)
	}
	file, _, err := r.FormFile("proposals")
	if err != nil {
		fail("Choose a proposals JSON file (it must be under 1 MB).")
		return
	}
	defer file.Close()
	var items []json.RawMessage
	if err := json.NewDecoder(file).Decode(&items); err != nil {
		fail("Not imported: the file must be a JSON array of proposals (" + err.Error() + ").")
		return
	}
	for i, raw := range items {
		row := adminSuggestImportRow{Index: i + 1}
		var p suggest.Proposal
		if err := json.Unmarshal(raw, &p); err != nil {
			row.Problems = []string{"Not a proposal object: " + err.Error()}
		} else {
			row.Type, row.Target = p.Type, p.EntryID
			if p.Type == suggest.TypeNew && p.Fields.Name != nil {
				row.Target = *p.Fields.Name
			}
			var ve *suggest.ValidationError
			row.ID, err = s.suggest.svc.CreateOwner(r.Context(), p)
			switch {
			case errors.As(err, &ve):
				row.Problems = ve.Problems
			case err != nil:
				s.log.ErrorContext(r.Context(), "suggestion import", "item", row.Index, "err", err)
				row.Problems = []string{"Not stored: internal error (see the server log)."}
			}
		}
		if row.ID > 0 {
			v.Created++
		} else {
			v.Refused++
		}
		v.Rows = append(v.Rows, row)
	}
	v.Notice = fmt.Sprintf("%d of %d proposals added to the queue.", v.Created, len(items))
	s.render(w, http.StatusOK, "admin_suggestions_import.html", v)
}
