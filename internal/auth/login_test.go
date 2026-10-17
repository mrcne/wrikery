package auth

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/pkg/wrike"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return strconv.Itoa(port)
}

// exchangeServer answers the code exchange and records the code it got.
func exchangeServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var code string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "authorization_code" {
			t.Errorf("form = %v", r.PostForm)
		}
		code = r.PostForm.Get("code")
		_, _ = w.Write([]byte(`{"access_token":"at1","refresh_token":"rt1","token_type":"bearer","expires_in":"3600","host":"app-eu.wrike.com"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &code
}

func startTestLogin(t *testing.T, redirect string) (*Login, string, *string) {
	t.Helper()
	srv, code := exchangeServer(t)
	app := wrike.OAuthApp{ClientID: "id1", ClientSecret: "s3cret", RedirectURI: redirect}
	l, err := StartLogin(app, wrike.OAuth{App: app})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	l.TokenURL = srv.URL + "/oauth2/token"
	u, err := url.Parse(l.URL())
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("client_id") != "id1" || u.Query().Get("scope") != Scope || u.Query().Get("redirect_uri") != redirect {
		t.Errorf("authorize url = %s", l.URL())
	}
	return l, u.Query().Get("state"), code
}

func waitFor(l *Login) chan func() (Session, error) {
	ch := make(chan func() (Session, error), 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := l.Wait(ctx)
		ch <- func() (Session, error) { return s, err }
	}()
	return ch
}

func get(t *testing.T, rawURL string) (int, string) {
	t.Helper()
	resp, err := http.Get(rawURL) //nolint:gosec // the test's own loopback listener
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestLoginCompletesOnTheRightState(t *testing.T) {
	port := freePort(t)
	l, state, code := startTestLogin(t, "http://localhost:"+port+"/callback")
	done := waitFor(l)
	status, body := get(t, "http://127.0.0.1:"+port+"/callback?code=c0de&state="+state)
	if status != http.StatusOK || !strings.Contains(body, "signed in, you can close this tab") {
		t.Errorf("redirect answered %d %q", status, body)
	}
	s, err := (<-done)()
	if err != nil {
		t.Fatal(err)
	}
	if *code != "c0de" {
		t.Errorf("exchanged code = %q", *code)
	}
	if s.AccessToken != "at1" || s.RefreshToken != "rt1" || s.Host != "app-eu.wrike.com" || s.ClientID != "id1" || s.ClientSecret != "s3cret" {
		t.Errorf("session = %+v", s)
	}
	if left := time.Until(s.Expiry); left < 59*time.Minute || left > 61*time.Minute {
		t.Errorf("expiry in %s, want about an hour", left)
	}
}

func TestLoginRefusesAnotherStateAndGoesOn(t *testing.T) {
	port := freePort(t)
	l, state, _ := startTestLogin(t, "http://localhost:"+port+"/callback")
	done := waitFor(l)
	if status, _ := get(t, "http://127.0.0.1:"+port+"/callback?code=evil&state=other"); status != http.StatusBadRequest {
		t.Errorf("wrong state answered %d, want 400", status)
	}
	if status, _ := get(t, "http://127.0.0.1:"+port+"/other?code=c0de&state="+state); status != http.StatusNotFound {
		t.Errorf("another path answered %d, want 404", status)
	}
	get(t, "http://127.0.0.1:"+port+"/callback?code=c0de&state="+state)
	if s, err := (<-done)(); err != nil || s.AccessToken != "at1" {
		t.Fatalf("Wait = %+v, %v", s, err)
	}
}

func TestLoginReportsAccessDenied(t *testing.T) {
	port := freePort(t)
	l, state, _ := startTestLogin(t, "http://localhost:"+port+"/callback")
	done := waitFor(l)
	_, body := get(t, "http://127.0.0.1:"+port+"/callback?error=access_denied&state="+state)
	if !strings.Contains(body, "sign in failed") {
		t.Errorf("body = %q", body)
	}
	if _, err := (<-done)(); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("Wait err = %v, want the refusal", err)
	}
}

func TestLoginPasteCompletes(t *testing.T) {
	l, state, code := startTestLogin(t, "https://localhost/callback")
	done := waitFor(l)
	l.Paste("  https://localhost/callback?code=c0de&state=" + state + "\n")
	s, err := (<-done)()
	if err != nil || s.AccessToken != "at1" || *code != "c0de" {
		t.Fatalf("Wait = %+v, %v, code %q", s, err, *code)
	}
}

func TestLoginPasteWithAnotherStateIsAnError(t *testing.T) {
	l, _, _ := startTestLogin(t, "https://localhost/callback")
	done := waitFor(l)
	l.Paste("https://localhost/callback?code=c0de&state=other")
	if _, err := (<-done)(); err == nil {
		t.Fatal("a pasted answer with another state was accepted")
	}
}

func TestLoginWithoutAPortListensNowhere(t *testing.T) {
	l, _, _ := startTestLogin(t, "https://localhost/callback")
	if l.srv != nil {
		t.Fatal("a listener runs for a redirect without a port")
	}
}

func TestLoginFallsBackToPasteWhenThePortIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	l, state, _ := startTestLogin(t, "http://localhost:"+port+"/callback")
	if l.srv != nil {
		t.Fatal("a listener was reported on a port another program holds")
	}
	done := waitFor(l)
	l.Paste("http://localhost:" + port + "/callback?code=c0de&state=" + state)
	if s, err := (<-done)(); err != nil || s.AccessToken != "at1" {
		t.Fatalf("Wait = %+v, %v", s, err)
	}
}

func TestLoginCloseEndsTheWait(t *testing.T) {
	l, _, _ := startTestLogin(t, "https://localhost/callback")
	done := waitFor(l)
	l.Close()
	if _, err := (<-done)(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Wait err = %v, want ErrCancelled", err)
	}
}

func TestLoginWaitEndsWithTheContext(t *testing.T) {
	l, _, _ := startTestLogin(t, "https://localhost/callback")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := l.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait err = %v, want the deadline", err)
	}
}

func TestLoginAnswersTheRootWhenTheRedirectHasNoPath(t *testing.T) {
	port := freePort(t)
	l, state, _ := startTestLogin(t, "http://localhost:"+port)
	done := waitFor(l)
	status, body := get(t, "http://127.0.0.1:"+port+"/?code=c0de&state="+state)
	if status != http.StatusOK || !strings.Contains(body, "signed in") {
		t.Errorf("the root answered %d %q", status, body)
	}
	if s, err := (<-done)(); err != nil || s.AccessToken != "at1" {
		t.Fatalf("Wait = %+v, %v", s, err)
	}
}
