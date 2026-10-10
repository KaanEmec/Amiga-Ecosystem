package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Owner is the single owner account (owner.id = 1).
type Owner struct {
	Username      string
	PasswordHash  string   // argon2id PHC string
	TOTPSecret    string   // base32
	RecoveryCodes []string // sha256 hex of each unused code
	CreatedAt     int64
}

// Session is one owner session; only the token hash is stored.
type Session struct {
	TokenHash string
	CreatedAt int64
	ExpiresAt int64 // absolute expiry
	LastSeen  int64
}

// GetOwner returns the owner account, or ErrNotFound before `owner init`.
func (q Queries) GetOwner(ctx context.Context) (Owner, error) {
	var o Owner
	var codes string
	err := q.db.QueryRowContext(ctx,
		"SELECT username, password_hash, totp_secret, recovery_codes, created_at FROM owner WHERE id = 1").
		Scan(&o.Username, &o.PasswordHash, &o.TOTPSecret, &codes, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Owner{}, fmt.Errorf("owner: %w", ErrNotFound)
	}
	if err != nil {
		return Owner{}, fmt.Errorf("getting owner: %w", err)
	}
	if err := json.Unmarshal([]byte(codes), &o.RecoveryCodes); err != nil {
		return Owner{}, fmt.Errorf("decoding recovery codes: %w", err)
	}
	return o, nil
}

// InsertOwner creates the owner row. It fails if one already exists.
func (q Queries) InsertOwner(ctx context.Context, o Owner) error {
	codes, err := encodeCodes(o.RecoveryCodes)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx,
		`INSERT INTO owner (id, username, password_hash, totp_secret, recovery_codes, created_at)
		 VALUES (1, ?, ?, ?, ?, ?)`,
		o.Username, o.PasswordHash, o.TOTPSecret, codes, o.CreatedAt)
	if err != nil {
		return fmt.Errorf("inserting owner: %w", err)
	}
	return nil
}

// UpdateOwnerPassword replaces the password hash.
func (q Queries) UpdateOwnerPassword(ctx context.Context, hash string) error {
	return q.updateOwner(ctx, "password_hash", hash)
}

// UpdateOwnerTOTP replaces the TOTP secret.
func (q Queries) UpdateOwnerTOTP(ctx context.Context, secret string) error {
	return q.updateOwner(ctx, "totp_secret", secret)
}

// UpdateOwnerRecoveryCodes replaces the stored recovery code hashes.
func (q Queries) UpdateOwnerRecoveryCodes(ctx context.Context, hashes []string) error {
	codes, err := encodeCodes(hashes)
	if err != nil {
		return err
	}
	return q.updateOwner(ctx, "recovery_codes", codes)
}

// updateOwner sets one column; col is always a constant from this file.
func (q Queries) updateOwner(ctx context.Context, col, value string) error {
	res, err := q.db.ExecContext(ctx, "UPDATE owner SET "+col+" = ? WHERE id = 1", value)
	if err != nil {
		return fmt.Errorf("updating owner %s: %w", col, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("updating owner %s: %w", col, err)
	}
	if n == 0 {
		return fmt.Errorf("owner: %w", ErrNotFound)
	}
	return nil
}

func encodeCodes(hashes []string) (string, error) {
	if hashes == nil {
		hashes = []string{}
	}
	b, err := json.Marshal(hashes)
	if err != nil {
		return "", fmt.Errorf("encoding recovery codes: %w", err)
	}
	return string(b), nil
}

// InsertSession stores a new session.
func (q Queries) InsertSession(ctx context.Context, s Session) error {
	_, err := q.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, created_at, expires_at, last_seen) VALUES (?, ?, ?, ?)",
		s.TokenHash, s.CreatedAt, s.ExpiresAt, s.LastSeen)
	if err != nil {
		return fmt.Errorf("inserting session: %w", err)
	}
	return nil
}

// GetSession returns a session by token hash, or ErrNotFound.
func (q Queries) GetSession(ctx context.Context, tokenHash string) (Session, error) {
	var s Session
	err := q.db.QueryRowContext(ctx,
		"SELECT token_hash, created_at, expires_at, last_seen FROM sessions WHERE token_hash = ?", tokenHash).
		Scan(&s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("session: %w", ErrNotFound)
	}
	if err != nil {
		return Session{}, fmt.Errorf("getting session: %w", err)
	}
	return s, nil
}

// TouchSession records activity on a session.
func (q Queries) TouchSession(ctx context.Context, tokenHash string, at int64) error {
	if _, err := q.db.ExecContext(ctx, "UPDATE sessions SET last_seen = ? WHERE token_hash = ?", at, tokenHash); err != nil {
		return fmt.Errorf("touching session: %w", err)
	}
	return nil
}

// DeleteSession removes one session if present.
func (q Queries) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

// DeleteAllSessions signs the owner out everywhere.
func (q Queries) DeleteAllSessions(ctx context.Context) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return fmt.Errorf("deleting sessions: %w", err)
	}
	return nil
}

// DeleteStaleSessions removes sessions past their absolute expiry or idle
// since before idleBefore.
func (q Queries) DeleteStaleSessions(ctx context.Context, now, idleBefore int64) error {
	if _, err := q.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE expires_at <= ? OR last_seen < ?", now, idleBefore); err != nil {
		return fmt.Errorf("pruning sessions: %w", err)
	}
	return nil
}

// InsertLoginAttempt records one failed login under key.
func (q Queries) InsertLoginAttempt(ctx context.Context, key string, at int64) error {
	if _, err := q.db.ExecContext(ctx, "INSERT INTO login_attempts (key, at) VALUES (?, ?)", key, at); err != nil {
		return fmt.Errorf("recording login attempt: %w", err)
	}
	return nil
}

// CountLoginAttempts counts failures under key at or after since.
func (q Queries) CountLoginAttempts(ctx context.Context, key string, since int64) (int, error) {
	var n int
	if err := q.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM login_attempts WHERE key = ? AND at >= ?", key, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting login attempts: %w", err)
	}
	return n, nil
}

// DeleteLoginAttempts clears failures under key.
func (q Queries) DeleteLoginAttempts(ctx context.Context, key string) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE key = ?", key); err != nil {
		return fmt.Errorf("clearing login attempts: %w", err)
	}
	return nil
}

// ClearLoginAttempts drops every recorded failure.
func (q Queries) ClearLoginAttempts(ctx context.Context) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM login_attempts"); err != nil {
		return fmt.Errorf("clearing login attempts: %w", err)
	}
	return nil
}

// PruneLoginAttempts drops failures older than before.
func (q Queries) PruneLoginAttempts(ctx context.Context, before int64) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE at < ?", before); err != nil {
		return fmt.Errorf("pruning login attempts: %w", err)
	}
	return nil
}
