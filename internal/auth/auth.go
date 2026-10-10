// Package auth implements the single owner account: argon2id password,
// TOTP second factor, hashed single-use recovery codes, server-side
// sessions, login throttling and per-session CSRF tokens.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
)

// Session and throttle policy.
const (
	CookieName      = "amiga_admin"
	CookiePath      = "/admin"
	IdleTimeout     = 12 * time.Hour
	AbsoluteTimeout = 7 * 24 * time.Hour
	ThrottleWindow  = 15 * time.Minute
	MaxIPFailures   = 5
	MaxAllFailures  = 100

	// touchEvery limits last_seen writes to one per minute per session.
	touchEvery = 60
	// totpStepSetting holds the last accepted TOTP time step (replay guard).
	totpStepSetting = "owner_totp_last_step"
	globalKey       = "global"
)

// Errors. Login failures are deliberately indistinguishable to callers.
var (
	ErrInvalidCredentials = errors.New("invalid username, password or code")
	ErrThrottled          = errors.New("too many failed sign-in attempts; try again later")
	ErrNoSession          = errors.New("no valid session")
	ErrOwnerExists        = errors.New("owner account already exists")
	ErrNoOwner            = errors.New("no owner account; run `amiga owner init`")
)

// Service is the owner authentication service.
type Service struct {
	store  *store.Store
	secret []byte
	secure bool // Secure cookies (production)
	now    func() time.Time
	params HashParams

	// verify serialises password hashing: argon2id at 64 MiB per call must
	// not run concurrently on a small VPS.
	verify chan struct{}
}

// Option configures a Service.
type Option func(*Service)

// WithHashParams sets the argon2id cost for new password hashes (tests use
// cheap parameters).
func WithHashParams(p HashParams) Option { return func(s *Service) { s.params = p } }

// New builds the service. now is the clock (time.Now when nil).
func New(st *store.Store, cfg config.Config, now func() time.Time, opts ...Option) *Service {
	if now == nil {
		now = time.Now
	}
	s := &Service{
		store:  st,
		secret: cfg.Secret,
		secure: cfg.Production(),
		now:    now,
		params: DefaultHashParams,
		verify: make(chan struct{}, 1),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// HasOwner reports whether `owner init` has run.
func (s *Service) HasOwner(ctx context.Context) (bool, error) {
	_, err := s.store.Q().GetOwner(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CheckUsername enforces a short printable username.
func CheckUsername(u string) error {
	if u == "" || len(u) > 64 {
		return errors.New("username must be 1-64 characters")
	}
	for _, r := range u {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return errors.New("username must not contain spaces or control characters")
		}
	}
	return nil
}

// InitOwner creates the owner account and returns the TOTP enrollment and
// recovery codes, which are shown once.
func (s *Service) InitOwner(ctx context.Context, username, password string) (Enrollment, error) {
	if err := CheckUsername(username); err != nil {
		return Enrollment{}, err
	}
	if err := CheckPassword(password); err != nil {
		return Enrollment{}, err
	}
	hash, err := hashPassword(password, s.params)
	if err != nil {
		return Enrollment{}, err
	}
	key, err := newTOTPKey(username)
	if err != nil {
		return Enrollment{}, err
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return Enrollment{}, err
	}
	err = s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		if _, err := q.GetOwner(ctx); err == nil {
			return ErrOwnerExists
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := q.InsertOwner(ctx, store.Owner{
			Username: username, PasswordHash: hash, TOTPSecret: key.Secret(),
			RecoveryCodes: hashes, CreatedAt: s.now().Unix(),
		}); err != nil {
			return err
		}
		if err := q.SetSetting(ctx, totpStepSetting, "0"); err != nil {
			return err
		}
		return q.DeleteAllSessions(ctx)
	})
	if err != nil {
		return Enrollment{}, err
	}
	return Enrollment{URI: key.URL(), Secret: key.Secret(), RecoveryCodes: codes}, nil
}

// ResetPassword sets a new password and ends every session.
func (s *Service) ResetPassword(ctx context.Context, password string) error {
	if err := CheckPassword(password); err != nil {
		return err
	}
	hash, err := hashPassword(password, s.params)
	if err != nil {
		return err
	}
	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		if err := q.UpdateOwnerPassword(ctx, hash); err != nil {
			return ownerErr(err)
		}
		return q.DeleteAllSessions(ctx)
	})
}

