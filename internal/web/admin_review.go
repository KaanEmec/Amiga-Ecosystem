package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/images"
	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
)

// Owner review of visitor suggestions: the queue, the three-panel review
// page and the decisions. Accepting new/change/remove/image publishes
// through the catalog primitives with a TxHook that records the decision
// and its moderation event in the same transaction.

const reviewPageSize = 25

// --- staged image previews --------------------------------------------------

// reviewStaging remembers the image staged for each pending image
// suggestion, so the review page can show it and Accept can publish it.
// It lives in memory: after a restart the owner previews again (the old
// staged file stays in the private staging dir).
type reviewStaging struct {
	mu sync.Mutex
	m  map[int64]images.Staged
}

func (rs *reviewStaging) get(id int64) (images.Staged, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	st, ok := rs.m[id]
	return st, ok
}

// put stores st for id and returns the one it replaces, if any.
func (rs *reviewStaging) put(id int64, st images.Staged) (images.Staged, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.m == nil {
		rs.m = map[int64]images.Staged{}
	}
	old, ok := rs.m[id]
	rs.m[id] = st
	return old, ok
}

func (rs *reviewStaging) take(id int64) (images.Staged, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	st, ok := rs.m[id]
	delete(rs.m, id)
	return st, ok
}

// has reports whether name is a currently staged preview.
func (rs *reviewStaging) has(name string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, st := range rs.m {
		if st.Name == name {
			return true
		}
	}
	return false
}

// reviewDiscardStaged deletes the staged preview of suggestion id, if any.
func (s *Server) reviewDiscardStaged(ctx context.Context, id int64) {
	if st, ok := s.admin.staged.take(id); ok {
		if err := s.admin.images.Discard(st); err != nil {
			s.log.WarnContext(ctx, "discarding staged image", "suggestion", id, "err", err)
		}
	}
}

// --- queue --------------------------------------------------------------------

type reviewQueueRow struct {
	ID          int64
	Type        string
	TypeLabel   string
	Title       string
	Summary     string
	EntryID     string
	Status      string
	StatusLabel string
	Votes       int
	Created     int64
	Stale       bool
}

type reviewQueueView struct {
	adminView
	Type, Status, Entry, Sort string
	Types, Statuses, Sorts    []adminOpt
	Rows                      []reviewQueueRow
	Total, Page, Pages        int
	PrevURL, NextURL          string
}

var reviewStatusFilters = []adminMetaItem{
	{ID: store.StatusPending, Label: "Pending"},
	{ID: store.StatusAccepted, Label: "Accepted"},
	{ID: store.StatusAcceptedEdited, Label: "Accepted with edits"},
	{ID: store.StatusRejected, Label: "Rejected"},
	{ID: store.StatusDuplicate, Label: "Duplicate"},
	{ID: store.StatusHidden, Label: "Hidden (spam)"},
	{ID: "all", Label: "Any status"},
}

var reviewSorts = []adminMetaItem{
	{ID: "oldest", Label: "Oldest first"},
	{ID: "newest", Label: "Newest first"},
	{ID: "support", Label: "Most support"},
}

func reviewKnown(items []adminMetaItem, id, fallback string) string {
	for _, it := range items {
		if it.ID == id {
			return id
		}
	}
	return fallback
}

