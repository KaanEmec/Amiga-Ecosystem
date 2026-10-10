package store

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestQueries_Entries(t *testing.T) {
	t.Parallel()
	st := openTest(t)
	ctx := t.Context()
	q := st.Q()

	e := Entry{ID: "a500", Doc: json.RawMessage(`{"id":"a500"}`), Rev: 1, Ord: 0, CreatedAt: 1, UpdatedAt: 1}
	if err := q.InsertEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if _, err := q.InsertRevision(ctx, Revision{EntryID: "a500", Rev: 1, After: e.Doc, Actor: "test", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.InsertRevision(ctx, Revision{EntryID: "missing", Rev: 1, Actor: "test", CreatedAt: 1}); err == nil {
		t.Error("revision for a missing entry was accepted; foreign keys not enforced")
	}

	e.Doc, e.Rev = json.RawMessage(`{"id":"a500","name":"x"}`), 2
	if err := q.UpdateEntry(ctx, e, 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEntry with stale rev: err = %v, want ErrNotFound", err)
	}
	if err := q.UpdateEntry(ctx, e, 1); err != nil {
		t.Fatal(err)
	}
	got, err := q.GetEntry(ctx, "a500")
	if err != nil {
		t.Fatal(err)
	}
	if got.Rev != 2 || string(got.Doc) != `{"id":"a500","name":"x"}` || got.DeletedAt != nil {
		t.Errorf("GetEntry = %+v", got)
	}
	if _, err := q.GetEntry(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEntry(nope) err = %v, want ErrNotFound", err)
	}
	revs, err := q.ListRevisions(ctx, "a500")
	if err != nil || len(revs) != 1 || revs[0].Before != nil || string(revs[0].After) != `{"id":"a500"}` {
		t.Errorf("ListRevisions = %+v, %v", revs, err)
	}
}