// RegenerateRecoveryCodes replaces all recovery codes.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context) ([]string, error) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.store.Q().UpdateOwnerRecoveryCodes(ctx, hashes); err != nil {
		return nil, ownerErr(err)
	}
	return codes, nil
}

// ResetTOTP generates a new TOTP secret and ends every session.
func (s *Service) ResetTOTP(ctx context.Context) (Enrollment, error) {
	var en Enrollment
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		o, err := q.GetOwner(ctx)
		if err != nil {
			return ownerErr(err)
		}
		key, err := newTOTPKey(o.Username)
		if err != nil {
			return err
		}
		if err := q.UpdateOwnerTOTP(ctx, key.Secret()); err != nil {
			return err
		}
		if err := q.SetSetting(ctx, totpStepSetting, "0"); err != nil {
			return err
		}
		en = Enrollment{URI: key.URL(), Secret: key.Secret()}
		return q.DeleteAllSessions(ctx)
	})
	return en, err
}

func ownerErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return ErrNoOwner
	}
	return err
}

// LoginResult is a successful sign-in.
type LoginResult struct {
	Token             string // session token for the cookie
	UsedRecoveryCode  bool
	RecoveryCodesLeft int
}

// Login checks the throttle, the password, then the TOTP code or a
// recovery code (accepted in the same field), and opens a new session.
// Any failure returns ErrInvalidCredentials or ErrThrottled.
func (s *Service) Login(ctx context.Context, username, password, code, ip string) (LoginResult, error) {
	select {
	case s.verify <- struct{}{}:
		defer func() { <-s.verify }()
	case <-ctx.Done():
		return LoginResult{}, ctx.Err()
	}

	now := s.now()
	ipKey := s.ipKey(ip)
	if err := s.checkThrottle(ctx, ipKey, now); err != nil {
		return LoginResult{}, err
	}

	q := s.store.Q()
	owner, err := q.GetOwner(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return LoginResult{}, err
	}
	hash := owner.PasswordHash
	if hash == "" {
		hash = dummyHash() // same work whether or not an owner exists
	}
	pwOK, err := verifyPassword(password, hash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("verifying password: %w", err)
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(owner.Username)) == 1
	if owner.PasswordHash == "" || !pwOK || !userOK {
		return LoginResult{}, s.fail(ctx, ipKey, now)
	}

	var res LoginResult
	token, tokenHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	err = s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		o, err := q.GetOwner(ctx) // re-read inside the write transaction
		if err != nil {
			return err
		}
		if err := s.consumeCode(ctx, q, &o, code, now, &res); err != nil {
			return err
		}
		if err := q.DeleteLoginAttempts(ctx, ipKey); err != nil {
			return err
		}
		if err := q.DeleteStaleSessions(ctx, now.Unix(), now.Add(-IdleTimeout).Unix()); err != nil {
			return err
		}
		return q.InsertSession(ctx, store.Session{
			TokenHash: tokenHash, CreatedAt: now.Unix(),
			ExpiresAt: now.Add(AbsoluteTimeout).Unix(), LastSeen: now.Unix(),
		})
	})
	if errors.Is(err, errCodeRejected) {
		return LoginResult{}, s.fail(ctx, ipKey, now)
	}
	if err != nil {
		return LoginResult{}, err
	}
	res.Token = token
	return res, nil
}

var errCodeRejected = errors.New("code rejected")

