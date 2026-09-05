package wrike

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("test-token", WithBaseURL(srv.URL))
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
		w.Write([]byte(`{"kind":"things","nextPageToken":"tok123","data":[{"id":"A"},{"id":"B"}]}`)) //nolint:errcheck
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
		w.Write([]byte(`{"kind":"things","data":[]}`)) //nolint:errcheck
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
		w.Write([]byte(`{"errorDescription":"Token is invalid","error":"not_authorized"}`)) //nolint:errcheck
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
	if apiErr.Error() == "" {
		t.Error("Error() must not be empty")
	}
}

func TestDoAPIErrorWithUnparseableBody(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`upstream exploded`)) //nolint:errcheck
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
		w.Write([]byte(`this is not json`)) //nolint:errcheck
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
