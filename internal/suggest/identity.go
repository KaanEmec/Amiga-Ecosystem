package suggest

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Form timing errors returned by Signer.CheckFormToken.
var (
	ErrFormToken   = errors.New("the form token is missing or invalid")
	ErrFormTooFast = errors.New("the form was submitted too quickly")
	ErrFormExpired = errors.New("the form has expired")
)

const visitorIDBytes = 16

var b64 = base64.RawURLEncoding

// Signer signs the browser-id cookie and the form timing token, and hashes
// client IPs, all with the application secret.
type Signer struct {
	secret []byte
}

// NewSigner builds a Signer from the application secret.
func NewSigner(secret []byte) *Signer { return &Signer{secret: secret} }

func (s *Signer) mac(purpose string, data []byte) []byte {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write(data)
	return m.Sum(nil)
}

// NewVisitor returns a fresh signed cookie value and the visitor id it
// carries (base64url of 16 random bytes).
func (s *Signer) NewVisitor() (token, id string, err error) {
	raw := make([]byte, visitorIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generating visitor id: %w", err)
	}
	return b64.EncodeToString(append(raw, s.mac("vid", raw)...)), b64.EncodeToString(raw), nil
}

// VisitorID verifies a cookie value and returns the visitor id.
func (s *Signer) VisitorID(token string) (string, bool) {
	b, err := b64.DecodeString(token)
	if err != nil || len(b) != visitorIDBytes+sha256.Size {
		return "", false
	}
	raw, sum := b[:visitorIDBytes], b[visitorIDBytes:]
	if !hmac.Equal(sum, s.mac("vid", raw)) {
		return "", false
	}
	return b64.EncodeToString(raw), true
}

// FormToken returns a signed timestamp for the suggestion form.
func (s *Signer) FormToken(now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	return ts + "." + b64.EncodeToString(s.mac("form_ts", []byte(ts)))
}

// CheckFormToken verifies a form token and that it was issued between min
// and max before now.
func (s *Signer) CheckFormToken(token string, now time.Time, minAge, maxAge time.Duration) error {
	ts, sig, ok := strings.Cut(token, ".")
	if !ok {
		return ErrFormToken
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(got, s.mac("form_ts", []byte(ts))) {
		return ErrFormToken
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrFormToken
	}
	age := now.Sub(time.Unix(sec, 0))
	switch {
	case age < minAge:
		return ErrFormTooFast
	case age > maxAge:
		return ErrFormExpired
	}
	return nil
}

// HashIP returns hex(sha256(secret || ip)); the raw IP is never stored.
func (s *Signer) HashIP(ip string) string {
	if ip == "" {
		return ""
	}
	h := sha256.New()
	h.Write(s.secret)
	h.Write([]byte(ip))
	return hex.EncodeToString(h.Sum(nil))
}
