package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"amigaconstellation/internal/store"
)

const (
	seedJSON   = "../../data/amiga-data.json"
	seedImages = "../../images"
)

func newTestCatalog(t *testing.T) (*Catalog, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "amiga.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return New(st), st
}

func loadSeed(t *testing.T) *Document {
	t.Helper()
	doc, err := LoadDocument(seedJSON)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// mutateEntry returns a copy of doc whose entry i was changed by fn.
func mutateEntry(t *testing.T, doc *Document, i int, fn func(map[string]any)) *Document {
	t.Helper()
	out := *doc
	out.Entries = append([]json.RawMessage(nil), doc.Entries...)
	var m map[string]any
	if err := json.Unmarshal(out.Entries[i], &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	out.Entries[i] = raw
	return &out
}

// decoded turns any JSON value into generic Go values for deep comparison.
func decoded(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCatalog_Export(t *testing.T) {
	t.Parallel()
	cat, _ := newTestCatalog(t)
	ctx := t.Context()
	if _, err := cat.Export(ctx); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Export on empty catalogue: err = %v, want ErrEmpty", err)
	}
	seed := loadSeed(t)
	if _, err := cat.Import(ctx, seed, "test"); err != nil {
		t.Fatal(err)
	}
	got, err := cat.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}

	sections := []struct {
		name      string
		got, want json.RawMessage
	}{
		{"meta", got.Meta, seed.Meta},
		{"domains", got.Domains, seed.Domains},
		{"tours", got.Tours, seed.Tours},
		{"_readme", got.Readme, seed.Readme},
	}
	for _, s := range sections {
		if !reflect.DeepEqual(decoded(t, s.got), decoded(t, s.want)) {
			t.Errorf("%s differs after round-trip", s.name)
		}
	}
	if len(got.Entries) != len(seed.Entries) {
		t.Fatalf("exported %d entries, want %d", len(got.Entries), len(seed.Entries))
	}
	for i := range seed.Entries {
		if !reflect.DeepEqual(decoded(t, got.Entries[i]), decoded(t, seed.Entries[i])) {
			t.Errorf("entry #%d differs after round-trip (order or content)", i)
		}
	}

	// The seed file is written with one-space indentation, so an unchanged
	// export reproduces it byte for byte.
	var buf bytes.Buffer
	if err := got.Encode(&buf, true); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(seedJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Error("indented export is not byte-identical to the seed file")
	}
}

func TestCatalog_Import(t *testing.T) {
	t.Parallel()
	cat, st := newTestCatalog(t)
	ctx := t.Context()
	seed := loadSeed(t)
	stats, err := cat.Import(ctx, seed, "test")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 332 {
		t.Errorf("created = %d, want 332", stats.Created)
	}
	if _, err := cat.Import(ctx, seed, "test"); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("second Import err = %v, want ErrNotEmpty", err)
	}
	if n, _ := st.Q().CountRevisions(ctx); n != 332 {
		t.Errorf("revisions = %d, want 332", n)
	}
}

func TestCatalog_ImportReplace(t *testing.T) {
	t.Parallel()
	cat, st := newTestCatalog(t)
	ctx := t.Context()
	seed := loadSeed(t)
	if _, err := cat.Import(ctx, seed, "test"); err != nil {
		t.Fatal(err)
	}
	_, etagBefore, err := cat.Cache().Get(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Change entry 1's text and drop an entry no other entry relates to.
	ids := map[string]bool{}
	for _, raw := range seed.Entries {
		e, _ := ParseEntry(raw)
		for _, r := range e.Related {
			ids[r] = true
		}
	}
	drop := -1
	for i := len(seed.Entries) - 1; i >= 0; i-- {
		e, _ := ParseEntry(seed.Entries[i])
		if !ids[e.ID] && i != 1 {
			drop = i
			break
		}
	}
	if drop < 0 {
		t.Fatal("no unreferenced entry to drop")
	}
	dropped, _ := ParseEntry(seed.Entries[drop])
	next := mutateEntry(t, seed, 1, func(m map[string]any) { m["why"] = "Changed by the test." })
	next.Entries = append(append([]json.RawMessage(nil), next.Entries[:drop]...), next.Entries[drop+1:]...)
	// Tours may reference the dropped entry; strip such steps.
	next.Tours = stripTourSteps(t, next.Tours, dropped.ID)

	stats, err := cat.ImportReplace(ctx, next, "test")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 1 || stats.Deleted != 1 || stats.Created != 0 || stats.Unchanged != 330 {
		t.Errorf("stats = %+v", stats)
	}
	changed, _ := ParseEntry(seed.Entries[1])
	row, err := st.Q().GetEntry(ctx, changed.ID)
	if err != nil || row.Rev != 2 {
		t.Errorf("changed entry rev = %d, %v; want 2", row.Rev, err)
	}
	gone, err := st.Q().GetEntry(ctx, dropped.ID)
	if err != nil || gone.DeletedAt == nil || gone.Rev != 2 {
		t.Errorf("dropped entry = %+v, %v; want soft-deleted at rev 2", gone, err)
	}
	revs, _ := st.Q().ListRevisions(ctx, dropped.ID)
	if len(revs) != 2 || revs[0].After != nil || revs[0].Before == nil {
		t.Errorf("deletion revision = %+v", revs)
	}

	_, etagAfter, err := cat.Cache().Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if etagAfter == etagBefore {
		t.Error("cache was not invalidated by ImportReplace")
	}
	exp, _ := cat.Export(ctx)
	if len(exp.Entries) != 331 {
		t.Errorf("export has %d entries, want 331", len(exp.Entries))
	}

	// Re-importing the seed restores the dropped entry.
	stats, err = cat.ImportReplace(ctx, seed, "test")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Restored != 1 || stats.Updated != 1 {
		t.Errorf("restore stats = %+v", stats)
	}
	exp, _ = cat.Export(ctx)
	if len(exp.Entries) != 332 || !reflect.DeepEqual(decoded(t, exp.Entries[drop]), decoded(t, seed.Entries[drop])) {
		t.Error("restored entry missing or out of order")
	}
}

func TestCatalog_ImportReplace_InvalidIsAtomic(t *testing.T) {
	t.Parallel()
	cat, st := newTestCatalog(t)
	ctx := t.Context()
	seed := loadSeed(t)
	if _, err := cat.Import(ctx, seed, "test"); err != nil {
		t.Fatal(err)
	}
	bad := mutateEntry(t, seed, 0, func(m map[string]any) { m["category"] = "nope" })
	if _, err := cat.ImportReplace(ctx, bad, "test"); err == nil {
		t.Fatal("invalid document accepted")
	}
	if n, _ := st.Q().CountRevisions(ctx); n != 332 {
		t.Errorf("revisions = %d after rejected import, want 332", n)
	}
}

func stripTourSteps(t *testing.T, tours json.RawMessage, id string) json.RawMessage {
	t.Helper()
	var ts []map[string]any
	if err := json.Unmarshal(tours, &ts); err != nil {
		t.Fatal(err)
	}
	for _, tour := range ts {
		steps, _ := tour["steps"].([]any)
		kept := steps[:0]
		for _, s := range steps {
			if m, _ := s.(map[string]any); m["id"] != id {
				kept = append(kept, s)
			}
		}
		tour["steps"] = kept
	}
	out, err := json.Marshal(ts)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
