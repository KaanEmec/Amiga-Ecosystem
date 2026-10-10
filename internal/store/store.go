// Package store owns the SQLite database: connection setup, migrations and
// typed queries. It is the only package that writes SQL.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// ErrNotFound is returned by single-row lookups that match nothing.
var ErrNotFound = errors.New("not found")

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// DBTX is satisfied by *sql.DB and *sql.Tx, so queries run inside or
// outside a transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Open opens (creating if needed) the SQLite database at path with WAL
// journaling, a busy timeout and foreign keys enforced on every connection.
// Transactions begin IMMEDIATE so writers queue instead of failing on lock
// upgrades. Call Migrate before use.
func Open(path string) (*Store, error) {
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)",
		"busy_timeout(5000)",
		"foreign_keys(1)",
		"synchronous(NORMAL)",
	} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite %s: %w", path, err)
	}
	// One writer at a time is enforced by SQLite; a few connections let
	// WAL readers proceed while a write is in flight.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connecting to sqlite %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for queries that need no transaction.
func (s *Store) DB() *sql.DB { return s.db }

// Q returns the typed queries bound to the database (no transaction).
func (s *Store) Q() Queries { return Queries{db: s.db} }

// Ping runs SELECT 1, for health checks.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// Tx runs fn in a transaction, committing if fn returns nil and rolling
// back otherwise (including on panic, which is re-raised).
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// BackupTo writes a consistent copy of the database to path using
// VACUUM INTO. path must not exist.
func (s *Store) BackupTo(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("vacuum into %s: %w", path, err)
	}
	return nil
}

// Queries groups the typed query helpers. Build one with Store.Q or
// NewQueries(tx) inside Store.Tx.
type Queries struct {
	db DBTX
}

// NewQueries binds the query helpers to a *sql.DB or *sql.Tx.
func NewQueries(db DBTX) Queries { return Queries{db: db} }