func (s *Server) reviewQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	v := reviewQueueView{
		adminView: s.adminViewFor(r, "Review queue", "queue"),
		Status:    reviewKnown(reviewStatusFilters, q.Get("status"), store.StatusPending),
		Sort:      reviewKnown(reviewSorts, q.Get("sort"), "oldest"),
		Entry:     strings.TrimSpace(q.Get("entry")),
	}
	typeItems := []adminMetaItem{{ID: "", Label: "Any type"}}
	for _, t := range suggest.Types {
		typeItems = append(typeItems, adminMetaItem{ID: t, Label: typeLabel(t)})
	}
	v.Type = reviewKnown(typeItems, q.Get("type"), "")
	v.Types = adminOpts(typeItems, v.Type)
	v.Statuses = adminOpts(reviewStatusFilters, v.Status)
	v.Sorts = adminOpts(reviewSorts, v.Sort)
	v.Page, _ = strconv.Atoi(q.Get("page"))
	v.Page = max(v.Page, 1)

	f := store.SuggestionFilter{
		Type: v.Type, EntryID: v.Entry, Limit: reviewPageSize, Offset: (v.Page - 1) * reviewPageSize,
		MostSupported: v.Sort == "support", OldestFirst: v.Sort == "oldest",
	}
	if v.Status != "all" {
		f.Statuses = []string{v.Status}
	}
	st := s.store.Q()
	rows, total, err := st.ListSuggestions(ctx, f)
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	v.Total = total
	v.Pages = max((total+reviewPageSize-1)/reviewPageSize, 1)
	revs := map[string]int64{}
	for _, sg := range rows {
		row := reviewQueueRow{
			ID: sg.ID, Type: sg.Type, TypeLabel: typeLabel(sg.Type), Title: sg.Title, Summary: sg.Summary,
			EntryID: sg.EntryID, Status: sg.Status, StatusLabel: reviewStatusLabel(sg.Status), Votes: sg.Votes, Created: sg.CreatedAt,
		}
		if sg.Status == store.StatusPending && sg.BaseRev != nil && sg.EntryID != "" {
			rev, ok := revs[sg.EntryID]
			if !ok {
				e, err := st.GetEntry(ctx, sg.EntryID)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					s.adminFail(w, r, err)
					return
				}
				rev = e.Rev
				revs[sg.EntryID] = rev
			}
			row.Stale = rev > *sg.BaseRev
		}
		v.Rows = append(v.Rows, row)
	}
	link := func(page int) string {
		u := url.Values{"status": {v.Status}, "sort": {v.Sort}, "page": {strconv.Itoa(page)}}
		if v.Type != "" {
			u.Set("type", v.Type)
		}
		if v.Entry != "" {
			u.Set("entry", v.Entry)
		}
		return "/admin/queue?" + u.Encode()
	}
	if v.Page > 1 {
		v.PrevURL = link(v.Page - 1)
	}
	if v.Page < v.Pages {
		v.NextURL = link(v.Page + 1)
	}
	s.render(w, http.StatusOK, "admin_queue.html", v)
}

func reviewStatusLabel(st string) string {
	if st == store.StatusHidden {
		return "Hidden (spam)"
	}
	label, _ := statusInfo(st)
	return label
}

// --- review page ----------------------------------------------------------------

// reviewRow is one field line of a review panel.
type reviewRow struct {
	Key, Label        string
	Current, Proposed string
	Removed           bool   // the proposal removes this field
	Conflict          bool   // the live value changed since the proposal
	Snapshot          string // value the visitor saw, when Conflict
}

type reviewView struct {
	adminView
	S                      store.Suggestion
	TypeLabel, StatusLabel string
	Pending, Hidden        bool
	Sources                []string
	EntryName              string
	EntryRev               int64
	EntryState             string // live, removed, missing; "" for new
	Current                []reviewRow
	Diff                   []reviewRow
	Deps                   *catalog.Dependencies
	Image                  *imageView
	Staged                 *images.Staged
	Stale                  bool
	Newer                  []adminRevision
	Final                  []reviewRow
	FinalNote              string
	Weights                []adminOpt // accept form (new/change)
	Unverified             bool
	Edit                   *adminEntryView // edit & accept form (new/change)
	Credit, SourcePage     string          // edit & accept form (image)
	DecisionNote, Internal string          // re-rendered note inputs
	DuplicateOf            string
	ExistingNew            bool   // a "new" proposal whose id now exists
	OwnerWeight            string // accept form's weight
}

func reviewID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// reviewFormatter renders field values with vocabulary labels and entry names
// when the suggestions feature is wired, raw values otherwise.
func (s *Server) reviewFormatter(ctx context.Context, docs ...map[string]json.RawMessage) (valueFormatter, error) {
	if s.suggest == nil {
		return valueFormatter{}, nil
	}
	opts, err := s.suggest.svc.Options(ctx)
	if err != nil {
		return valueFormatter{}, err
	}
	var ids []string
	for _, d := range docs {
		ids = append(ids, relatedIDs(d)...)
	}
	names, err := s.suggest.svc.EntryNames(ctx, ids)
	if err != nil {
		return valueFormatter{}, err
	}
	return valueFormatter{opts: opts, names: names}, nil
}

