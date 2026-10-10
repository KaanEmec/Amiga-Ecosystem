// Package config reads the AMIGA_* environment variables into a Config.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Environments accepted in AMIGA_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// minSecretLen is the minimum decoded length of AMIGA_SECRET in bytes.
const minSecretLen = 32

// Config is the application configuration. It is built once by Load and
// passed explicitly to every component that needs it.
type Config struct {
	Addr             string // AMIGA_ADDR, listen address
	DataDir          string // AMIGA_DATA_DIR, holds amiga.db, media/, staging/
	Env              string // AMIGA_ENV, development or production
	Secret           []byte // AMIGA_SECRET, decoded; signs cookies
	SeedJSON         string // AMIGA_SEED_JSON, seed catalogue file
	SeedImages       string // AMIGA_SEED_IMAGES, seed images directory
	TurnstileSiteKey string // AMIGA_TURNSTILE_SITE_KEY
	TurnstileSecret  string // AMIGA_TURNSTILE_SECRET
	BaseURL          string // AMIGA_BASE_URL, public origin, e.g. https://amiga.example

	// Suggestion and vote rate limits (per signed browser id, and per
	// hashed client IP as a shared ceiling).
	RateSuggestPerDay   int // AMIGA_RATE_SUGGEST_DAY, default 5
	RateSuggestPerDayIP int // AMIGA_RATE_SUGGEST_DAY_IP, default 25
	RateVotePerHour     int // AMIGA_RATE_VOTE_HOUR, default 30
	RateVotePerHourIP   int // AMIGA_RATE_VOTE_HOUR_IP, default 150
	// +1s one hashed IP may add to a single suggestion per day, since
	// browser ids (cookies) are free to mint.
	RateVotePerSuggestionIP int  // AMIGA_RATE_VOTE_SUGGESTION_IP, default 20
	TrustProxyForwarding    bool // AMIGA_TRUST_PROXY=1: client IP is the last X-Forwarded-For hop
}

// Production reports whether the app runs with production policies.
func (c Config) Production() bool { return c.Env == EnvProduction }

// DBPath is the SQLite database file.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "amiga.db") }

// MediaDir holds published images, served at /images/.
func (c Config) MediaDir() string { return filepath.Join(c.DataDir, "media") }

// StagingDir holds fetched but unpublished images.
func (c Config) StagingDir() string { return filepath.Join(c.DataDir, "staging") }

// Load builds a Config from getenv (normally os.Getenv). In development a
// missing AMIGA_SECRET is generated once and persisted to <data_dir>/secret;
// in production it is required.
func Load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	cfg := Config{
		Addr:             get("AMIGA_ADDR", ":8080"),
		DataDir:          get("AMIGA_DATA_DIR", "./runtime"),
		Env:              get("AMIGA_ENV", EnvDevelopment),
		SeedJSON:         get("AMIGA_SEED_JSON", "./data/amiga-data.json"),
		SeedImages:       get("AMIGA_SEED_IMAGES", "./images"),
		TurnstileSiteKey: get("AMIGA_TURNSTILE_SITE_KEY", ""),
		TurnstileSecret:  get("AMIGA_TURNSTILE_SECRET", ""),
		BaseURL:          strings.TrimRight(get("AMIGA_BASE_URL", ""), "/"),
	}

	if cfg.Env != EnvDevelopment && cfg.Env != EnvProduction {
		return Config{}, fmt.Errorf("AMIGA_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, cfg.Env)
	}
	if (cfg.TurnstileSiteKey == "") != (cfg.TurnstileSecret == "") {
		return Config{}, errors.New("AMIGA_TURNSTILE_SITE_KEY and AMIGA_TURNSTILE_SECRET must be set together")
	}
	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return Config{}, fmt.Errorf("AMIGA_BASE_URL must be an origin like https://amiga.example, got %q", cfg.BaseURL)
		}
	}
	if cfg.Production() && cfg.BaseURL == "" {
		return Config{}, errors.New("AMIGA_BASE_URL is required when AMIGA_ENV=production")
	}
	rates := []struct {
		key string
		dst *int
		def int
	}{
		{"AMIGA_RATE_SUGGEST_DAY", &cfg.RateSuggestPerDay, 5},
		{"AMIGA_RATE_SUGGEST_DAY_IP", &cfg.RateSuggestPerDayIP, 25},
		{"AMIGA_RATE_VOTE_HOUR", &cfg.RateVotePerHour, 30},
		{"AMIGA_RATE_VOTE_HOUR_IP", &cfg.RateVotePerHourIP, 150},
		{"AMIGA_RATE_VOTE_SUGGESTION_IP", &cfg.RateVotePerSuggestionIP, 20},
	}
	for _, r := range rates {
		n, err := strconv.Atoi(get(r.key, strconv.Itoa(r.def)))
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("%s must be a positive integer", r.key)
		}
		*r.dst = n
	}
	cfg.TrustProxyForwarding = get("AMIGA_TRUST_PROXY", "") == "1"

	secret, err := loadSecret(getenv("AMIGA_SECRET"), cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.Secret = secret
	// Public mutations fail closed: production never runs without Turnstile.
	if cfg.Production() && cfg.TurnstileSecret == "" {
		return Config{}, errors.New("AMIGA_TURNSTILE_SITE_KEY and AMIGA_TURNSTILE_SECRET are required when AMIGA_ENV=production")
	}
	return cfg, nil
}

func loadSecret(raw string, cfg Config) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		b, err := decodeSecret(raw)
		if err != nil {
			return nil, fmt.Errorf("AMIGA_SECRET: %w", err)
		}
		return b, nil
	}
	if cfg.Production() {
		return nil, fmt.Errorf("AMIGA_SECRET is required when AMIGA_ENV=production (at least %d random bytes, hex or base64; e.g. `openssl rand -hex 32`)", minSecretLen)
	}
	return devSecret(filepath.Join(cfg.DataDir, "secret"))
}

// devSecret reads the persisted development secret or creates it.
func devSecret(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		b, err := decodeSecret(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("development secret %s: %w", path, err)
		}
		return b, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading development secret: %w", err)
	}
	b := make([]byte, minSecretLen)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generating development secret: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(b)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("writing development secret: %w", err)
	}
	return b, nil
}

// decodeSecret accepts hex, standard or URL-safe base64 (padded or not).
func decodeSecret(s string) ([]byte, error) {
	decoders := []func(string) ([]byte, error){
		hex.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	}
	for _, dec := range decoders {
		if b, err := dec(s); err == nil {
			if len(b) < minSecretLen {
				return nil, fmt.Errorf("decodes to %d bytes, need at least %d", len(b), minSecretLen)
			}
			return b, nil
		}
	}
	return nil, errors.New("must be hex or base64 encoded")
}
