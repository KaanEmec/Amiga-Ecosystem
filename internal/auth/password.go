package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password length policy.
const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
)

// HashParams are the argon2id cost parameters for new hashes. Verification
// always uses the parameters encoded in the stored hash.
type HashParams struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// DefaultHashParams: 64 MiB, 3 passes, 2 lanes.
var DefaultHashParams = HashParams{MemoryKiB: 64 * 1024, Time: 3, Threads: 2}

const (
	saltLen = 16
	keyLen  = 32
)

// maxVerifyMemoryKiB caps the memory a stored hash may demand (256 MiB).
const maxVerifyMemoryKiB = 256 * 1024

// CheckPassword enforces the length policy.
func CheckPassword(pw string) error {
	n := len([]rune(pw))
	if n < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(pw) > MaxPasswordLen {
		return fmt.Errorf("password must be at most %d bytes", MaxPasswordLen)
	}
	return nil
}

// hashPassword returns an encoded PHC string:
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<key> (unpadded standard base64).
func hashPassword(pw string, p HashParams) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, keyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// verifyPassword reports whether pw matches the encoded hash, in constant
// time with respect to the key bytes.
func verifyPassword(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var p HashParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return false, fmt.Errorf("parsing argon2 parameters: %w", err)
	}
	if p.MemoryKiB == 0 || p.MemoryKiB > maxVerifyMemoryKiB || p.Time == 0 || p.Time > 16 || p.Threads == 0 {
		return false, errors.New("argon2 parameters out of range")
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decoding salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false, errors.New("decoding argon2 key")
	}
	got := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