// reviewImagePayload decodes an image suggestion's payload.
func reviewImagePayload(sg store.Suggestion) imageView {
	var img struct {
		URL        string `json:"image_url"`
		Credit     string `json:"credit"`
		SourcePage string `json:"source_page"`
	}
	_ = json.Unmarshal(sg.Payload, &img)
	return imageView{URL: img.URL, Credit: img.Credit, SourcePage: img.SourcePage}
}

// reviewMerge returns the document Accept publishes: for "change" the live
// document with the payload applied (null removes a field), for "new" the
// proposed entry. weight and unverified are the owner-only fields; an empty
// weight keeps the live one (change) or uses 2 (new).
func reviewMerge(sg store.Suggestion, live json.RawMessage, weight string, unverified bool) (json.RawMessage, []string) {
	switch sg.Type {
	case suggest.TypeNew:
		f, err := adminFormFromDoc(sg.Payload)
		if err != nil {
			return nil, []string{err.Error()}
		}
		f.Weight, f.Unverified = weight, unverified
		if f.Weight == "" {
			f.Weight = "2"
		}
		return adminDocFromForm(nil, f, true)
	case suggest.TypeChange:
		payload := decodeObject(sg.Payload)
		var fields []catalog.Field
		for _, k := range orderedKeys(payload) {
			if isNullJSON(payload[k]) {
				fields = append(fields, catalog.Field{Key: k, Remove: true})
			} else {
				fields = append(fields, catalog.Field{Key: k, Value: payload[k]})
			}
		}
		if weight != "" {
			n, err := strconv.Atoi(weight)
			if err != nil {
				return nil, []string{"weight must be a whole number"}
			}
			fields = append(fields, catalog.Field{Key: "weight", Value: n})
		}
		fields = append(fields, catalog.Field{Key: "unverified", Value: true, Remove: !unverified})
		doc, err := catalog.Patch(live, fields...)
		if err != nil {
			return nil, []string{err.Error()}
		}
		return doc, nil
	}
	return nil, []string{"only new and change proposals have an entry document"}
}

