package wrike

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("test-token", WithBaseURL(srv.URL))
	c.sleep = func(ctx context.Context, d time.Duration) error { return nil }
	return c
}

func TestDoSendsAuthAndDecodesData(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		if r.URL.Path != "/things" {
			t.Errorf("path = %q, want /things", r.URL.Path)
		}
		if got := r.URL.Query().Get("pageSize"); got != "2" {
			t.Errorf("pageSize = %q, want 2", got)
		}
		_, _ = w.Write([]byte(`{"kind":"things","nextPageToken":"tok123","data":[{"id":"A"},{"id":"B"}]}`))
	}))

	var out []struct {
		ID string `json:"id"`
	}
	q := url.Values{}
	q.Set("pageSize", "2")
	next, err := c.do(context.Background(), http.MethodGet, "/things", q, nil, &out)
	if err != nil {
		t.Fatal(err)
	}
	if next != "tok123" {
		t.Errorf("nextPageToken = %q, want tok123", next)
	}
	if len(out) != 2 || out[0].ID != "A" || out[1].ID != "B" {
		t.Errorf("out = %+v", out)
	}
}

func TestDoFormEncodesBody(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want PUT", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("title"); got != "new title" {
			t.Errorf("title = %q, want new title", got)
		}
		_, _ = w.Write([]byte(`{"kind":"things","data":[]}`))
	}))

	form := url.Values{}
	form.Set("title", "new title")
	if _, err := c.do(context.Background(), http.MethodPut, "/things/T1", nil, form, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDoReturnsTypedAPIError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorDescription":"Token is invalid","error":"not_authorized"}`))
	}))

	_, err := c.do(context.Background(), http.MethodGet, "/contacts", nil, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 || apiErr.Code != "not_authorized" {
		t.Errorf("apiErr = %+v", apiErr)
	}
	if !apiErr.IsAuth() || apiErr.IsRateLimit() || apiErr.IsNotFound() {
		t.Errorf("classification wrong: %+v", apiErr)
	}
	if apiErr.Method != "GET" || apiErr.Path != "/contacts" {
		t.Errorf("request on the error = %q %q, want GET /contacts", apiErr.Method, apiErr.Path)
	}
	// The log line for a rejected request is all a reader gets, it must say which request.
	if got := apiErr.Error(); !strings.Contains(got, "GET /contacts") || !strings.Contains(got, "not_authorized") {
		t.Errorf("Error() = %q, want the request and the code", got)
	}
}

func TestDoAPIErrorWithUnparseableBody(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`upstream exploded`))
	}))

	_, err := c.do(context.Background(), http.MethodGet, "/contacts", nil, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 502 {
		t.Errorf("StatusCode = %d, want 502", apiErr.StatusCode)
	}
}

func TestDoRejectsMalformedEnvelope(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`this is not json`))
	}))

	if _, err := c.do(context.Background(), http.MethodGet, "/contacts", nil, nil, nil); err == nil {
		t.Fatal("want decode error, got nil")
	}
}

func TestWithBaseURLTrimsTrailingSlash(t *testing.T) {
	c := New("tok", WithBaseURL("https://example.test/api/v4/"))
	if c.baseURL != "https://example.test/api/v4" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestBaseURLBuildsFromHost(t *testing.T) {
	if got := BaseURL(EUHost); got != "https://app-eu.wrike.com/api/v4" {
		t.Errorf("BaseURL(EUHost) = %q, want https://app-eu.wrike.com/api/v4", got)
	}
}

func TestDoSendsDefaultUserAgent(t *testing.T) {
	var got string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"kind":"things","data":[]}`))
	}))

	if _, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if want := "wrikery (+https://github.com/mrcne/wrikery)"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}

func TestWithUserAgentOverridesDefault(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"kind":"things","data":[]}`))
	}))
	t.Cleanup(srv.Close)

	c := New("test-token", WithBaseURL(srv.URL), WithUserAgent("wrikery/1.2.3 (+https://github.com/mrcne/wrikery)"))
	c.sleep = func(ctx context.Context, d time.Duration) error { return nil }

	if _, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if want := "wrikery/1.2.3 (+https://github.com/mrcne/wrikery)"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}

func TestDo300WithEmptyBodyIsWrongHostErrorAndNotRetried(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusMultipleChoices)
	}))

	_, err := c.do(context.Background(), http.MethodGet, "/contacts", nil, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsWrongHost() {
		t.Errorf("IsWrongHost() = false, want true: %+v", apiErr)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1: a 300 must not be retried", calls)
	}
}
