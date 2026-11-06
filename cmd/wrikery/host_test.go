package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// hostRouter sends a request to the test server that stands for the host the client built its request for,
// so probeHost's per host lookups land on independent servers: srv2 is the EU host, srv3 the second US host
// and srv1 every other host, the second US host too while srv3 is nil.
type hostRouter struct {
	srv1, srv2, srv3 *httptest.Server
}

func (h hostRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	target := h.srv1
	switch req.URL.Hostname() {
	case wrike.EUHost:
		target = h.srv2
	case wrike.US2Host:
		if h.srv3 != nil {
			target = h.srv3
		}
	}
	dst, err := url.Parse(target.URL)
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = dst.Scheme, dst.Host
	out.Host = ""
	return http.DefaultTransport.RoundTrip(out)
}

func TestProbeHostFindsEUAfterDefaultAnswers300(t *testing.T) {
	var euCalls atomic.Int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		euCalls.Add(1)
		_, _ = w.Write([]byte(`{"kind":"contacts","data":[{"id":"C1","firstName":"Ann","me":true}]}`))
	}))
	defer srv2.Close()

	hc := &http.Client{Transport: hostRouter{srv1: srv1, srv2: srv2}}
	host, me, err := probeHost(context.Background(), "tok", apiHosts, hc)
	if err != nil {
		t.Fatal(err)
	}
	if host != wrike.EUHost {
		t.Errorf("host = %q, want %q", host, wrike.EUHost)
	}
	if me.ID != "C1" {
		t.Errorf("me = %+v, want ID C1", me)
	}
	// The hosts are asked at once and the first acceptance cancels the rest, so only the EU host's count is certain.
	if n := euCalls.Load(); n != 1 {
		t.Errorf("the EU host was asked %d times, want once", n)
	}
}

// A host that never answers must not cost the others their turn.
func TestProbeHostTakesTheHostThatAnswersWhileAnotherHangs(t *testing.T) {
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hung.Close()
	eu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"kind":"contacts","data":[{"id":"C1","firstName":"Ann","me":true}]}`))
	}))
	defer eu.Close()

	hc := &http.Client{Transport: hostRouter{srv1: hung, srv2: eu}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	host, _, err := probeHost(ctx, "tok", apiHosts, hc)
	if err != nil || host != wrike.EUHost {
		t.Fatalf("host = %q, %v, want the EU host", host, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the answer took %s, the hanging host was waited for", time.Since(start))
	}
}

// Wrike answers 300, not 401, to a valid token of another data center, so a 401 anywhere is about the token,
// whatever the other hosts say, and the verdict waits for them.
func TestProbeHostReportsTheTokenRejectedWhenOneHostAnswers401(t *testing.T) {
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"not_authorized","errorDescription":"Token is invalid"}`))
	}))
	defer rejecting.Close()
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hung.Close()

	hc := &http.Client{Transport: hostRouter{srv1: rejecting, srv2: hung}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := probeHost(ctx, "tok", apiHosts, hc)
	var apiErr *wrike.APIError
	if !errors.As(err, &apiErr) || !apiErr.IsAuth() {
		t.Fatalf("want an auth *APIError, got %v", err)
	}
}

func TestProbeHostReturnsErrNoDataCenterWhenEveryHostAnswers300(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer srv2.Close()

	hc := &http.Client{Transport: hostRouter{srv1: srv1, srv2: srv2}}
	_, _, err := probeHost(context.Background(), "tok", apiHosts, hc)
	if !errors.Is(err, errNoDataCenter) {
		t.Fatalf("want errNoDataCenter, got %v", err)
	}
}

// No acceptance and not every host answered 300: the error names each host with what it answered, in the order of the list.
func TestProbeHostNamesEveryHostWhenNoneAccepts(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked by policy", http.StatusForbidden)
	}))
	defer blocked.Close()
	eu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer eu.Close()

	hc := &http.Client{Transport: hostRouter{srv1: blocked, srv2: eu}}
	_, _, err := probeHost(context.Background(), "tok", apiHosts, hc)
	var apiErr *wrike.APIError
	if err == nil || errors.Is(err, errNoDataCenter) || errors.As(err, &apiErr) {
		t.Fatalf("want the probe's own error, got %v", err)
	}
	text := err.Error()
	for _, want := range []string{"www.wrike.com: http 403", "app-eu.wrike.com: answered 300, another data center", "app-us2.wrike.com: http 403"} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q should contain %q", text, want)
		}
	}
	if strings.Index(text, "www.wrike.com") > strings.Index(text, "app-eu.wrike.com") || strings.Index(text, "app-eu.wrike.com") > strings.Index(text, "app-us2.wrike.com") {
		t.Errorf("the hosts should come in the order of the list: %q", text)
	}
}

func TestResolveHostPrefersTheSessionHost(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hosts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	session := auth.Session{AccessToken: "at", RefreshToken: "rt", Host: wrike.EUHost}
	// No probe runs for a browser session, the token answer named the host, so no fake server is needed.
	host, err := resolveHost(context.Background(), config.Config{}, st, session, nil)
	if err != nil || host != wrike.EUHost {
		t.Fatalf("host = %q, %v, want the session's host", host, err)
	}
	host, err = resolveHost(context.Background(), config.Config{Host: "x.wrike.com"}, st, session, nil)
	if err != nil || host != "x.wrike.com" {
		t.Fatalf("host = %q, %v, want the config to win", host, err)
	}
}