// reviewSameDoc compares two JSON documents by value (key order ignored).
func reviewSameDoc(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// reviewOwnerFields reads the live owner-only fields of an entry document.
func reviewOwnerFields(doc json.RawMessage) (weight string, unverified bool) {
	var m struct {
		Weight     *int `json:"weight"`
		Unverified bool `json:"unverified"`
	}
	_ = json.Unmarshal(doc, &m)
	if m.Weight != nil {
		weight = strconv.Itoa(*m.Weight)
	}
	return weight, m.Unverified
}

// reviewLoad builds the review page of suggestion id. Owner-only fields
// default to the live entry's (change) or weight 2 (new).
func (s *Server) reviewLoad(r *http.Request, id int64) (reviewView, error) {
	ctx := r.Context()
	q := s.store.Q()
	sg, err := q.GetSuggestion(ctx, id)
	if err != nil {
		return reviewView{}, err
	}
	v := reviewView{
		adminView: s.adminViewFor(r, fmt.Sprintf("Suggestion #%d", id), "queue"),
		S:         sg, TypeLabel: typeLabel(sg.Type), StatusLabel: reviewStatusLabel(sg.Status),
		Pending: sg.Status == store.StatusPending, Hidden: sg.Status == store.StatusHidden,
		EntryName: sg.Title,
	}
	_ = json.Unmarshal(sg.Sources, &v.Sources)

	var live store.Entry
	if sg.EntryID != "" {
		live, err = q.GetEntry(ctx, sg.EntryID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			v.EntryState = "missing"
		case err != nil:
			return reviewView{}, err
		case live.DeletedAt != nil:
			v.EntryState, v.EntryRev = "removed", live.Rev
		default:
			v.EntryState, v.EntryRev = "live", live.Rev
		}
	}
	liveMap, payload, snapshot, final := decodeObject(live.Doc), decodeObject(sg.Payload), decodeObject(sg.Snapshot), decodeObject(sg.Final)
	f, err := s.reviewFormatter(ctx, liveMap, payload, snapshot, final)
	if err != nil {
		return reviewView{}, err
	}
	if n, ok := liveMap["name"]; ok {
		_ = json.Unmarshal(n, &v.EntryName)
	}
	for _, k := range orderedKeys(liveMap) {
		v.Current = append(v.Current, reviewRow{Key: k, Label: fieldLabel(k), Proposed: f.format(k, liveMap[k])})
	}

	// Staleness: the entry moved on since the visitor saw it.
	if v.Pending && sg.BaseRev != nil && v.EntryState != "missing" && live.Rev > *sg.BaseRev {
		v.Stale = true
		revs, err := q.ListRevisions(ctx, sg.EntryID)
		if err != nil {
			return reviewView{}, err
		}
		for _, rv := range revs {
			if rv.Rev > *sg.BaseRev {
				v.Newer = append(v.Newer, adminRevisionOf(rv))
			}
		}
	}

	// Proposed differences.
	switch sg.Type {
	case suggest.TypeNew:
		for _, k := range orderedKeys(payload) {
			v.Diff = append(v.Diff, reviewRow{Key: k, Label: fieldLabel(k), Proposed: f.format(k, payload[k])})
		}
		var pid string
		_ = json.Unmarshal(payload["id"], &pid)
		if v.Pending && pid != "" {
			if _, err := q.GetEntry(ctx, pid); err == nil {
				v.ExistingNew = true
			} else if !errors.Is(err, store.ErrNotFound) {
				return reviewView{}, err
			}
		}
	case suggest.TypeChange:
		// Pending: compare with the live entry and flag fields edited since
		// the proposal. Decided: show what the visitor saw.
		cur := liveMap
		if !v.Pending {
			cur = snapshot
		}
		for _, k := range orderedKeys(payload) {
			row := reviewRow{Key: k, Label: fieldLabel(k), Current: f.format(k, cur[k]), Proposed: f.format(k, payload[k]), Removed: isNullJSON(payload[k])}
			if v.Pending && string(snapshot[k]) != string(liveMap[k]) {
				row.Conflict, row.Snapshot = true, f.format(k, snapshot[k])
			}
			v.Diff = append(v.Diff, row)
		}
	case suggest.TypeRemove:
		if v.EntryState == "live" {
			deps, err := s.catalog.Dependencies(ctx, sg.EntryID)
			if err != nil {
				return reviewView{}, err
			}
			v.Deps = &deps
		}
	case suggest.TypeImage:
		img := reviewImagePayload(sg)
		v.Image = &img
		v.Credit, v.SourcePage = img.Credit, img.SourcePage
		if st, ok := s.admin.staged.get(id); ok {
			v.Staged = &st
		}
	}

	// Final preview: the stored final once decided, otherwise what Accept
	// would publish with the default owner fields.
	if !v.Pending {
		for _, k := range orderedKeys(final) {
			v.Final = append(v.Final, reviewRow{Key: k, Label: fieldLabel(k), Proposed: f.format(k, final[k])})
		}
		return v, nil
	}
	switch sg.Type {
	case suggest.TypeNew, suggest.TypeChange:
		weight, unverified := reviewOwnerFields(live.Doc)
		v.OwnerWeight, v.Unverified = weight, unverified
		doc, problems := reviewMerge(sg, live.Doc, weight, unverified)
		if len(problems) > 0 {
			v.FinalNote = "The proposal cannot be merged: " + strings.Join(problems, "; ")
			break
		}
		merged := decodeObject(doc)
		mf, err := s.reviewFormatter(ctx, merged)
		if err != nil {
			return reviewView{}, err
		}
		for _, k := range orderedKeys(merged) {
			v.Final = append(v.Final, reviewRow{Key: k, Label: fieldLabel(k), Proposed: mf.format(k, merged[k]),
				Conflict: sg.Type == suggest.TypeChange && string(merged[k]) != string(liveMap[k])})
		}
		form, err := adminFormFromDoc(doc)
		if err != nil {
			return reviewView{}, err
		}
		ev := adminEntryView{IsNew: sg.Type == suggest.TypeNew, ID: sg.EntryID, Rev: live.Rev, Form: form}
		if ev.IsNew {
			ev.ID = form.ID
		}
		v.Edit = &ev
		v.OwnerWeight = form.Weight
	case suggest.TypeRemove:
		v.FinalNote = "Accepting removes this entry from the public catalogue and applies the cleanup listed under the proposed differences."
	case suggest.TypeImage:
		v.FinalNote = "Accepting publishes the staged copy under images/suggested/ and sets it as the entry's image with the credit below."
	}
	return v, nil
}

// reviewRender fills the form options and renders the review page.
func (s *Server) reviewRender(w http.ResponseWriter, r *http.Request, status int, v reviewView) {
	v.Weights = adminWeightOpts(v.OwnerWeight)
	if v.Edit != nil {
		if err := s.adminFillEntryView(r, v.Edit); err != nil {
			s.adminFail(w, r, err)
			return
		}
	}
	s.render(w, status, "admin_review.html", v)
}

func (s *Server) reviewPage(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	v, err := s.reviewLoad(r, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.reviewRender(w, r, http.StatusOK, v)
}

// reviewFail re-renders the review page with an error, keeping the owner's
// note inputs (and, when edit is non-nil, the edited entry form).
func (s *Server) reviewFail(w http.ResponseWriter, r *http.Request, id int64, status int, msg string, problems []string, edit *adminForm) {
	v, err := s.reviewLoad(r, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	v.Notice, v.Error, v.Problems = "", msg, problems
	v.DecisionNote = strings.TrimSpace(r.PostFormValue("decision_note"))
	v.Internal = strings.TrimSpace(r.PostFormValue("internal_note"))
	v.DuplicateOf = strings.TrimSpace(r.PostFormValue("duplicate_of"))
	if r.PostFormValue("edit") == "1" && v.S.Type == suggest.TypeImage {
		v.Credit = strings.TrimSpace(r.PostFormValue("credit"))
		v.SourcePage = strings.TrimSpace(r.PostFormValue("source_page"))
	}
	if edit != nil && v.Edit != nil {
		v.Edit.Form = *edit
	}
	s.reviewRender(w, r, status, v)
}

const reviewAlreadyDecided = "Already decided: this suggestion is no longer pending, so nothing was published."

// reviewPublishError maps an error from a decision to the page.
func (s *Server) reviewPublishError(w http.ResponseWriter, r *http.Request, id int64, err error, edit *adminForm) {
	if errors.Is(err, store.ErrNotPending) {
		s.reviewFail(w, r, id, http.StatusConflict, reviewAlreadyDecided, nil, nil)
		return
	}
	status, msg, problems, ok := adminProblems(err)
	if !ok {
		s.adminFail(w, r, err)
		return
	}
	if errors.Is(err, catalog.ErrStale) {
		msg = "Not published: the entry changed while you were reviewing. Review the newer revision below and accept again."
	}
	s.reviewFail(w, r, id, status, msg, problems, edit)
}

// --- decisions --------------------------------------------------------------------

// reviewLog records a decision in the moderation log.
func reviewLog(ctx context.Context, q store.Queries, sg store.Suggestion, entryID, status string, detail map[string]any, at int64) error {
	if detail == nil {
		detail = map[string]any{}
	}
	detail["type"] = sg.Type
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encoding moderation detail: %w", err)
	}
	ev := store.ModerationEvent{SuggestionID: &sg.ID, Action: "suggestion_" + status, Detail: raw, CreatedAt: at}
	if entryID != "" {
		ev.EntryID = &entryID
	}
	_, err = q.InsertModeration(ctx, ev)
	return err
}

// reviewDecideHook flips the suggestion's status and logs the decision
// inside the publication transaction; a suggestion that is no longer
// pending fails with store.ErrNotPending and rolls the publication back.
func reviewDecideHook(sg store.Suggestion, entryID string, d store.Decision) catalog.TxHook {
	return func(ctx context.Context, tx *sql.Tx) error {
		q := store.NewQueries(tx)
		if err := q.DecideSuggestion(ctx, sg.ID, d); err != nil {
			return err
		}
		return reviewLog(ctx, q, sg, entryID, d.Status, nil, d.At)
	}
}

// reviewDecision reads the note fields shared by all decision forms.
func reviewDecision(r *http.Request, status string) store.Decision {
	return store.Decision{
		Status:       status,
		DecisionNote: strings.TrimSpace(r.PostFormValue("decision_note")),
		InternalNote: strings.TrimSpace(r.PostFormValue("internal_note")),
		At:           time.Now().Unix(),
	}
}

// reviewPending loads a suggestion for a decision, answering 404 or
// "already decided" itself when it cannot proceed.
func (s *Server) reviewPending(w http.ResponseWriter, r *http.Request) (store.Suggestion, bool) {
	id, ok := reviewID(r)
	if !ok {
		http.NotFound(w, r)
		return store.Suggestion{}, false
	}
	sg, err := s.store.Q().GetSuggestion(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return store.Suggestion{}, false
	}
	if err != nil {
		s.adminFail(w, r, err)
		return store.Suggestion{}, false
	}
	if sg.Status != store.StatusPending {
		s.reviewFail(w, r, id, http.StatusConflict, reviewAlreadyDecided, nil, nil)
		return store.Suggestion{}, false
	}
	return sg, true
}

func reviewDone(w http.ResponseWriter, r *http.Request, id int64, ok string) {
	http.Redirect(w, r, fmt.Sprintf("/admin/suggestions/%d?ok=%s", id, ok), http.StatusSeeOther)
}

// reviewAccept handles Accept and, with edit=1, Edit & accept.
func (s *Server) reviewAccept(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sg, ok := s.reviewPending(w, r)
	if !ok {
		return
	}
	edit := r.PostFormValue("edit") == "1"
	rev := adminExpectedRev(r)
	reason := fmt.Sprintf("suggestion #%d accepted", sg.ID)

	// change, remove and image act on a live entry and may be stale.
	var live store.Entry
	if sg.Type != suggest.TypeNew {
		var err error
		live, err = s.store.Q().GetEntry(ctx, sg.EntryID)
		if errors.Is(err, store.ErrNotFound) {
			s.reviewPublishError(w, r, sg.ID, catalog.ErrNotFound, nil)
			return
		}
		if err != nil {
			s.adminFail(w, r, err)
			return
		}
		if live.DeletedAt != nil {
			s.reviewPublishError(w, r, sg.ID, catalog.ErrDeleted, nil)
			return
		}
		if sg.BaseRev != nil && live.Rev > *sg.BaseRev && r.PostFormValue("reviewed") != "1" {
			var keep *adminForm
			if edit && sg.Type == suggest.TypeChange {
				f := adminFormFromRequest(r)
				f.ID = sg.EntryID
				keep = &f
			}
			s.reviewFail(w, r, sg.ID, http.StatusConflict,
				"Not published: the entry changed after this proposal was made. Review the newer revisions and tick “I reviewed the newer changes”.", nil, keep)
			return
		}
	}

	d := reviewDecision(r, store.StatusAccepted)
	switch sg.Type {
	case suggest.TypeNew, suggest.TypeChange:
		weight, unverified := r.PostFormValue("weight"), r.PostFormValue("unverified") == "1"
		var form adminForm
		if edit {
			form = adminFormFromRequest(r)
			if sg.Type == suggest.TypeChange {
				form.ID = sg.EntryID
			}
			weight, unverified = form.Weight, form.Unverified
		}
		proposed, problems := reviewMerge(sg, live.Doc, weight, unverified)
		doc := proposed
		if edit {
			var base json.RawMessage
			if sg.Type == suggest.TypeChange {
				base = live.Doc
			}
			doc, problems = adminDocFromForm(base, form, sg.Type == suggest.TypeNew)
			if !reviewSameDoc(doc, proposed) {
				d.Status = store.StatusAcceptedEdited
				reason += " with edits"
			}
		}
		if len(problems) > 0 {
			s.reviewPublishError(w, r, sg.ID, &catalog.ValidationError{Problems: problems}, reviewKeep(edit, form))
			return
		}
		d.Final = doc
		var err error
		if sg.Type == suggest.TypeNew {
			id := form.ID
			if !edit {
				e, perr := catalog.ParseEntry(doc)
				if perr != nil {
					s.reviewPublishError(w, r, sg.ID, &catalog.ValidationError{Problems: []string{perr.Error()}}, nil)
					return
				}
				id = e.ID
			}
			_, err = s.catalog.Create(ctx, doc, adminActor, reason, &sg.ID, reviewDecideHook(sg, id, d))
		} else {
			_, err = s.catalog.Update(ctx, sg.EntryID, doc, rev, adminActor, reason, &sg.ID, reviewDecideHook(sg, sg.EntryID, d))
		}
		if err != nil {
			s.reviewPublishError(w, r, sg.ID, err, reviewKeep(edit, form))
			return
		}

	case suggest.TypeRemove:
		if r.PostFormValue("confirm") != "1" {
			s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, "Tick the confirmation box to remove this entry and clean up its links.", nil, nil)
			return
		}
		if _, err := s.catalog.Remove(ctx, sg.EntryID, rev, adminActor, reason, &sg.ID, reviewDecideHook(sg, sg.EntryID, d)); err != nil {
			s.reviewPublishError(w, r, sg.ID, err, nil)
			return
		}

	case suggest.TypeImage:
		if !s.reviewAcceptImage(w, r, sg, live, rev, edit, &d, reason) {
			return
		}

	default:
		s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, "Unknown proposal type.", nil, nil)
		return
	}
	reviewDone(w, r, sg.ID, d.Status)
}

