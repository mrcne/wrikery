// Package wrike is a standalone client for the Wrike REST API v4.
// It imports only the standard library and knows nothing about the rest of the application.
package wrike

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

// DefaultHost and EUHost are the two hosts Wrike serves API traffic from.
// Which one accepts a token depends on the data center the account lives in, see https://developers.wrike.com/docs/faq:
// "The web host for API endpoints differ depending on the datacenter that contains user's data".
const (
	DefaultHost = "www.wrike.com"
	EUHost      = "app-eu.wrike.com"
)

// BaseURL builds the API v4 base URL for a host, such as DefaultHost or EUHost.
func BaseURL(host string) string {
	return "https://" + host + "/api/v4"
}

// Client talks to the Wrike REST API v4 over HTTP.
// A rate limited request is always retried with backoff, using the Retry-After value when
// Wrike sends one. A server error or a network failure is retried the same way, but only for
// an idempotent method, since a POST that failed on the server side may already be applied and
// retrying it could duplicate the write.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	maxRetries int
	sleep      func(ctx context.Context, d time.Duration) error
}

// Option configures a Client constructed by New.
type Option func(*Client)

// WithBaseURL points the client at a base URL other than the default host, such as EUHost's.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient replaces the default HTTP client, for a custom timeout or transport.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// New creates a Client for the given API token, defaulting to DefaultHost until an Option
// such as WithBaseURL points it elsewhere.
func New(token string, opts ...Option) *Client {
	c := &Client{
		baseURL:    BaseURL(DefaultHost),
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: 3,
		sleep:      sleepContext,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// envelope is the JSON wrapper every Wrike response uses.
type envelope struct {
	Kind          string          `json:"kind"`
	NextPageToken string          `json:"nextPageToken"`
	Data          json.RawMessage `json:"data"`
}

// doOnce performs one API request and unmarshals the envelope's data into out.
// out may be nil when the caller does not need the response body.
func (c *Client) doOnce(ctx context.Context, method, path string, query url.Values, form url.Values, out any) (string, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if len(form) > 0 {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if len(form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	// The http client follows real redirects itself,
	// so a 3xx that reaches this code is an answer, not a redirect in progress.
	// Wrike answers 300 with an empty body when the token's account lives in another data center,
	// observed on GET /contacts?me=true, so treat any 3xx as an API error too.
	if resp.StatusCode >= 300 {
		return "", parseAPIError(resp.StatusCode, resp.Header, raw)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("decoding %s %s response: %w", method, path, err)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return "", fmt.Errorf("decoding %s %s data: %w", method, path, err)
		}
	}
	return env.NextPageToken, nil
}

// do wraps doOnce with the retry policy: rate limits and transient
// failures are retried with backoff, everything else returns immediately.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, form url.Values, out any) (string, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		next, err := c.doOnce(ctx, method, path, query, form, out)
		if err == nil {
			return next, nil
		}
		lastErr = err
		if attempt >= c.maxRetries || !retryable(method, err) {
			return "", lastErr
		}
		if serr := c.sleep(ctx, retryDelay(err, attempt)); serr != nil {
			return "", serr
		}
	}
}

// retryable is true for rate limits always, and for server errors and network failures only on idempotent methods.
// A POST that hit a server error may already be applied, retrying it could duplicate the write.
func retryable(method string, err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return idempotent(method) && apiErr.StatusCode >= 500
	}
	var urlErr *url.Error
	return idempotent(method) && errors.As(err, &urlErr)
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

func retryDelay(err error, attempt int) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return time.Duration(1<<attempt) * time.Second
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
