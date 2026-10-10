package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Catalogue meta keys stored in catalog_meta.
const (
	MetaMeta    = "meta"
	MetaDomains = "domains"
	MetaTours   = "tours"
	MetaReadme  = "_readme"
)

// Setting keys stored in settings.
const (
	SettingSubmissionsPaused = "submissions_paused"
	SettingVotingPaused      = "voting_paused"
)

// ListMeta returns every catalog_meta row keyed by key.
func (q Queries) ListMeta(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := q.db.QueryContext(ctx, "SELECT key, doc FROM catalog_meta")
	if err != nil {
		return nil, fmt.Errorf("listing catalog meta: %w", err)
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var key, doc string
		if err := rows.Scan(&key, &doc); err != nil {
			return nil, fmt.Errorf("scanning catalog meta: %w", err)
		}
		out[key] = json.RawMessage(doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating catalog meta: %w", err)
	}
	return out, nil
}

// GetMeta returns one catalog_meta document, or ErrNotFound.
func (q Queries) GetMeta(ctx context.Context, key string) (json.RawMessage, error) {
	var doc string
	err := q.db.QueryRowContext(ctx, "SELECT doc FROM catalog_meta WHERE key = ?", key).Scan(&doc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("catalog meta %q: %w", key, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting catalog meta %q: %w", key, err)
	}
	return json.RawMessage(doc), nil
}

// PutMeta inserts or replaces one catalog_meta document.
func (q Queries) PutMeta(ctx context.Context, key string, doc json.RawMessage) error {
	_, err := q.db.ExecContext(ctx,
		"INSERT INTO catalog_meta (key, doc) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET doc = excluded.doc",
		key, string(doc))
	if err != nil {
		return fmt.Errorf("putting catalog meta %q: %w", key, err)
	}
	return nil
}

// DeleteMeta removes one catalog_meta document if present.
func (q Queries) DeleteMeta(ctx context.Context, key string) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM catalog_meta WHERE key = ?", key); err != nil {
		return fmt.Errorf("deleting catalog meta %q: %w", key, err)
	}
	return nil
}

// GetSetting returns a setting value and whether it exists.
func (q Queries) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := q.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("getting setting %q: %w", key, err)
	}
	return v, true, nil
}

// SetSetting inserts or replaces a setting.
func (q Queries) SetSetting(ctx context.Context, key, value string) error {
	_, err := q.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	if err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	return nil
}

// SettingBool reads a boolean setting ("1"/"true"); missing means false.
func (q Queries) SettingBool(ctx context.Context, key string) (bool, error) {
	v, ok, err := q.GetSetting(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	return v == "1" || v == "true", nil
}
