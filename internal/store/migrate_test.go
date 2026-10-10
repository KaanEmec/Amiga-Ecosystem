package store

import "testing"

func TestStore_Migrate(t *testing.T) {
	t.Parallel()
	st := openTest(t) // applies once
	ctx := t.Context()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := migs[len(migs)-1].version; v != want {
		t.Errorf("schema version = %d, want %d", v, want)
	}
	for _, table := range []string{
		"entries", "entry_revisions", "catalog_meta", "suggestions", "votes", "moderation_log",
		"images", "owner", "sessions", "login_attempts", "rate_events", "settings", "pageviews",
	} {
		var n int
		if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?", table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("table %s missing", table)
		}
	}
	for _, index := range []string{"suggestions_status_created", "votes_voter", "rate_events_key_at", "login_attempts_key_at", "pageviews_day", "pageviews_visitor_at"} {
		var n int
		if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type = 'index' AND name = ?", index).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("index %s missing", index)
		}
	}
}
