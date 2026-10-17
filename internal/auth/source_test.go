package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	delay   time.Duration // how long an answer takes
	before  func()        // runs before the answer, the other process at work
}

func (e *tokenEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/oauth2/token" {
		http.NotFound(w, r)
		return
	}
	e.calls++
	if e.before != nil {
		e.before()
	}
	time.Sleep(e.delay)
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

// storedSource builds a source whose session is also in the store, the way a sign in leaves it.
// A renewal treats the store as the authority, so a session held in memory alone reads as a sign out.
func storedSource(t *testing.T, tk Tokens, s Session, hc *http.Client) *Source {
	t.Helper()
	if s.OAuth() {
		if err := tk.SaveSession(s); err != nil {
			t.Fatal(err)
		}
	}
	return NewSource(tk, s, hc, "wrikery/test")
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
	src := storedSource(t, testTokens(t), oauthSession(time.Now().Add(time.Hour)), hc)
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
	src := storedSource(t, tk, oauthSession(now.Add(2*time.Minute)), hc)
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
}

func TestSourceAdoptsAPairAnotherSourceWrote(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	e, hc := refreshServer(t, http.StatusOK)
	tk := testTokens(t)
	near := time.Now().Add(2 * time.Minute)
	first := storedSource(t, tk, oauthSession(near), hc)
	second := storedSource(t, tk, oauthSession(near), hc)
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
	src := storedSource(t, testTokens(t), oauthSession(time.Now().Add(2*time.Minute)), hc)
	got, err := src.Token(context.Background(), "")
	if err != nil || got != "at1" {
		t.Fatalf("Token = %q, %v, want the held token, it is valid until its expiry", got, err)
	}
}

func TestSourceReturnsTheErrorWhenARefreshAfterARejectionFails(t *testing.T) {
	keyring.MockInit()
	_, hc := refreshServer(t, http.StatusBadRequest)
	src := storedSource(t, testTokens(t), oauthSession(time.Now().Add(time.Hour)), hc)
	_, err := src.Token(context.Background(), "at1")
	if err == nil || !strings.Contains(err.Error(), "refresh token is dead") {
		t.Fatalf("err = %v, want the refusal", err)
	}
	if !errors.Is(err, wrike.ErrRenewalRefused) {
		t.Errorf("err = %v, want the refusal marked, so the client lets the 401 stand", err)
	}
}

func TestSourceReturnsANetworkFailureWithoutTheRefusalMark(t *testing.T) {
	keyring.MockInit()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	hc := &http.Client{Transport: routeTo{srv}}
	src := storedSource(t, testTokens(t), oauthSession(time.Now().Add(time.Hour)), hc)
	_, err := src.Token(context.Background(), "at1")
	if err == nil || errors.Is(err, wrike.ErrRenewalRefused) {
		t.Fatalf("err = %v, want a plain failure, the session may be fine", err)
	}
}

func TestSourceDoesNotRenewAfterASignOut(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	e, hc := refreshServer(t, http.StatusOK)
	tk := testTokens(t)
	src := storedSource(t, tk, oauthSession(time.Now().Add(2*time.Minute)), hc)
	if err := tk.Delete(); err != nil {
		t.Fatal(err)
	}
	// The held token is valid until its expiry, after that the sign in screen is the right place.
	if got, err := src.Token(context.Background(), ""); err != nil || got != "at1" {
		t.Fatalf("proactive Token = %q, %v, want the held token", got, err)
	}
	_, err := src.Token(context.Background(), "at1")
	if !errors.Is(err, ErrSignedOut) || !errors.Is(err, wrike.ErrRenewalRefused) {
		t.Fatalf("Token after a rejection = %v, want ErrSignedOut marked as a refusal", err)
	}
	if e.calls != 0 {
		t.Errorf("refresh calls = %d, want none after a sign out", e.calls)
	}
	if _, err := tk.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Load = %v, want nothing stored, a renewal must not undo a sign out", err)
	}
}

func TestSourceDoesNotRenewWhenATokenReplacedTheSession(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	e, hc := refreshServer(t, http.StatusOK)
	tk := testTokens(t)
	src := storedSource(t, tk, oauthSession(time.Now().Add(2*time.Minute)), hc)
	if err := tk.SaveToken("perm"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Token(context.Background(), "at1"); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("Token = %v, want ErrSignedOut", err)
	}
	if stored, err := tk.Load(); err != nil || stored.OAuth() || stored.AccessToken != "perm" || e.calls != 0 {
		t.Fatalf("stored = %+v, %v, calls %d, want the token kept and no refresh", stored, err, e.calls)
	}
}

