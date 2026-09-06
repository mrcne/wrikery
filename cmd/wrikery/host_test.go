package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// hostRouter sends a request to srv1 or srv2 depending on which host the client built its request for,
// so probeHost's per host lookups land on two independent test servers.
type hostRouter struct {
	srv1, srv2 *httptest.Server
}

func (h hostRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	target := h.srv1
	if req.URL.Hostname() == wrike.EUHost {
		target = h.srv2
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
	calls1, calls2 := 0, 0
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls1++
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls2++
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
	if calls1 != 1 || calls2 != 1 {
		t.Errorf("calls1=%d calls2=%d, want exactly one request to each host", calls1, calls2)
	}
}

func TestProbeHostStopsOnAuthErrorAndNeverTriesTheNextHost(t *testing.T) {
	calls2 := 0
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"not_authorized","errorDescription":"Token is invalid"}`))
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls2++
		_, _ = w.Write([]byte(`{"kind":"contacts","data":[]}`))
	}))
	defer srv2.Close()

	hc := &http.Client{Transport: hostRouter{srv1: srv1, srv2: srv2}}
	_, _, err := probeHost(context.Background(), "tok", apiHosts, hc)
	var apiErr *wrike.APIError
	if !errors.As(err, &apiErr) || !apiErr.IsAuth() {
		t.Fatalf("want an auth *APIError, got %v", err)
	}
	if calls2 != 0 {
		t.Errorf("calls2 = %d, want 0: the second host must never be tried", calls2)
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
