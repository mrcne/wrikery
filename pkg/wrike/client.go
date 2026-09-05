// Package wrike is a standalone client for the Wrike REST API v4.
// It imports only the standard library and knows nothing about the rest of the application.
package wrike

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://www.wrike.com/api/v4"

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

func New(token string, opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
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

// do performs one API request and unmarshals the envelope's data into out.
// out may be nil when the caller does not need the response body.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, form url.Values, out any) (string, error) {
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
	if resp.StatusCode >= 400 {
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