func reviewKeep(edit bool, f adminForm) *adminForm {
	if !edit {
		return nil
	}
	return &f
}

// reviewAcceptImage publishes the staged image (fetching it first when the
// owner did not preview) and sets it as the entry's image. A failed fetch
// leaves the suggestion pending and the catalogue unchanged.
func (s *Server) reviewAcceptImage(w http.ResponseWriter, r *http.Request, sg store.Suggestion, live store.Entry, rev int64, edit bool, d *store.Decision, reason string) bool {
	ctx := r.Context()
	img := reviewImagePayload(sg)
	credit, sourcePage := img.Credit, img.SourcePage
	if edit {
		credit = strings.TrimSpace(r.PostFormValue("credit"))
		sourcePage = strings.TrimSpace(r.PostFormValue("source_page"))
		if credit != img.Credit || sourcePage != img.SourcePage {
			d.Status = store.StatusAcceptedEdited
			reason += " with edits"
		}
	}
	// Check the rev before downloading or moving anything into media/.
	if live.Rev != rev {
		s.reviewPublishError(w, r, sg.ID, &catalog.StaleError{ID: live.ID, Expected: rev, Current: live.Rev}, nil)
		return false
	}
	staged, ok := s.admin.staged.take(sg.ID)
	if !ok {
		var err error
		if staged, err = s.admin.images.Stage(ctx, img.URL); err != nil {
			s.log.InfoContext(ctx, "image fetch refused", "suggestion", sg.ID, "err", err)
			s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, "Image not fetched; the suggestion stays pending: "+err.Error(), nil, nil)
			return false
		}
	}
	pub, err := s.admin.images.Publish(staged, sg.EntryID, credit, sourcePage, &sg.ID)
	if err != nil {
		_ = s.admin.images.Discard(staged)
		s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, "Image not published; the suggestion stays pending: "+err.Error(), nil, nil)
		return false
	}
	// The provenance row is written in the catalogue transaction; on any
	// failure the moved file is removed again.
	published := false
	defer func() {
		if !published {
			s.undoImage(ctx, pub)
		}
	}()
	fields := []catalog.Field{{Key: "image", Value: pub.Path}, {Key: "imageCredit", Value: credit, Remove: credit == ""}}
	doc, err := catalog.Patch(live.Doc, fields...)
	if err != nil {
		s.adminFail(w, r, err)
		return false
	}
	final := map[string]string{"image": pub.Path}
	if credit != "" {
		final["imageCredit"] = credit
	}
	if d.Final, err = json.Marshal(final); err != nil {
		s.adminFail(w, r, err)
		return false
	}
	if _, err := s.catalog.Update(ctx, sg.EntryID, doc, rev, adminActor, reason+" (image from "+staged.SourceURL+")", &sg.ID,
		pub.Record, reviewDecideHook(sg, sg.EntryID, *d)); err != nil {
		s.reviewPublishError(w, r, sg.ID, err, nil)
		return false
	}
	published = true
	return true
}

