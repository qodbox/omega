// Package billing talks to Stripe over its REST API and verifies the webhooks
// it sends back. It is written against net/http on purpose: a project built on
// Omega inherits no third-party billing SDK, and no SDK release can change what
// this layer does behind its back.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is Stripe's API host. Tests point this at an httptest server.
	DefaultBaseURL = "https://api.stripe.com"

	// DefaultAPIVersion pins the request/response shapes. Stripe never changes
	// an existing version, so an unattended upgrade cannot reshape a payload
	// under a running deployment; raise it deliberately, after reading the
	// changelog.
	DefaultAPIVersion = "2024-06-20"
)

// ErrNotConfigured is returned by every call when no secret key is set. It lets
// an application boot, and expose its other routes, without billing credentials.
var ErrNotConfigured = errors.New("billing: no Stripe secret key configured")

// Config carries what the client needs to reach Stripe.
type Config struct {
	SecretKey  string
	APIVersion string
	BaseURL    string
	Timeout    time.Duration

	// MaxAttempts bounds the retries of a request Stripe answered with a 5xx,
	// a 429, or that never reached it. One attempt means no retry.
	MaxAttempts int

	// Backoff is the pause before the second attempt; it doubles after each one.
	Backoff time.Duration
}

// Client is a Stripe API client. The zero value is unusable: build one with New.
type Client struct {
	secret     string
	apiVersion string
	base       string
	attempts   int
	backoff    time.Duration
	http       *http.Client
}

// New builds a client from a config, filling in the defaults. A client without
// a secret key is still returned: every call then fails with ErrNotConfigured,
// which the service layer turns into a clean 503 rather than a panic at boot.
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	backoff := cfg.Backoff
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}
	version := strings.TrimSpace(cfg.APIVersion)
	if version == "" {
		version = DefaultAPIVersion
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}

	return &Client{
		secret:     strings.TrimSpace(cfg.SecretKey),
		apiVersion: version,
		base:       base,
		attempts:   attempts,
		backoff:    backoff,
		http:       &http.Client{Timeout: timeout},
	}
}

// Configured reports whether a secret key was supplied.
func (c *Client) Configured() bool { return c != nil && c.secret != "" }

// Error is a failure Stripe described in its own terms.
type Error struct {
	Status  int    `json:"-"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Param   string `json:"param"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("stripe: %s (%d)", e.Message, e.Status)
	}
	return fmt.Sprintf("stripe: request failed with %d", e.Status)
}

// Retryable reports whether re-sending the same request could succeed.
func (e *Error) Retryable() bool { return e.Status == http.StatusTooManyRequests || e.Status >= 500 }

// get reads a resource.
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, "", out)
}

// post writes one. An idempotency key is mandatory: Stripe replays the first
// answer instead of creating a second customer or a second subscription when a
// retry — ours or the caller's — repeats a request that already went through.
func (c *Client) post(ctx context.Context, path string, form url.Values, idempotencyKey string, out any) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return errors.New("billing: a write to Stripe needs an idempotency key")
	}
	return c.do(ctx, http.MethodPost, path, form, idempotencyKey, out)
}

func (c *Client) do(ctx context.Context, method, path string, form url.Values, idempotencyKey string, out any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}

	var lastErr error
	pause := c.backoff

	for attempt := 1; attempt <= c.attempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pause):
			}
			pause *= 2
		}

		retryable, err := c.attempt(ctx, method, path, form, idempotencyKey, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			return err
		}
	}
	return lastErr
}

// attempt sends the request once and reports whether a retry is worth trying.
func (c *Client) attempt(ctx context.Context, method, path string, form url.Values, idempotencyKey string, out any) (retryable bool, err error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	request, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return false, err
	}

	request.Header.Set("Authorization", "Bearer "+c.secret)
	request.Header.Set("Stripe-Version", c.apiVersion)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}

	response, err := c.http.Do(request)
	if err != nil {
		// The request may or may not have reached Stripe. Retrying is safe:
		// a GET has no effect, and a POST carries an idempotency key.
		return true, err
	}
	defer response.Body.Close()

	// A Stripe payload is small; the cap is there so a wrong host cannot feed
	// us an unbounded body.
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return true, err
	}

	if response.StatusCode >= 400 {
		failure := decodeError(raw, response.StatusCode)
		return failure.Retryable(), failure
	}

	if out == nil {
		return false, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return false, fmt.Errorf("billing: decoding the Stripe answer: %w", err)
	}
	return false, nil
}

func decodeError(raw []byte, status int) *Error {
	var envelope struct {
		Error Error `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return &Error{Status: status}
	}
	envelope.Error.Status = status
	return &envelope.Error
}
