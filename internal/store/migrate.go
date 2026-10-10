package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies pending migrations in version order. Each migration runs
// in its own transaction together with the PRAGMA user_version bump, so a
// failed migration leaves the database at the previous version. Safe to
// call on every start.
func (s *Store) Migrate(ctx context.Context) error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migs {
		err := s.Tx(ctx, func(tx *sql.Tx) error {
			var current int
			if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
				return fmt.Errorf("reading schema version: %w", err)
			}
			if m.version <= current {
				return nil
			}
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return fmt.Errorf("applying migration %s: %w", m.name, err)
			}
			// PRAGMA does not accept bound parameters; version is an int.
			if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(m.version)); err != nil {
				return fmt.Errorf("recording migration %s: %w", m.name, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the applied migration version.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("reading schema version: %w", err)
	}
	return v, nil
}

// loadMigrations reads migrations/NNNN_name.sql in version order.
func loadMigrations() ([]migration, error) {
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}
	migs := make([]migration, 0, len(files))
	seen := map[int]string{}
	for _, f := range files {
		name := strings.TrimPrefix(f, "migrations/")
		num, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("migration %s: name must start with a positive version like 0001_", name)
		}
		if prev, dup := seen[v]; dup {
			return nil, fmt.Errorf("migrations %s and %s share version %d", prev, name, v)
		}
		seen[v] = name
		body, err := migrationFS.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", name, err)
		}
		migs = append(migs, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}
