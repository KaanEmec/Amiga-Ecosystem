package auth

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
)

const testPassword = "correct horse battery"

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// cheap argon2 parameters keep tests fast under -race.
var cheap = HashParams{MemoryKiB: 64, Time: 1, Threads: 1}

func newTestService(t *testing.T) (*Service, *fakeClock, Enrollment) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "amiga.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Date(2026, 10, 10, 12, 0, 5, 0, time.UTC)}
	cfg := config.Config{Secret: []byte(strings.Repeat("k", 32)), Env: config.EnvDevelopment}
	svc := New(st, cfg, clock.Now, WithHashParams(cheap))
	en, err := svc.InitOwner(t.Context(), "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return svc, clock, en
}

func code(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := totp.GenerateCodeCustom(secret, at, totpOpts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHashPassword_RoundTrip(t *testing.T) {
	t.Parallel()
	h, err := hashPassword("pw-123456789012", cheap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("hash = %q", h)
	}
	if ok, err := verifyPassword("pw-123456789012", h); !ok || err != nil {
		t.Errorf("verify correct = %v, %v", ok, err)
	}
	if ok, _ := verifyPassword("pw-123456789013", h); ok {
		t.Error("verify wrong password succeeded")
	}
	if _, err := verifyPassword("x", "$argon2i$v=19$m=64,t=1,p=1$AA$AA"); err == nil {
		t.Error("accepted argon2i hash")
	}
	if _, err := verifyPassword("x", "$argon2id$v=19$m=999999999,t=1,p=1$AA$AA"); err == nil {
		t.Error("accepted absurd memory parameter")
	}
}

func TestCheckPassword(t *testing.T) {
	t.Parallel()
	if CheckPassword("short") == nil {
		t.Error("short password accepted")
	}
	if CheckPassword(strings.Repeat("x", 300)) == nil {
		t.Error("overlong password accepted")
	}
	if err := CheckPassword(testPassword); err != nil {
		t.Error(err)
	}
}

func TestService_InitOwner(t *testing.T) {
	t.Parallel()
	svc, _, en := newTestService(t)
	if !strings.HasPrefix(en.URI, "otpauth://totp/") || en.Secret == "" || len(en.RecoveryCodes) != 10 {
		t.Errorf("enrollment = %+v", en)
	}
	if _, err := svc.InitOwner(t.Context(), "other", testPassword); !errors.Is(err, ErrOwnerExists) {
		t.Errorf("second init: err = %v", err)
	}
	if ok, err := svc.HasOwner(t.Context()); !ok || err != nil {
		t.Errorf("HasOwner = %v, %v", ok, err)
	}
}

func TestService_Login(t *testing.T) {
	t.Parallel()
	svc, clock, en := newTestService(t)
	ctx := t.Context()

	tests := []struct {
		name     string
		user, pw string
		code     string
	}{
		{"wrong password", "owner", testPassword + "x", code(t, en.Secret, clock.Now())},
		{"wrong username", "Owner", testPassword, code(t, en.Secret, clock.Now())},
		{"wrong code", "owner", testPassword, "000000"},
		{"code too old", "owner", testPassword, code(t, en.Secret, clock.Now().Add(-2*time.Minute))},
		{"empty code", "owner", testPassword, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Login(ctx, tt.user, tt.pw, tt.code, "10.0.0."+tt.name[:1]); !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("err = %v, want ErrInvalidCredentials", err)
			}
		})
	}

	c := code(t, en.Secret, clock.Now())
	res, err := svc.Login(ctx, "owner", testPassword, c, "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Token == "" || res.UsedRecoveryCode {
		t.Errorf("result = %+v", res)
	}
	if _, err := svc.Session(ctx, res.Token); err != nil {
		t.Errorf("new session invalid: %v", err)
	}

	t.Run("totp replay rejected", func(t *testing.T) {
		if _, err := svc.Login(ctx, "owner", testPassword, c, "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("replayed code: err = %v", err)
		}
		// The previous step's code is older than the used one: also rejected.
		if _, err := svc.Login(ctx, "owner", testPassword, code(t, en.Secret, clock.Now().Add(-30*time.Second)), "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("older code: err = %v", err)
		}
		clock.Advance(30 * time.Second)
		if _, err := svc.Login(ctx, "owner", testPassword, code(t, en.Secret, clock.Now()), "192.0.2.1"); err != nil {
			t.Errorf("next step code: err = %v", err)
		}
	})
}

func TestService_Login_RecoveryCodeSingleUse(t *testing.T) {
	t.Parallel()
	svc, _, en := newTestService(t)
	ctx := t.Context()
	rc := strings.ToUpper(en.RecoveryCodes[3]) // case and hyphen are normalised
	res, err := svc.Login(ctx, "owner", testPassword, rc, "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.UsedRecoveryCode || res.RecoveryCodesLeft != 9 {
		t.Errorf("result = %+v", res)
	}
	if _, err := svc.Login(ctx, "owner", testPassword, rc, "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("reused recovery code: err = %v", err)
	}

	codes, err := svc.RegenerateRecoveryCodes(ctx)
	if err != nil || len(codes) != 10 {
		t.Fatalf("regenerate = %d, %v", len(codes), err)
	}
	if _, err := svc.Login(ctx, "owner", testPassword, en.RecoveryCodes[4], "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old recovery code after regenerate: err = %v", err)
	}
	if _, err := svc.Login(ctx, "owner", testPassword, codes[0], "192.0.2.1"); err != nil {
		t.Errorf("new recovery code: %v", err)
	}
}

