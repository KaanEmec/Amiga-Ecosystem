package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
	"amigaconstellation/internal/turnstile"
)

// Form timing bounds: faster than a person can fill the form, or stale.
const (
	formMinAge = 3 * time.Second
	formMaxAge = 24 * time.Hour
)

// suggestDeps holds the public suggestions/votes feature.
type suggestDeps struct {
	svc      *suggest.Service
	verifier turnstile.Verifier
	siteKey  string // Turnstile widget key; "" when the widget is not rendered
	signer   *suggest.Signer
	search   *windowLimiter // /api/entries/search
	now      func() time.Time
}

// WithSuggest enables the public suggestion pages and APIs. widget reports
// whether the Turnstile widget is rendered (see turnstile.FromConfig).
func WithSuggest(svc *suggest.Service, verifier turnstile.Verifier, widget bool) Option {
	return func(s *Server) {
		d := &suggestDeps{svc: svc, verifier: verifier, signer: suggest.NewSigner(s.cfg.Secret),
			search: newWindowLimiter(searchWindow), now: time.Now}
		if widget {
			d.siteKey = s.cfg.TurnstileSiteKey
		}
		s.suggest = d
	}
}

// registerSuggestRoutes mounts the public suggestion and vote routes.
func (s *Server) registerSuggestRoutes(mux *http.ServeMux) {
	if s.suggest == nil {
		return
	}
	mux.HandleFunc("GET /suggestions", s.handleSuggestionList)
	mux.HandleFunc("GET /suggestions/new", s.handleSuggestionForm)
	mux.HandleFunc("POST /suggestions/new", s.handleSuggestionSubmit)
	mux.HandleFunc("GET /suggestions/{id}", s.handleSuggestionDetail)
	mux.HandleFunc("GET /api/suggestions", s.handleAPISuggestionList)
	mux.HandleFunc("PUT /api/suggestions/{id}/vote", s.handleAPIVote(true))
	mux.HandleFunc("DELETE /api/suggestions/{id}/vote", s.handleAPIVote(false))
	mux.HandleFunc("GET /api/entries/search", s.handleAPIEntrySearch)
}

// ---------------------------------------------------------------- pages

func (s *Server) handleSuggestionList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vid := s.ensureVisitor(w, r)
	params := listParams(r, vid)
	res, err := s.suggest.svc.List(ctx, params)
	if err != nil {
		s.suggestFail(w, r, err)
		return
	}
	subPaused, votePaused, err := s.suggest.svc.Paused(ctx)
	if err != nil {
		s.suggestFail(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "suggestions_list.html", s.newListPage(params.Normalize(), res, subPaused, votePaused))
}

func (s *Server) handleSuggestionDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		s.renderSuggestError(w, http.StatusNotFound, "Suggestion not found", "There is no suggestion with that number.")
		return
	}
	vid := s.ensureVisitor(w, r)
	sg, err := s.suggest.svc.Get(ctx, id, vid)
	if errors.Is(err, suggest.ErrNotFound) {
		s.renderSuggestError(w, http.StatusNotFound, "Suggestion not found", "There is no suggestion with that number, or it was removed.")
		return
	}
	if err != nil {
		s.suggestFail(w, r, err)
		return
	}
	page, err := s.newDetailPage(r, sg)
	if err != nil {
		s.suggestFail(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "suggestions_detail.html", page)
}

func (s *Server) handleSuggestionForm(w http.ResponseWriter, r *http.Request) {
	s.ensureVisitor(w, r)
	q := r.URL.Query()
	v := formValues{Type: q.Get("type"), Entry: strings.TrimSpace(q.Get("entry"))}
	if v.Type == "" {
		v.Type = suggest.TypeNew
		if v.Entry != "" {
			v.Type = suggest.TypeChange
		}
	}
	if v.Entry != "" && v.Type == suggest.TypeChange {
		if _, doc, err := s.suggest.svc.Entry(r.Context(), v.Entry); err == nil {
			v.fillFromEntry(doc)
		}
	}
	s.renderSuggestForm(w, r, http.StatusOK, v, nil)
}

// handleSuggestionSubmit is the HTML form POST. Every failure re-renders the
// form with the visitor's input and a fresh token and widget.
func (s *Server) handleSuggestionSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.sameOriginMutation(r, false) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	v := formFromRequest(r)
	fail := func(status int, msgs ...string) { s.renderSuggestForm(w, r, status, v, msgs) }

	vid, ok := s.visitorID(r)
	if !ok {
		s.ensureVisitor(w, r)
		fail(http.StatusForbidden, "Your browser did not send the suggestion cookie. Cookies must be enabled for this site; please submit again.")
		return
	}
	if r.PostForm.Get("website") != "" {
		fail(http.StatusBadRequest, "Your suggestion could not be accepted.")
		return
	}
	switch err := s.suggest.signer.CheckFormToken(r.PostForm.Get("form_ts"), s.suggest.now(), formMinAge, formMaxAge); {
	case errors.Is(err, suggest.ErrFormTooFast):
		fail(http.StatusBadRequest, "That was quick! Please check your suggestion and submit again.")
		return
	case errors.Is(err, suggest.ErrFormExpired):
		fail(http.StatusBadRequest, "The form expired. Please submit again.")
		return
	case err != nil:
		fail(http.StatusBadRequest, "The form is incomplete. Please submit again.")
		return
	}
	if problems := v.inputProblems(); len(problems) > 0 {
		fail(http.StatusUnprocessableEntity, problems...)
		return
	}
	if err := s.suggest.verifier.Verify(ctx, r.PostForm.Get("cf-turnstile-response"), s.visitorIP(r), turnstile.ActionSuggest); err != nil {
		s.log.InfoContext(ctx, "turnstile rejected suggestion", "err", err)
		fail(http.StatusForbidden, "The anti-spam check failed or expired. Please complete it again.")
		return
	}
	id, err := s.suggest.svc.Create(ctx, v.proposal(), s.visitor(r, vid))
	if err != nil {
		status, msg, problems := suggestErrorInfo(err)
		if status == http.StatusInternalServerError {
			s.log.ErrorContext(ctx, "creating suggestion", "err", err)
		}
		if len(problems) == 0 {
			problems = []string{msg}
		}
		fail(status, problems...)
		return
	}
	http.Redirect(w, r, "/suggestions/"+strconv.FormatInt(id, 10)+"?created=1", http.StatusSeeOther)
}

