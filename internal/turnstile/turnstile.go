// Package turnstile verifies Cloudflare Turnstile tokens server-side and
// decides, from configuration, whether verification runs at all.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"amigaconstellation/internal/config"
)

// SiteverifyURL is Cloudflare's server-side validation endpoint.
const SiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Actions passed to the widget's data-action and checked on verification.
const (
	ActionSuggest = "suggest"
	ActionVote    = "vote"
)

// timeout bounds one siteverify round trip.
const timeout = 5 * time.Second

// maxTokenLen is Cloudflare's documented maximum token length.
const maxTokenLen = 2048

// ErrFailed is returned (wrapped) whenever a token is not accepted.
var ErrFailed = errors.New("turnstile verification failed")

// Verifier checks a widget token for the given action.
type Verifier interface {
	Verify(ctx context.Context, token, remoteIP, action string) error
}

// Client calls the siteverify endpoint.
type Client struct {
	secret   string
	hostname string // expected hostname; empty skips the check
	endpoint string
	http     *http.Client
}

// NewClient builds a siteverify client. hostname is the expected widget
// hostname (from AMIGA_BASE_URL); endpoint "" means SiteverifyURL.
func NewClient(secret, hostname, endpoint string) *Client {
	if endpoint == "" {
		endpoint = SiteverifyURL
	}
	return &Client{
		secret:   secret,
		hostname: hostname,
		endpoint: endpoint,
		http:     &http.Client{Timeout: timeout},
	}
}

type siteverifyResponse struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify posts the token to siteverify and checks success, hostname and
// action. Any transport or decoding problem is a failure (fail closed).
func (c *Client) Verify(ctx context.Context, token, remoteIP, action string) error {
	if token == "" || len(token) > maxTokenLen {
		return fmt.Errorf("%w: missing or oversized token", ErrFailed)
	}
	form := url.Values{"secret": {c.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: building request: %w", ErrFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: siteverify: %w", ErrFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: siteverify status %d", ErrFailed, resp.StatusCode)
	}
	var out siteverifyResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return fmt.Errorf("%w: decoding siteverify: %w", ErrFailed, err)
	}
	switch {
	case !out.Success:
		return fmt.Errorf("%w: %s", ErrFailed, strings.Join(out.ErrorCodes, ","))
	case c.hostname != "" && !strings.EqualFold(out.Hostname, c.hostname):
		return fmt.Errorf("%w: hostname %q, want %q", ErrFailed, out.Hostname, c.hostname)
	case out.Action != action:
		return fmt.Errorf("%w: action %q, want %q", ErrFailed, out.Action, action)
	}
	return nil
}

// skip accepts everything; used only in development without keys.
type skip struct{}

func (skip) Verify(context.Context, string, string, string) error { return nil }

// refuse rejects everything; the fail-closed fallback.
type refuse struct{}

func (refuse) Verify(context.Context, string, string, string) error {
	return fmt.Errorf("%w: turnstile is not configured", ErrFailed)
}

// FromConfig applies the policy: keys configured -> verify with Cloudflare;
// no keys outside production -> skip (with a warning); no keys in production
// -> refuse every token (config.Load already rejects that configuration, so
// this is defence in depth). enabled reports whether the widget should be
// rendered.
func FromConfig(cfg config.Config, log *slog.Logger) (v Verifier, enabled bool) {
	if cfg.TurnstileSecret != "" {
		host := ""
		if u, err := url.Parse(cfg.BaseURL); err == nil {
			host = u.Hostname()
		}
		return NewClient(cfg.TurnstileSecret, host, ""), true
	}
	if cfg.Production() {
		log.Error("turnstile keys missing in production; public mutations are refused")
		return refuse{}, false
	}
	log.Warn("turnstile disabled: AMIGA_TURNSTILE_SITE_KEY/SECRET not set (development only)")
	return skip{}, false
}