func TestService_Login_Throttle(t *testing.T) {
	t.Parallel()
	svc, clock, en := newTestService(t)
	ctx := t.Context()

	for i := range MaxIPFailures {
		if _, err := svc.Login(ctx, "owner", "wrong password!", "123456", "198.51.100.7"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: err = %v", i, err)
		}
	}
	good := func(ip string) error {
		_, err := svc.Login(ctx, "owner", testPassword, code(t, en.Secret, clock.Now()), ip)
		return err
	}
	if err := good("198.51.100.7"); !errors.Is(err, ErrThrottled) {
		t.Errorf("after %d failures: err = %v, want ErrThrottled", MaxIPFailures, err)
	}
	if err := good("198.51.100.8"); err != nil {
		t.Errorf("other IP: err = %v", err)
	}
	clock.Advance(ThrottleWindow + time.Second)
	if err := good("198.51.100.7"); err != nil {
		t.Errorf("after window: err = %v", err)
	}

	// Global cap: failures spread across many addresses.
	for i := range MaxAllFailures {
		_, _ = svc.Login(ctx, "owner", "wrong password!", "123456", fmt.Sprintf("203.0.%d.%d", i/200, i%200))
	}
	if err := good("192.0.2.200"); !errors.Is(err, ErrThrottled) {
		t.Errorf("after %d global failures: err = %v, want ErrThrottled", MaxAllFailures, err)
	}

	// `owner unlock` clears every failure, global and per IP.
	if err := svc.UnlockLogins(ctx); err != nil {
		t.Fatal(err)
	}
	clock.Advance(30 * time.Second) // fresh TOTP step
	if err := good("192.0.2.200"); err != nil {
		t.Errorf("after unlock: err = %v", err)
	}
}

func TestService_Session_Expiry(t *testing.T) {
	t.Parallel()
	svc, clock, en := newTestService(t)
	ctx := t.Context()
	login := func() string {
		clock.Advance(30 * time.Second) // fresh TOTP step each time
		res, err := svc.Login(ctx, "owner", testPassword, code(t, en.Secret, clock.Now()), "192.0.2.1")
		if err != nil {
			t.Fatal(err)
		}
		return res.Token
	}

	idle := login()
	clock.Advance(IdleTimeout + time.Minute)
	if _, err := svc.Session(ctx, idle); !errors.Is(err, ErrNoSession) {
		t.Errorf("idle session: err = %v, want ErrNoSession", err)
	}

	active := login()
	for range 16 { // 16 * 11h > 7d, but never idle for 12h
		clock.Advance(11 * time.Hour)
		if _, err := svc.Session(ctx, active); err != nil {
			if !errors.Is(err, ErrNoSession) {
				t.Fatal(err)
			}
			break
		}
	}
	if _, err := svc.Session(ctx, active); !errors.Is(err, ErrNoSession) {
		t.Errorf("session past absolute timeout still valid")
	}

	tok := login()
	if err := svc.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Session(ctx, tok); !errors.Is(err, ErrNoSession) {
		t.Errorf("session after logout: err = %v", err)
	}
	if _, err := svc.Session(ctx, "garbage"); !errors.Is(err, ErrNoSession) {
		t.Errorf("garbage token: err = %v", err)
	}
}

func TestService_CSRF(t *testing.T) {
	t.Parallel()
	svc, clock, en := newTestService(t)
	ctx := t.Context()
	res, err := svc.Login(ctx, "owner", testPassword, code(t, en.Secret, clock.Now()), "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.Session(ctx, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	tok := svc.CSRFToken(sess)
	if !svc.ValidCSRF(sess, tok) {
		t.Error("own CSRF token rejected")
	}
	if svc.ValidCSRF(Session{TokenHash: "other"}, tok) || svc.ValidCSRF(sess, "") || svc.ValidCSRF(sess, tok+"x") {
		t.Error("foreign or tampered CSRF token accepted")
	}
}

func TestService_Resets_EndSessions(t *testing.T) {
	t.Parallel()
	svc, clock, en := newTestService(t)
	ctx := t.Context()
	res, err := svc.Login(ctx, "owner", testPassword, code(t, en.Secret, clock.Now()), "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(ctx, "a different passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Session(ctx, res.Token); !errors.Is(err, ErrNoSession) {
		t.Error("session survived password reset")
	}
	newEn, err := svc.ResetTOTP(ctx)
	if err != nil || newEn.Secret == en.Secret {
		t.Fatalf("ResetTOTP = %+v, %v", newEn, err)
	}
	clock.Advance(30 * time.Second)
	if _, err := svc.Login(ctx, "owner", "a different passphrase", code(t, en.Secret, clock.Now()), "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old TOTP secret still accepted: %v", err)
	}
	if _, err := svc.Login(ctx, "owner", "a different passphrase", code(t, newEn.Secret, clock.Now()), "192.0.2.1"); err != nil {
		t.Errorf("new TOTP secret: %v", err)
	}
}