// ---------------------------------------------------------------- JSON API

func (s *Server) handleAPISuggestionList(w http.ResponseWriter, r *http.Request) {
	vid, _ := s.visitorID(r)
	res, err := s.suggest.svc.List(r.Context(), listParams(r, vid))
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAPIEntrySearch(w http.ResponseWriter, r *http.Request) {
	if !s.allowSearch(r) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, apiErr{Error: suggest.ErrRateLimited.Error()})
		return
	}
	hints, err := s.suggest.svc.Similar(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, hints)
}

// apiMutation runs the checks every JSON mutation needs and returns the
// browser id; on failure it has already written the response.
func (s *Server) apiMutation(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.sameOriginMutation(r, false) || r.Header.Get("X-Requested-With") != "fetch" {
		writeJSON(w, http.StatusForbidden, apiErr{Error: "cross-origin request refused"})
		return "", false
	}
	vid, ok := s.visitorID(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, apiErr{Error: "missing browser cookie; reload the page and try again"})
		return "", false
	}
	return vid, true
}

type apiErr struct {
	Error     string   `json:"error"`
	Problems  []string `json:"problems,omitempty"`
	Turnstile bool     `json:"turnstile,omitempty"` // the widget must be reset
}

func (s *Server) handleAPIVote(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		vid, ok := s.apiMutation(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			writeJSON(w, http.StatusNotFound, apiErr{Error: "suggestion not found"})
			return
		}
		if err := s.suggest.verifier.Verify(ctx, r.Header.Get("X-Turnstile-Token"), s.visitorIP(r), turnstile.ActionVote); err != nil {
			s.log.InfoContext(ctx, "turnstile rejected vote", "err", err)
			writeJSON(w, http.StatusForbidden, apiErr{Error: "the anti-spam check failed or expired", Turnstile: true})
			return
		}
		n, err := s.suggest.svc.ToggleVote(ctx, id, s.visitor(r, vid), on)
		if err != nil {
			s.apiError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"votes": n, "voted": on})
	}
}

// ---------------------------------------------------------------- errors

// suggestErrorInfo maps service errors to a status, a message and the
// validation problems (if any).
func suggestErrorInfo(err error) (int, string, []string) {
	var ve *suggest.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusUnprocessableEntity, "please fix the problems listed", ve.Problems
	case errors.Is(err, suggest.ErrRateLimited):
		return http.StatusTooManyRequests, suggest.ErrRateLimited.Error(), nil
	case errors.Is(err, suggest.ErrSubmissionsPaused):
		return http.StatusServiceUnavailable, suggest.ErrSubmissionsPaused.Error(), nil
	case errors.Is(err, suggest.ErrVotingPaused):
		return http.StatusServiceUnavailable, suggest.ErrVotingPaused.Error(), nil
	case errors.Is(err, suggest.ErrClosed):
		return http.StatusConflict, suggest.ErrClosed.Error(), nil
	case errors.Is(err, suggest.ErrNotFound):
		return http.StatusNotFound, "suggestion not found", nil
	case errors.Is(err, suggest.ErrNoVisitor):
		return http.StatusForbidden, "missing browser cookie; reload the page and try again", nil
	}
	return http.StatusInternalServerError, "something went wrong; please try again", nil
}

func (s *Server) apiError(w http.ResponseWriter, r *http.Request, err error) {
	status, msg, problems := suggestErrorInfo(err)
	if status == http.StatusInternalServerError {
		s.log.ErrorContext(r.Context(), "suggestions api", "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, apiErr{Error: msg, Problems: problems})
}

// suggestFail logs an unexpected error and renders a generic error page.
func (s *Server) suggestFail(w http.ResponseWriter, r *http.Request, err error) {
	s.log.ErrorContext(r.Context(), "suggestions page", "path", r.URL.Path, "err", err)
	s.renderSuggestError(w, http.StatusInternalServerError, "Something went wrong", "Please try again in a moment.")
}

func listParams(r *http.Request, voterID string) suggest.ListParams {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	return suggest.ListParams{
		Query: q.Get("q"), Type: q.Get("type"), Status: q.Get("status"), Sort: q.Get("sort"),
		Page: page, VoterID: voterID,
	}
}

// statusInfo is the public label and tone of a suggestion status.
func statusInfo(st string) (label, tone string) {
	switch st {
	case store.StatusPending:
		return "Pending", "info"
	case store.StatusAccepted:
		return "Accepted", "good"
	case store.StatusAcceptedEdited:
		return "Accepted with edits", "good"
	case store.StatusRejected:
		return "Rejected", "off"
	case store.StatusDuplicate:
		return "Duplicate", "warn"
	}
	return st, "off"
}

func typeLabel(t string) string {
	switch t {
	case suggest.TypeNew:
		return "New entry"
	case suggest.TypeChange:
		return "Change"
	case suggest.TypeRemove:
		return "Removal"
	case suggest.TypeImage:
		return "Image"
	}
	return t
}