// consumeCode accepts a fresh TOTP code (never the same or an earlier time
// step twice) or an unused recovery code, which it burns.
func (s *Service) consumeCode(ctx context.Context, q store.Queries, o *store.Owner, code string, now time.Time, res *LoginResult) error {
	code = strings.TrimSpace(code)
	if isDigits(code) {
		step, ok := matchTOTP(o.TOTPSecret, code, now)
		if !ok {
			return errCodeRejected
		}
		lastRaw, _, err := q.GetSetting(ctx, totpStepSetting)
		if err != nil {
			return err
		}
		last, _ := strconv.ParseInt(lastRaw, 10, 64)
		if step <= last {
			return errCodeRejected // replay of a used (or older) code
		}
		return q.SetSetting(ctx, totpStepSetting, strconv.FormatInt(step, 10))
	}
	i := matchRecoveryCode(o.RecoveryCodes, code)
	if i < 0 {
		return errCodeRejected
	}
	left := slices.Delete(slices.Clone(o.RecoveryCodes), i, i+1)
	if err := q.UpdateOwnerRecoveryCodes(ctx, left); err != nil {
		return err
	}
	res.UsedRecoveryCode, res.RecoveryCodesLeft = true, len(left)
	return nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// checkThrottle refuses when the client or everyone failed too often.
func (s *Service) checkThrottle(ctx context.Context, ipKey string, now time.Time) error {
	q := s.store.Q()
	since := now.Add(-ThrottleWindow).Unix()
	n, err := q.CountLoginAttempts(ctx, ipKey, since)
	if err != nil {
		return err
	}
	if n >= MaxIPFailures {
		return ErrThrottled
	}
	if n, err = q.CountLoginAttempts(ctx, globalKey, since); err != nil {
		return err
	}
	if n >= MaxAllFailures {
		return ErrThrottled
	}
	return nil
}

// fail records a failed attempt and returns ErrInvalidCredentials.
func (s *Service) fail(ctx context.Context, ipKey string, now time.Time) error {
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := store.NewQueries(tx)
		if err := q.PruneLoginAttempts(ctx, now.Add(-ThrottleWindow).Unix()); err != nil {
			return err
		}
		if err := q.InsertLoginAttempt(ctx, ipKey, now.Unix()); err != nil {
			return err
		}
		return q.InsertLoginAttempt(ctx, globalKey, now.Unix())
	})
	if err != nil {
		return err
	}
	return ErrInvalidCredentials
}

// UnlockLogins clears every recorded failed login, lifting both the
// per-IP and the global throttle (CLI `owner unlock`).
func (s *Service) UnlockLogins(ctx context.Context) error {
	return s.store.Q().ClearLoginAttempts(ctx)
}

// ipKey hashes the client address with the app secret; raw IPs are never stored.
func (s *Service) ipKey(ip string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("amiga-login-ip\x00" + ip))
	return "ip:" + hex.EncodeToString(m.Sum(nil)[:16])
}

// Session is a validated owner session.
type Session struct {
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Session validates a session token, enforcing the idle and absolute
// timeouts, and records activity.
func (s *Service) Session(ctx context.Context, token string) (Session, error) {
	if token == "" || len(token) > 128 {
		return Session{}, ErrNoSession
	}
	h := hashToken(token)
	q := s.store.Q()
	row, err := q.GetSession(ctx, h)
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	now := s.now().Unix()
	if now >= row.ExpiresAt || now-row.LastSeen > int64(IdleTimeout/time.Second) {
		if err := q.DeleteSession(ctx, h); err != nil {
			return Session{}, err
		}
		return Session{}, ErrNoSession
	}
	if now-row.LastSeen >= touchEvery {
		if err := q.TouchSession(ctx, h, now); err != nil {
			return Session{}, err
		}
	}
	return Session{TokenHash: h, CreatedAt: time.Unix(row.CreatedAt, 0), ExpiresAt: time.Unix(row.ExpiresAt, 0)}, nil
}

// Logout ends the session for token (no error if it does not exist).
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.Q().DeleteSession(ctx, hashToken(token))
}

// CSRFToken is the per-session form token: HMAC(secret, session hash).
func (s *Service) CSRFToken(sess Session) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("amiga-admin-csrf\x00" + sess.TokenHash))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// ValidCSRF compares a submitted form token in constant time.
func (s *Service) ValidCSRF(sess Session, token string) bool {
	want := s.CSRFToken(sess)
	return subtle.ConstantTimeCompare([]byte(want), []byte(token)) == 1
}

// SetCookie writes the session cookie.
func (s *Service) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: CookiePath,
		MaxAge:   int(AbsoluteTimeout / time.Second),
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

// ClearCookie removes the session cookie.
func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: CookiePath, MaxAge: -1,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

// TokenFromRequest returns the session token cookie value, if any.
func TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func newToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generating session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// dummyHash is verified when no owner exists so timing does not reveal it.
// It is a hash of a random value at the default cost, built on first use.
var dummyHash = sync.OnceValue(func() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	h, err := hashPassword(base64.RawStdEncoding.EncodeToString(b), DefaultHashParams)
	if err != nil {
		panic(err)
	}
	return h
})
