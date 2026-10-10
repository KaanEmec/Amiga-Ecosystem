package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "amiga.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestOpen_Pragmas(t *testing.T) {
	t.Parallel()
	st := openTest(t)
	ctx := t.Context()
	var mode string
	var fk, timeout int
	if err := st.DB().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" || fk != 1 || timeout != 5000 {
		t.Errorf("journal_mode=%q foreign_keys=%d busy_timeout=%d", mode, fk, timeout)
	}
}

func TestStore_Tx(t *testing.T) {
	t.Parallel()
	st := openTest(t)
	ctx := t.Context()
	boom := errors.New("boom")

	err := st.Tx(ctx, func(tx *sql.Tx) error {
		if err := NewQueries(tx).SetSetting(ctx, SettingVotingPaused, "1"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Tx error = %v, want boom", err)
	}
	if _, ok, err := st.Q().GetSetting(ctx, SettingVotingPaused); err != nil || ok {
		t.Fatalf("setting after rollback: ok=%v err=%v", ok, err)
	}

	if err := st.Tx(ctx, func(tx *sql.Tx) error {
		return NewQueries(tx).SetSetting(ctx, SettingVotingPaused, "1")
	}); err != nil {
		t.Fatal(err)
	}
	paused, err := st.Q().SettingBool(ctx, SettingVotingPaused)
	if err != nil || !paused {
		t.Fatalf("SettingBool = %v, %v; want true", paused, err)
	}
}

func TestStore_BackupTo(t *testing.T) {
	t.Parallel()
	st := openTest(t)
	ctx := context.WithoutCancel(t.Context())
	if err := st.Q().SetSetting(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "copy.db")
	if err := st.BackupTo(ctx, dest); err != nil {
		t.Fatal(err)
	}
	cp, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	v, ok, err := cp.Q().GetSetting(ctx, "k")
	if err != nil || !ok || v != "v" {
		t.Errorf("backup setting = %q %v %v", v, ok, err)
	}
}
