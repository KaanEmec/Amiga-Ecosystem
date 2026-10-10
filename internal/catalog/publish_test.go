package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"amigaconstellation/internal/store"
)

// newSeededCatalog returns a catalogue with the seed imported.
func newSeededCatalog(t *testing.T) (*Catalog, *store.Store) {
	t.Helper()
	cat, st := newTestCatalog(t)
	if _, err := cat.Import(t.Context(), loadSeed(t), "test"); err != nil {
		t.Fatal(err)
	}
	return cat, st
}

func newEntryDoc(id string) json.RawMessage {
	return json.RawMessage(`{"id":"` + id + `","name":"Test Machine","year":2025,"category":"hw-classic",` +
		`"kind":"original","status":"shipping","arch":["68k"],"weight":2,"what":"A test & more.","why":"Because.",` +
		`"related":["a500"],"tags":["test"]}`)
}

func exportDoc(t *testing.T, cat *Catalog) *Document {
	t.Helper()
	doc, err := cat.Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func findEntry(t *testing.T, doc *Document, id string) (Entry, bool) {
	t.Helper()
	for _, raw := range doc.Entries {
		e, err := ParseEntry(raw)
		if err != nil {
			t.Fatal(err)
		}
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func revCount(t *testing.T, st *store.Store) int {
	t.Helper()
	n, err := st.Q().CountRevisions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func entryRow(t *testing.T, st *store.Store, id string) store.Entry {
	t.Helper()
	e, err := st.Q().GetEntry(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func etag(t *testing.T, cat *Catalog) string {
	t.Helper()
	_, tag, err := cat.Cache().Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return tag
}

func TestCatalog_Create(t *testing.T) {
	t.Parallel()
	cat, st := newSeededCatalog(t)
	ctx := t.Context()
	before := etag(t, cat)

	sid := (*int64)(nil)
	pub, err := cat.Create(ctx, newEntryDoc("test-machine"), "owner", "new", sid)
	if err != nil {
		t.Fatal(err)
	}
	if pub.ID != "test-machine" || pub.Rev != 1 {
		t.Errorf("pub = %+v", pub)
	}
	if etag(t, cat) == before {
		t.Error("cache not invalidated")
	}
	doc := exportDoc(t, cat)
	last := doc.Entries[len(doc.Entries)-1]
	if !bytes.Contains(last, []byte(`"what":"A test & more."`)) {
		t.Errorf("new entry not last or HTML-escaped: %s", last)
	}
	revs, err := st.Q().ListRevisions(ctx, "test-machine")
	if err != nil || len(revs) != 1 || revs[0].Before != nil || revs[0].Actor != "owner" {
		t.Fatalf("revisions = %+v, %v", revs, err)
	}
	log, err := st.Q().ListModeration(ctx, "test-machine", 10)
	if err != nil || len(log) != 1 || log[0].Action != ActionCreate {
		t.Fatalf("moderation = %+v, %v", log, err)
	}

	if _, err := cat.Create(ctx, newEntryDoc("test-machine"), "owner", "", nil); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate create: err = %v, want ErrExists", err)
	}
}

func TestCatalog_Create_InvalidRollsBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		doc  json.RawMessage
	}{
		{"unknown category", json.RawMessage(`{"id":"x1","name":"X","year":2000,"category":"nope","kind":"original","status":"shipping","arch":["68k"],"weight":1,"what":"w","why":"y"}`)},
		{"dangling related", json.RawMessage(`{"id":"x1","name":"X","year":2000,"category":"hw-classic","kind":"original","status":"shipping","arch":["68k"],"weight":1,"what":"w","why":"y","related":["nope"]}`)},
		{"bad slug", json.RawMessage(`{"id":"X 1","name":"X","year":2000,"category":"hw-classic","kind":"original","status":"shipping","arch":["68k"],"weight":1,"what":"w","why":"y"}`)},
		{"not an object", json.RawMessage(`[1]`)},
		{"not json", json.RawMessage(`{`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cat, st := newSeededCatalog(t)
			_, err := cat.Create(t.Context(), tt.doc, "owner", "", nil)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if n := revCount(t, st); n != 333 {
				t.Errorf("revisions = %d, want 333", n)
			}
		})
	}
}

func TestCatalog_Update(t *testing.T) {
	t.Parallel()
	cat, st := newSeededCatalog(t)
	ctx := t.Context()
	old := entryRow(t, st, "a500")

	next, err := Patch(old.Doc, Field{Key: "name", Value: "Amiga 500 (edited)"}, Field{Key: "newField", Value: 1})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := cat.Update(ctx, "a500", next, old.Rev, "owner", "rename", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pub.Rev != old.Rev+1 {
		t.Errorf("rev = %d, want %d", pub.Rev, old.Rev+1)
	}
	e, _ := findEntry(t, exportDoc(t, cat), "a500")
	if e.Name != "Amiga 500 (edited)" {
		t.Errorf("name = %q", e.Name)
	}
	revs, _ := st.Q().ListRevisions(ctx, "a500")
	if len(revs) != 2 || !bytes.Equal(revs[0].Before, old.Doc) || !bytes.Equal(revs[0].After, next) {
		t.Errorf("latest revision before/after not recorded: %+v", revs[0])
	}
	log, _ := st.Q().ListModeration(ctx, "a500", 1)
	if len(log) != 1 || !bytes.Contains(log[0].Detail, []byte(`"fields":["name","newField"]`)) {
		t.Errorf("moderation detail = %s", log[0].Detail)
	}

	t.Run("stale rev", func(t *testing.T) {
		_, err := cat.Update(ctx, "a500", old.Doc, old.Rev, "owner", "", nil)
		var se *StaleError
		if !errors.Is(err, ErrStale) || !errors.As(err, &se) || se.Current != pub.Rev {
			t.Errorf("err = %v, want StaleError at rev %d", err, pub.Rev)
		}
	})
	t.Run("unchanged", func(t *testing.T) {
		p, err := cat.Update(ctx, "a500", next, pub.Rev, "owner", "", nil)
		if err != nil || !p.Unchanged || p.Rev != pub.Rev {
			t.Errorf("pub = %+v, err = %v", p, err)
		}
	})
	t.Run("invalid rolls back", func(t *testing.T) {
		bad, _ := Patch(next, Field{Key: "weight", Value: 9})
		_, err := cat.Update(ctx, "a500", bad, pub.Rev, "owner", "", nil)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("err = %v", err)
		}
		if got := entryRow(t, st, "a500").Rev; got != pub.Rev {
			t.Errorf("rev = %d after rejected update, want %d", got, pub.Rev)
		}
	})
	t.Run("id change", func(t *testing.T) {
		renamed, _ := Patch(next, Field{Key: "id", Value: "a500x"})
		var ve *ValidationError
		if _, err := cat.Update(ctx, "a500", renamed, pub.Rev, "owner", "", nil); !errors.As(err, &ve) {
			t.Errorf("err = %v, want *ValidationError", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		if _, err := cat.Update(ctx, "nope", newEntryDoc("nope"), 1, "owner", "", nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestCatalog_Update_HookRollsBack(t *testing.T) {
	t.Parallel()
	cat, st := newSeededCatalog(t)
	old := entryRow(t, st, "a500")
	next, _ := Patch(old.Doc, Field{Key: "name", Value: "Hooked"})
	boom := errors.New("boom")
	called := false
	hook := func(ctx context.Context, tx *sql.Tx) error {
		called = true
		return boom
	}
	if _, err := cat.Update(t.Context(), "a500", next, old.Rev, "owner", "", nil, hook); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want hook error", err)
	}
	if !called {
		t.Fatal("hook not called")
	}
	if got := entryRow(t, st, "a500"); got.Rev != old.Rev || !bytes.Equal(got.Doc, old.Doc) {
		t.Error("hook failure did not roll back the update")
	}
}

func TestCatalog_Dependencies_And_Remove(t *testing.T) {
	t.Parallel()
	cat, st := newSeededCatalog(t)
	ctx := t.Context()

	deps, err := cat.Dependencies(ctx, "a500")
	if err != nil {
		t.Fatal(err)
	}
	if !deps.Featured || len(deps.RelatedBy) == 0 || len(deps.TourStops) == 0 {
		t.Fatalf("a500 dependencies = %+v, want featured with related and tour stops", deps)
	}
	revsBefore := revCount(t, st)
	firstDep := entryRow(t, st, deps.RelatedBy[0].ID)
	rev := entryRow(t, st, "a500").Rev

	if _, err := cat.Remove(ctx, "a500", rev-1, "owner", "", nil); !errors.Is(err, ErrStale) {
		t.Fatalf("stale remove: err = %v", err)
	}

	pub, err := cat.Remove(ctx, "a500", rev, "owner", "obsolete", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pub.Affected) != len(deps.RelatedBy) || len(pub.ToursChanged) == 0 {
		t.Errorf("pub = %+v, deps = %d related", pub, len(deps.RelatedBy))
	}
	if got, want := revCount(t, st), revsBefore+1+len(deps.RelatedBy); got != want {
		t.Errorf("revisions = %d, want %d", got, want)
	}

	doc := exportDoc(t, cat)
	if _, ok := findEntry(t, doc, "a500"); ok {
		t.Error("removed entry still exported")
	}
	for _, raw := range doc.Entries {
		e, _ := ParseEntry(raw)
		if slices.Contains(e.Related, "a500") {
			t.Errorf("entry %q still relates to a500", e.ID)
		}
	}
	if bytes.Contains(doc.Tours, []byte(`"id":"a500"`)) {
		t.Error("tours still reference a500")
	}
	if _, err := Validate(doc); err != nil {
		t.Errorf("exported catalogue invalid after remove: %v", err)
	}
	if got := entryRow(t, st, firstDep.ID); got.Rev != firstDep.Rev+1 {
		t.Errorf("dependent rev = %d, want %d", got.Rev, firstDep.Rev+1)
	}
	if _, err := cat.Dependencies(ctx, "a500"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Dependencies of removed: err = %v", err)
	}
	if _, err := cat.Remove(ctx, "a500", pub.Rev, "owner", "", nil); !errors.Is(err, ErrDeleted) {
		t.Errorf("second remove: err = %v, want ErrDeleted", err)
	}
	log, _ := st.Q().ListModeration(ctx, "a500", 10)
	actions := []string{}
	for _, ev := range log {
		actions = append(actions, ev.Action)
	}
	if !slices.Contains(actions, ActionRemove) || !slices.Contains(actions, ActionTourCleanup) {
		t.Errorf("moderation actions = %v", actions)
	}

	// Restore brings the entry back (a500 relates to nothing removed).
	rp, err := cat.Restore(ctx, "a500", "owner", "undo")
	if err != nil {
		t.Fatal(err)
	}
	if rp.Rev != pub.Rev+1 {
		t.Errorf("restore rev = %d", rp.Rev)
	}
	if _, ok := findEntry(t, exportDoc(t, cat), "a500"); !ok {
		t.Error("restored entry not exported")
	}
	revs, _ := st.Q().ListRevisions(ctx, "a500")
	if revs[0].Before != nil || revs[0].After == nil {
		t.Errorf("restore revision = %+v", revs[0])
	}
	if _, err := cat.Restore(ctx, "a500", "owner", ""); !errors.Is(err, ErrNotDeleted) {
		t.Errorf("restore live: err = %v", err)
	}
}

func TestCatalog_Restore_Dangling(t *testing.T) {
	t.Parallel()
	cat, st := newSeededCatalog(t)
	ctx := t.Context()

	// x relates to y; remove x, then y; restoring x would dangle.
	var x Entry
	for _, raw := range exportDoc(t, cat).Entries {
		e, _ := ParseEntry(raw)
		if len(e.Related) > 0 {
			x = e
			break
		}
	}
	y := x.Related[0]
	if _, err := cat.Remove(ctx, x.ID, entryRow(t, st, x.ID).Rev, "owner", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Remove(ctx, y, entryRow(t, st, y).Rev, "owner", "", nil); err != nil {
		t.Fatal(err)
	}
	revs := revCount(t, st)
	_, err := cat.Restore(ctx, x.ID, "owner", "")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("restore with dangling related: err = %v, want *ValidationError", err)
	}
	if revCount(t, st) != revs || entryRow(t, st, x.ID).DeletedAt == nil {
		t.Fatal("failed restore changed the database")
	}

	fixed, err := dropRelatedForTest(entryRow(t, st, x.ID).Doc, y)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.RestoreAs(ctx, x.ID, fixed, "owner", "resolved"); err != nil {
		t.Fatalf("RestoreAs: %v", err)
	}
	if _, ok := findEntry(t, exportDoc(t, cat), x.ID); !ok {
		t.Error("entry not restored")
	}
}

func dropRelatedForTest(doc json.RawMessage, id string) (json.RawMessage, error) {
	out, _, err := dropRelated(doc, id)
	return out, err
}

func TestPatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		in     string
		fields []Field
		want   string
	}{
		{"keeps order and bytes", `{"b":1, "a":"x\u2014y","c":[1]}`, []Field{{Key: "b", Value: 2}}, `{"b":2,"a":"x\u2014y","c":[1]}`},
		{"appends new key", `{"a":1}`, []Field{{Key: "b", Value: []string{"<x>"}}}, `{"a":1,"b":["<x>"]}`},
		{"removes key", `{"a":1,"b":2}`, []Field{{Key: "a", Remove: true}}, `{"b":2}`},
		{"empty base", ``, []Field{{Key: "id", Value: "x"}}, `{"id":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Patch(json.RawMessage(tt.in), tt.fields...)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("Patch = %s, want %s", got, tt.want)
			}
		})
	}
	if _, err := Patch(json.RawMessage(`[1]`)); err == nil {
		t.Error("Patch accepted an array")
	}
}
