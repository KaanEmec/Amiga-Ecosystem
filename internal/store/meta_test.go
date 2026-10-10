package store

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestQueries_Meta(t *testing.T) {
	t.Parallel()
	st := openTest(t)
	ctx := t.Context()
	q := st.Q()
	if err := q.PutMeta(ctx, MetaTours, json.RawMessage(`[1]`)); err != nil {
		t.Fatal(err)
	}
	if err := q.PutMeta(ctx, MetaTours, json.RawMessage(`[2]`)); err != nil {
		t.Fatal(err)
	}
	got, err := q.GetMeta(ctx, MetaTours)
	if err != nil || string(got) != `[2]` {
		t.Errorf("GetMeta = %s, %v", got, err)
	}
	if err := q.DeleteMeta(ctx, MetaTours); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetMeta(ctx, MetaTours); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMeta after delete err = %v, want ErrNotFound", err)
	}
	if err := q.PutMeta(ctx, "bogus", json.RawMessage(`{}`)); err == nil {
		t.Error("catalog_meta accepted an unknown key")
	}
	if err := q.PutMeta(ctx, MetaMeta, json.RawMessage(`{not json`)); err == nil {
		t.Error("catalog_meta accepted invalid JSON")
	}
}