// undoImage removes a published image file whose catalogue publication failed.
func (s *Server) undoImage(ctx context.Context, pub *images.Published) {
	if err := pub.Undo(); err != nil {
		s.log.WarnContext(ctx, "removing unpublished image", "path", pub.Path, "err", err)
	}
}

// reviewDecideOnly records a decision that publishes nothing.
func (s *Server) reviewDecideOnly(ctx context.Context, sg store.Suggestion, d store.Decision, detail map[string]any) error {
	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		if err := q.DecideSuggestion(ctx, sg.ID, d); err != nil {
			return err
		}
		return reviewLog(ctx, q, sg, sg.EntryID, d.Status, detail, d.At)
	})
}

func (s *Server) reviewReject(w http.ResponseWriter, r *http.Request) {
	sg, ok := s.reviewPending(w, r)
	if !ok {
		return
	}
	if err := s.reviewDecideOnly(r.Context(), sg, reviewDecision(r, store.StatusRejected), nil); err != nil {
		s.reviewPublishError(w, r, sg.ID, err, nil)
		return
	}
	s.reviewDiscardStaged(r.Context(), sg.ID)
	reviewDone(w, r, sg.ID, store.StatusRejected)
}

func (s *Server) reviewDuplicate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sg, ok := s.reviewPending(w, r)
	if !ok {
		return
	}
	raw := strings.TrimPrefix(strings.TrimSpace(r.PostFormValue("duplicate_of")), "#")
	canonical, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || canonical <= 0 || canonical == sg.ID {
		s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, "Enter the number of the original suggestion (not this one).", nil, nil)
		return
	}
	orig, err := s.store.Q().GetSuggestion(ctx, canonical)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, fmt.Sprintf("Suggestion #%d does not exist.", canonical), nil, nil)
		return
	case err != nil:
		s.adminFail(w, r, err)
		return
	case orig.Status == store.StatusDuplicate:
		s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity,
			fmt.Sprintf("Suggestion #%d is itself a duplicate; point to the original it duplicates instead.", canonical), nil, nil)
		return
	case orig.Status == store.StatusHidden:
		s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, fmt.Sprintf("Suggestion #%d is hidden; it cannot be the original.", canonical), nil, nil)
		return
	}
	d := reviewDecision(r, store.StatusDuplicate)
	d.DuplicateOf = &canonical
	if err := s.reviewDecideOnly(ctx, sg, d, map[string]any{"duplicate_of": canonical}); err != nil {
		s.reviewPublishError(w, r, sg.ID, err, nil)
		return
	}
	s.reviewDiscardStaged(ctx, sg.ID)
	reviewDone(w, r, sg.ID, store.StatusDuplicate)
}

