package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTP parameters: RFC 6238 defaults understood by every authenticator app.
const (
	totpPeriod = 30
	totpSkew   = 1 // accept one step either side
	issuer     = "Amiga Constellation"
)

var totpOpts = totp.ValidateOpts{Period: totpPeriod, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// recoveryCodeCount is how many recovery codes an owner holds.
const recoveryCodeCount = 10

// Enrollment is shown once, by the CLI, when a TOTP secret or recovery
// codes are (re)generated.
type Enrollment struct {
	URI           string   // otpauth:// URI for QR/authenticator import
	Secret        string   // base32 secret for manual entry
	RecoveryCodes []string // plain recovery codes; only hashes are stored
}

func newTOTPKey(account string) (*otp.Key, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      totpPeriod,
		SecretSize:  20,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, fmt.Errorf("generating TOTP secret: %w", err)
	}
	return key, nil
}

// matchTOTP returns the time step whose code equals code, checking the
// current step and totpSkew steps either side. Each candidate is compared
// in constant time.
func matchTOTP(secret, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	step := now.Unix() / totpPeriod
	var found int64
	ok := false
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		s := step + d
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0), totpOpts)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 && !ok {
			found, ok = s, true
		}
	}
	return found, ok
}

// newRecoveryCodes returns plain codes (xxxxx-xxxxx, 50 bits each) and
// their hashes.
func newRecoveryCodes() (codes, hashes []string, err error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	for range recoveryCodeCount {
		b := make([]byte, 7)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, fmt.Errorf("generating recovery code: %w", err)
		}
		s := strings.ToLower(enc.EncodeToString(b))[:10]
		codes = append(codes, s[:5]+"-"+s[5:])
		hashes = append(hashes, hashRecoveryCode(s))
	}
	return codes, hashes, nil
}

// normalizeCode strips spaces and hyphens and lowercases.
func normalizeCode(code string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code)))
}

func hashRecoveryCode(normalized string) string {
	sum := sha256.Sum256([]byte("amiga-recovery\x00" + normalized))
	return hex.EncodeToString(sum[:])
}

// matchRecoveryCode returns the index of the stored hash matching code, or
// -1. Every stored hash is compared in constant time.
func matchRecoveryCode(hashes []string, code string) int {
	n := normalizeCode(code)
	if len(n) != 10 {
		return -1
	}
	h := []byte(hashRecoveryCode(n))
	idx := -1
	for i, stored := range hashes {
		if subtle.ConstantTimeCompare(h, []byte(stored)) == 1 && idx < 0 {
			idx = i
		}
	}
	return idx
}
