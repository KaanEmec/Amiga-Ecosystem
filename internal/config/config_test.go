package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const hexSecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func TestLoad(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "development defaults", env: map[string]string{}},
		{name: "production with secret and base url", env: map[string]string{"AMIGA_ENV": "production", "AMIGA_SECRET": hexSecret, "AMIGA_BASE_URL": "https://amiga.example", "AMIGA_TURNSTILE_SITE_KEY": "k", "AMIGA_TURNSTILE_SECRET": "s"}},
		{name: "production base64 secret", env: map[string]string{"AMIGA_ENV": "production", "AMIGA_SECRET": "ABEiM0RVZneImaq7zN3u/wARIjNEVWZ3iJmqu8zd7v8=", "AMIGA_BASE_URL": "https://amiga.example", "AMIGA_TURNSTILE_SITE_KEY": "k", "AMIGA_TURNSTILE_SECRET": "s"}},
		{name: "production without turnstile", env: map[string]string{"AMIGA_ENV": "production", "AMIGA_SECRET": hexSecret, "AMIGA_BASE_URL": "https://amiga.example"}, wantErr: "AMIGA_TURNSTILE_SITE_KEY and AMIGA_TURNSTILE_SECRET are required"},
		{name: "bad rate limit", env: map[string]string{"AMIGA_RATE_VOTE_HOUR": "0"}, wantErr: "AMIGA_RATE_VOTE_HOUR must be"},
		{name: "production without secret", env: map[string]string{"AMIGA_ENV": "production", "AMIGA_BASE_URL": "https://amiga.example"}, wantErr: "AMIGA_SECRET is required"},
		{name: "production without base url", env: map[string]string{"AMIGA_ENV": "production", "AMIGA_SECRET": hexSecret}, wantErr: "AMIGA_BASE_URL is required"},
		{name: "unknown env", env: map[string]string{"AMIGA_ENV": "staging"}, wantErr: "AMIGA_ENV must be"},
		{name: "short secret", env: map[string]string{"AMIGA_SECRET": "abcd"}, wantErr: "need at least 32"},
		{name: "garbage secret", env: map[string]string{"AMIGA_SECRET": "not a secret!"}, wantErr: "hex or base64"},
		{name: "base url with path", env: map[string]string{"AMIGA_BASE_URL": "https://amiga.example/app"}, wantErr: "AMIGA_BASE_URL must be an origin"},
		{name: "half turnstile config", env: map[string]string{"AMIGA_TURNSTILE_SITE_KEY": "x"}, wantErr: "must be set together"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.env["AMIGA_DATA_DIR"] = t.TempDir()
			cfg, err := Load(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if len(cfg.Secret) < minSecretLen {
				t.Errorf("secret length = %d, want >= %d", len(cfg.Secret), minSecretLen)
			}
		})
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg, err := Load(env(map[string]string{"AMIGA_DATA_DIR": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.Env != EnvDevelopment || cfg.SeedJSON != "./data/amiga-data.json" || cfg.SeedImages != "./images" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Production() {
		t.Error("Production() = true for development")
	}
	if cfg.RateVotePerSuggestionIP != 20 {
		t.Errorf("RateVotePerSuggestionIP = %d, want 20", cfg.RateVotePerSuggestionIP)
	}
	if got, want := cfg.MediaDir(), filepath.Join(dir, "media"); got != want {
		t.Errorf("MediaDir() = %q, want %q", got, want)
	}
}

func TestLoad_DevSecretPersisted(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested")
	e := env(map[string]string{"AMIGA_DATA_DIR": dir})
	first, err := Load(e)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "secret"))
	if err != nil {
		t.Fatalf("secret file not written: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("secret file mode = %v, want 0600", perm)
	}
	second, err := Load(e)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Secret, second.Secret) {
		t.Error("development secret changed between loads")
	}
}