func TestSourceFinishesARefreshAfterTheContextIsCancelled(t *testing.T) {
	keyring.MockInit()
	e, hc := refreshServer(t, http.StatusOK)
	e.delay = 200 * time.Millisecond
	tk := testTokens(t)
	src := storedSource(t, tk, oauthSession(time.Now().Add(2*time.Minute)), hc)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	// Wrike rotates the pair as it answers, a refresh that is cut short loses the only live refresh token.
	if got, err := src.Token(ctx, ""); err != nil || got != "at2" {
		t.Fatalf("Token = %q, %v, want the refresh to finish", got, err)
	}
	if stored, err := tk.Load(); err != nil || stored.RefreshToken != "rt2" {
		t.Errorf("stored = %+v, %v, want the rotated pair", stored, err)
	}
}

func TestSourceKeepsARefreshedPairWhenTheSaveFails(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	e, hc := refreshServer(t, http.StatusOK)
	tk := testTokens(t)
	src := storedSource(t, tk, oauthSession(time.Now().Add(2*time.Minute)), hc)
	// The lock file exists already, so only the session write runs into the read-only directory.
	if err := os.WriteFile(tk.LockFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(tk.SessionFile)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if got, err := src.Token(context.Background(), ""); err != nil || got != "at2" {
		t.Fatalf("Token = %q, %v, want the new pair in memory although it could not be stored", got, err)
	}
	if got, err := src.Token(context.Background(), ""); err != nil || got != "at2" || e.calls != 1 {
		t.Fatalf("second Token = %q, %v, calls %d, want the held pair without another refresh", got, err, e.calls)
	}
}

func TestSourceAdoptsAPairSavedDuringAFailedRefresh(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	e, hc := refreshServer(t, http.StatusBadRequest)
	tk := testTokens(t)
	other := oauthSession(time.Now().Add(time.Hour))
	other.AccessToken, other.RefreshToken = "at9", "rt9"
	// Without the lock the other process got its refresh in first, and the stored pair is the live one.
	e.before = func() {
		if err := tk.SaveSession(other); err != nil {
			t.Error(err)
		}
	}
	src := storedSource(t, tk, oauthSession(time.Now().Add(time.Hour)), hc)
	if got, err := src.Token(context.Background(), "at1"); err != nil || got != "at9" {
		t.Fatalf("Token = %q, %v, want the pair the other process saved", got, err)
	}
}

func TestSourceWaitsAMinuteAfterAFailedProactiveRenewal(t *testing.T) {
	keyring.MockInit()
	e, hc := refreshServer(t, http.StatusBadRequest)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	src := storedSource(t, testTokens(t), oauthSession(now.Add(2*time.Minute)), hc)
	src.now = func() time.Time { return now }
	for range 3 {
		if got, err := src.Token(context.Background(), ""); err != nil || got != "at1" {
			t.Fatalf("Token = %q, %v", got, err)
		}
	}
	if e.calls != 1 {
		t.Errorf("refresh calls = %d, want one, a failed renewal is not tried again at once", e.calls)
	}
	now = now.Add(61 * time.Second)
	if _, err := src.Token(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if e.calls != 2 {
		t.Errorf("refresh calls = %d, want a second try after a minute", e.calls)
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
	src := storedSource(t, tk, oauthSession(time.Now().Add(time.Hour)), hc)
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

func TestSourceRefreshesWithAnAdoptedPairWhenItIsStaleToo(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	e, hc := refreshServer(t, http.StatusOK)
	tk := testTokens(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	first := storedSource(t, tk, oauthSession(base.Add(2*time.Minute)), hc)
	first.now = func() time.Time { return base }
	// The second process started from the same stored pair and then slept through most of the hour.
	second := storedSource(t, tk, oauthSession(base.Add(2*time.Minute)), hc)
	second.now = func() time.Time { return base.Add(58 * time.Minute) }
	if got, err := first.Token(context.Background(), ""); err != nil || got != "at2" {
		t.Fatalf("first Token = %q, %v", got, err)
	}
	// Its own refresh token is dead, and the stored pair is about to expire as well, so the refresh must run with that pair's token.
	e.refresh = "rt2"
	e.body = `{"access_token":"at3","refresh_token":"rt3","token_type":"bearer","expires_in":"3600","host":"app-eu.wrike.com"}`
	got, err := second.Token(context.Background(), "")
	if err != nil || got != "at3" {
		t.Fatalf("second Token = %q, %v, want a pair refreshed from the stored one", got, err)
	}
	if e.calls != 2 {
		t.Errorf("refresh calls = %d, want two", e.calls)
	}
	if stored, err := tk.Load(); err != nil || stored.AccessToken != "at3" || stored.RefreshToken != "rt3" {
		t.Errorf("stored = %+v, %v", stored, err)
	}
}