// reviewHide hides a suggestion of any status as spam. An empty internal
// note keeps the existing one.
func (s *Server) reviewHide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := reviewID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	note := strings.TrimSpace(r.PostFormValue("internal_note"))
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		sg, err := q.GetSuggestion(ctx, id)
		if err != nil {
			return err
		}
		if note == "" {
			note = sg.InternalNote
		}
		at := time.Now().Unix()
		if err := q.HideSuggestion(ctx, id, note, at); err != nil {
			return err
		}
		return reviewLog(ctx, q, sg, sg.EntryID, store.StatusHidden, map[string]any{"previous_status": sg.Status}, at)
	})
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	s.reviewDiscardStaged(ctx, id)
	reviewDone(w, r, id, store.StatusHidden)
}

// --- image preview ----------------------------------------------------------------

// reviewPreview fetches a pending image suggestion's URL through the safe
// fetcher into the private staging dir (owner-triggered only).
func (s *Server) reviewPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sg, ok := s.reviewPending(w, r)
	if !ok {
		return
	}
	if sg.Type != suggest.TypeImage {
		http.NotFound(w, r)
		return
	}
	staged, err := s.admin.images.Stage(ctx, reviewImagePayload(sg).URL)
	if err != nil {
		s.log.InfoContext(ctx, "image fetch refused", "suggestion", sg.ID, "err", err)
		s.reviewFail(w, r, sg.ID, http.StatusUnprocessableEntity, "Image not fetched; the suggestion stays pending: "+err.Error(), nil, nil)
		return
	}
	if old, replaced := s.admin.staged.put(sg.ID, staged); replaced {
		_ = s.admin.images.Discard(old)
	}
	reviewDone(w, r, sg.ID, "staged")
}

// reviewStagedFile serves a staged preview to the owner, from the staging
// dir only (never /images), and only while it is a current preview.
func (s *Server) reviewStagedFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !s.admin.staged.has(name) {
		http.NotFound(w, r)
		return
	}
	f, err := s.admin.images.OpenStaged(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.adminFail(w, r, err)
		return
	}
	ext := name[strings.LastIndexByte(name, '.')+1:]
	ct := map[string]string{"jpg": "image/jpeg", "png": "image/png", "gif": "image/gif", "webp": "image/webp"}[ext]
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
