package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// routeTo sends every request to the test server and keeps the host the caller built it for in the Host header,
// so a handler can check which host a request was meant for.
type routeTo struct{ srv *httptest.Server }

func (r routeTo) RoundTrip(req *http.Request) (*http.Response, error) {
	dst, err := url.Parse(r.srv.URL)
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.Host = req.URL.Host
	out.URL.Scheme, out.URL.Host = dst.Scheme, dst.Host
	return http.DefaultTransport.RoundTrip(out)
}

// tokenEndpoint answers a refresh with the pair given, after checking the grant and the refresh token it got.
type tokenEndpoint struct {
	t       *testing.T
	calls   int
	refresh string
	status  int
	body    string
}

func (e *tokenEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/oauth2/token" {
		http.NotFound(w, r)
		return
	}
	e.calls++
	_ = r.ParseForm()
	if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != e.refresh {
		e.t.Errorf("refresh form = %v", r.PostForm)
	}
	if r.PostForm.Get("client_id") != "id1" || r.PostForm.Get("client_secret") != "s3cret" {
		e.t.Errorf("app in the form = %v", r.PostForm)
	}
	if r.Host != "app-eu.wrike.com" {
		e.t.Errorf("the refresh went to %q, want the session's host", r.Host)
	}
	w.WriteHeader(e.status)
	_, _ = w.Write([]byte(e.body))
}

func refreshServer(t *testing.T, status int) (*tokenEndpoint, *http.Client) {
	t.Helper()
	e := &tokenEndpoint{t: t, refresh: "rt1", status: status, body: `{"access_token":"at2","refresh_token":"rt2","token_type":"bearer","expires_in":"3600","host":"app-eu.wrike.com"}`}
	if status != http.StatusOK {
		e.body = `{"error":"invalid_grant","error_description":"refresh token is dead"}`
	}
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return e, &http.Client{Transport: routeTo{srv}}
}

func TestSourceReturnsAPermanentTokenAsItIs(t *testing.T) {
	src := NewSource(testTokens(t), Session{AccessToken: "perm"}, nil, "wrikery/test")
	for _, rejected := range []string{"", "perm"} {
		got, err := src.Token(context.Background(), rejected)
		if err != nil || got != "perm" {
			t.Errorf("Token(%q) = %q, %v, want perm", rejected, got, err)
		}
	}
}

func TestSourceReturnsAFreshTokenWithoutACall(t *testing.T) {
	keyring.MockInit()
	e, hc := refreshServer(t, http.StatusOK)
	src := NewSource(testTokens(t), oauthSession(time.Now().Add(time.Hour)), hc, "wrikery/test")
	got, err := src.Token(context.Background(), "")
	if err != nil || got != "at1" {
		t.Fatalf("Token = %q, %v", got, err)
	}
	if e.calls != 0 {
		t.Errorf("refresh calls = %d, want none for a token with an hour left", e.calls)
	}
}

func TestSourceRefreshesNearTheExpiryAndSavesFirst(t *testing.T) {
	keyring.MockInit()
	e, hc := refreshServer(t, http.StatusOK)
	tk := testTokens(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	src := NewSource(tk, oauthSession(now.Add(2*time.Minute)), hc, "wrikery/test")
	src.now = func() time.Time { return now }
	got, err := src.Token(context.Background(), "")
	if err != nil || got != "at2" {
		t.Fatalf("Token = %q, %v, want the renewed at2", got, err)
	}
	if e.calls != 1 {
		t.Errorf("refresh calls = %d, want one", e.calls)
	}
	stored, err := tk.Load()
	if err != nil || stored.AccessToken != "at2" || stored.RefreshToken != "rt2" {
		t.Fatalf("stored = %+v, %v, want the renewed pair in the keychain", stored, err)
	}
	if !stored.Expiry.Equal(now.Add(time.Hour)) || stored.Host != "app-eu.wrike.com" || stored.ClientID != "id1" || stored.ClientSecret != "s3cret" {
		t.Errorf("stored = %+v, want the expiry an hour on, the host and the app kept", stored)
	}
	if s := src.Session(); s.AccessToken != "at2" {
		t.Errorf("Session() = %+v, want the renewed one", s)
	}
}

func TestSourceAdoptsAPairAnotherSourceWrote(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	e, hc := refreshServer(t, http.StatusOK)
	tk := testTokens(t)
	near := time.Now().Add(2 * time.Minute)
	first := NewSource(tk, oauthSession(near), hc, "wrikery/test")
	second := NewSource(tk, oauthSession(near), hc, "wrikery/test")
	if got, err := first.Token(context.Background(), ""); err != nil || got != "at2" {
		t.Fatalf("first Token = %q, %v", got, err)
	}
	// The second process still holds the old pair. Its refresh token is dead now, the stored pair is what it must use.
	for _, rejected := range []string{"", "at1"} {
		got, err := second.Token(context.Background(), rejected)
		if err != nil || got != "at2" {
			t.Fatalf("second Token(%q) = %q, %v, want the adopted at2", rejected, got, err)
		}
	}
	if e.calls != 1 {
		t.Errorf("refresh calls = %d, want one, the second source adopts and does not refresh", e.calls)
	}
}

func TestSourceKeepsTheHeldTokenWhenAProactiveRefreshFails(t *testing.T) {
	keyring.MockInit()
	_, hc := refreshServer(t, http.StatusBadRequest)
	src := NewSource(testTokens(t), oauthSession(time.Now().Add(2*time.Minute)), hc, "wrikery/test")
	got, err := src.Token(context.Background(), "")
	if err != nil || got != "at1" {
		t.Fatalf("Token = %q, %v, want the held token, it is valid until its expiry", got, err)
	}
}

func TestSourceReturnsTheErrorWhenARefreshAfterARejectionFails(t *testing.T) {
	keyring.MockInit()
	_, hc := refreshServer(t, http.StatusBadRequest)
	src := NewSource(testTokens(t), oauthSession(time.Now().Add(time.Hour)), hc, "wrikery/test")
	_, err := src.Token(context.Background(), "at1")
	if err == nil || !strings.Contains(err.Error(), "refresh token is dead") {
		t.Fatalf("err = %v, want the refusal", err)
	}
}

func TestClientWithTheSourceRenewsARejectedToken(t *testing.T) {
	keyring.MockInit()
	tk := testTokens(t)
	var apiCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"at2","refresh_token":"rt2","token_type":"bearer","expires_in":3600,"host":"app-eu.wrike.com"}`))
		case "/api/v4/contacts":
			apiCalls = append(apiCalls, r.Header.Get("Authorization"))
			if r.Header.Get("Authorization") != "Bearer at2" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"not_authorized","errorDescription":"Access token is unknown or invalid"}`))
				return
			}
			_, _ = w.Write([]byte(`{"kind":"contacts","data":[{"id":"C1","firstName":"Ann","me":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: routeTo{srv}}
	// An hour left, so the renewal comes from the 401 alone.
	src := NewSource(tk, oauthSession(time.Now().Add(time.Hour)), hc, "wrikery/test")
	client := wrike.New("", wrike.WithTokenSource(src), wrike.WithBaseURL(wrike.BaseURL("app-eu.wrike.com")), wrike.WithHTTPClient(hc))
	me, err := client.Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.ID != "C1" || strings.Join(apiCalls, ",") != "Bearer at1,Bearer at2" {
		t.Errorf("me = %+v, calls = %v", me, apiCalls)
	}
	if stored, err := tk.Load(); err != nil || stored.AccessToken != "at2" {
		t.Errorf("stored = %+v, %v, want the renewed pair", stored, err)
	}
}
