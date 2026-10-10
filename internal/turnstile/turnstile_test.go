package turnstile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"amigaconstellation/internal/config"
)

func siteverify(t *testing.T, body string, check func(r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_Verify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		action  string
		token   string
		wantErr bool
	}{
		{"success", `{"success":true,"hostname":"amiga.example","action":"suggest"}`, ActionSuggest, "tok", false},
		{"failure", `{"success":false,"error-codes":["invalid-input-response"]}`, ActionSuggest, "tok", true},
		{"wrong hostname", `{"success":true,"hostname":"evil.example","action":"suggest"}`, ActionSuggest, "tok", true},
		{"wrong action", `{"success":true,"hostname":"amiga.example","action":"vote"}`, ActionSuggest, "tok", true},
		{"malformed", `not json`, ActionVote, "tok", true},
		{"empty token", `{"success":true,"hostname":"amiga.example","action":"vote"}`, ActionVote, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := siteverify(t, tt.body, func(r *http.Request) {
				if r.Method != http.MethodPost || r.PostForm.Get("secret") != "s3cret" ||
					r.PostForm.Get("response") != tt.token || r.PostForm.Get("remoteip") != "203.0.113.9" {
					t.Errorf("unexpected request: %s %v", r.Method, r.PostForm)
				}
			})
			err := NewClient("s3cret", "amiga.example", srv.URL).Verify(t.Context(), tt.token, "203.0.113.9", tt.action)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrFailed) {
				t.Errorf("error %v does not wrap ErrFailed", err)
			}
		})
	}
}

func TestClient_Verify_ServerDown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if err := NewClient("s", "", srv.URL).Verify(t.Context(), "tok", "", ActionVote); !errors.Is(err, ErrFailed) {
		t.Fatalf("Verify() = %v, want ErrFailed", err)
	}
}

func TestClient_Verify_Timeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // lets the server notice the client hanging up
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := NewClient("s", "", srv.URL).Verify(ctx, "tok", "", ActionVote); !errors.Is(err, ErrFailed) {
		t.Fatalf("Verify() = %v, want ErrFailed", err)
	}
}

func TestFromConfig(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name        string
		cfg         config.Config
		wantEnabled bool
		wantPass    bool
	}{
		{"development without keys skips", config.Config{Env: config.EnvDevelopment}, false, true},
		{"production without keys refuses", config.Config{Env: config.EnvProduction}, false, false},
		{"keys verify", config.Config{Env: config.EnvProduction, TurnstileSiteKey: "k", TurnstileSecret: "s", BaseURL: "https://amiga.example"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, enabled := FromConfig(tt.cfg, log)
			if enabled != tt.wantEnabled {
				t.Errorf("enabled = %v, want %v", enabled, tt.wantEnabled)
			}
			// An empty token never reaches the network.
			err := v.Verify(t.Context(), "", "", ActionVote)
			if (err == nil) != tt.wantPass {
				t.Errorf("Verify() = %v, want pass %v", err, tt.wantPass)
			}
		})
	}
	v, _ := FromConfig(config.Config{TurnstileSecret: "s", BaseURL: "https://amiga.example:8443"}, log)
	if c, ok := v.(*Client); !ok || c.hostname != "amiga.example" {
		t.Errorf("client hostname = %+v", v)
	}
}
