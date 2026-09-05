package wrike

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// retryTestClient records sleeps instead of sleeping.
func retryTestClient(t *testing.T, handler http.Handler) (*Client, *[]time.Duration) {
	t.Helper()
	c := newTestClient(t, handler)
	var slept []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return c, &slept
}

func Test429HonorsRetryAfter(t *testing.T) {
	calls := 0
	c, slept := retryTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limit_exceeded","errorDescription":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"kind":"things","data":[]}`))
	}))

	if _, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	want := []time.Duration{7 * time.Second, 7 * time.Second}
	if len(*slept) != 2 || (*slept)[0] != want[0] || (*slept)[1] != want[1] {
		t.Errorf("slept = %v, want %v", *slept, want)
	}
}

func Test429WithoutHeaderUsesBackoff(t *testing.T) {
	calls := 0
	c, slept := retryTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limit_exceeded","errorDescription":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"kind":"things","data":[]}`))
	}))

	if _, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second}
	if len(*slept) != 2 || (*slept)[0] != want[0] || (*slept)[1] != want[1] {
		t.Errorf("slept = %v, want %v", *slept, want)
	}
}

func TestServerErrorsAreRetried(t *testing.T) {
	calls := 0
	c, _ := retryTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"server_error","errorDescription":"oops"}`))
			return
		}
		_, _ = w.Write([]byte(`{"kind":"things","data":[]}`))
	}))

	if _, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	calls := 0
	c, slept := retryTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_parameter","errorDescription":"bad"}`))
	}))

	_, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Fatalf("want 400 APIError, got %v", err)
	}
	if calls != 1 || len(*slept) != 0 {
		t.Errorf("calls = %d, slept = %v, want 1 call and no sleeps", calls, *slept)
	}
}

func TestRetriesExhaustedReturnsLastError(t *testing.T) {
	calls := 0
	c, slept := retryTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limit_exceeded","errorDescription":"slow down"}`))
	}))

	_, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.IsRateLimit() {
		t.Fatalf("want rate limit APIError, got %v", err)
	}
	if calls != 4 {
		t.Errorf("calls = %d, want 4 (1 attempt + 3 retries)", calls)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	if len(*slept) != 3 || (*slept)[0] != want[0] || (*slept)[1] != want[1] || (*slept)[2] != want[2] {
		t.Errorf("slept = %v, want %v", *slept, want)
	}
}

func TestSleepErrorAbortsRetry(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limit_exceeded","errorDescription":"slow down"}`))
	}))
	c.sleep = func(ctx context.Context, d time.Duration) error {
		return context.Canceled
	}

	_, err := c.do(context.Background(), http.MethodGet, "/things", nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}
