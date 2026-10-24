package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zalando/go-keyring"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

func TestHooksHaveNoSignInWithoutAClientID(t *testing.T) {
	a := &app{cfg: config.Config{}}
	if a.hooks().SignIn != nil {
		t.Error("a sign in hook without an app in the config")
	}
	a.cfg.OAuth.ClientID = "id1"
	if a.hooks().SignIn == nil {
		t.Error("no sign in hook although the config names an app")
	}
}

func TestSignInAdapterNeedsASecret(t *testing.T) {
	a := &app{cfg: config.Config{OAuth: config.OAuthConfig{ClientID: "id1", RedirectURI: "https://localhost/callback"}}}
	if _, err := a.signIn("  "); err == nil {
		t.Fatal("a sign in without a secret was started")
	}
}

// signInFixture is an app wired to a fake Wrike: the token endpoint, Me, and a free loopback port for the redirect.
type signInFixture struct {
	a    *app
	tk   auth.Tokens
	port string
}

func newSignInFixture(t *testing.T, cfg config.Config, exchangeDelay time.Duration) signInFixture {
	t.Helper()
	keyring.MockInit()
	st, err := store.Open(filepath.Join(t.TempDir(), "signin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := filepath.Join(t.TempDir(), "wrikery")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tk := auth.Tokens{TokenFile: filepath.Join(dir, "token"), SessionFile: filepath.Join(dir, "oauth.json"), LockFile: filepath.Join(dir, "auth.lock")}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			time.Sleep(exchangeDelay)
			_, _ = w.Write([]byte(`{"access_token":"at1","refresh_token":"rt1","token_type":"bearer","expires_in":"3600","host":"app-eu.wrike.com"}`))
		case "/api/v4/contacts":
			_, _ = w.Write([]byte(`{"kind":"contacts","data":[{"id":"C1","firstName":"Ann","lastName":"Kowalska","me":true}]}`))
		case "/api/v4/account":
			_, _ = w.Write([]byte(`{"kind":"accounts","data":[{"id":"ACC1","name":"Acme"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	cfg.PollInterval = time.Minute
	cfg.OAuth = config.OAuthConfig{ClientID: "id1", RedirectURI: "http://localhost:" + port + "/callback"}
	a := &app{cfg: cfg, st: st, tokens: tk, prog: tea.NewProgram(quietModel{}),
		hc: &http.Client{Transport: hostRouter{srv1: srv, srv2: srv}}, loginTokenURL: srv.URL + "/oauth2/token"}
	// The program never runs, so the event forwarder blocks on its first send until the program is killed.
	t.Cleanup(func() { a.shutdown(); a.prog.Kill() })
	return signInFixture{a: a, tk: tk, port: port}
}

// redirect plays the browser coming back with a code for the attempt.
func (f signInFixture) redirect(t *testing.T, si ui.SignIn) {
	t.Helper()
	u, err := url.Parse(si.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://127.0.0.1:" + f.port + "/callback?code=c0de&state=" + u.Query().Get("state")) //nolint:gosec // the test's own loopback listener
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func result(t *testing.T, si ui.SignIn) ui.SignInResult {
	t.Helper()
	select {
	case r := <-si.Result:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
		return ui.SignInResult{}
	}
}

func TestSignInAdapterSavesTheSessionAndStartsTheEngine(t *testing.T) {
	f := newSignInFixture(t, config.Config{}, 0)
	si, err := f.a.signIn("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !si.Listening {
		t.Error("the listener should run on the free port")
	}
	f.redirect(t, si)
	if r := result(t, si); r.Err != nil || r.Name != "Ann Kowalska" || r.App != "id1" {
		t.Fatalf("result = %+v", r)
	}
	stored, err := f.tk.Load()
	if err != nil || !stored.OAuth() || stored.Host != "app-eu.wrike.com" || stored.ClientSecret != "s3cret" {
		t.Errorf("stored = %+v, %v, want the session with the host and the app", stored, err)
	}
	if a := f.a; a.current() == nil {
		t.Error("no engine runs after the sign in")
	}
	// The settings screen reads the host from the meta table, the token probe writes it and so must the browser sign in.
	if host, _ := f.a.st.GetMeta(context.Background(), store.MetaKeyHost); host != "app-eu.wrike.com" {
		t.Errorf("meta host = %q, want the session's host", host)
	}
}

func TestSignInAdapterCancelStopsASaveInFlight(t *testing.T) {
	f := newSignInFixture(t, config.Config{}, 300*time.Millisecond)
	si, err := f.a.signIn("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	f.redirect(t, si)
	// Esc on the screen while the exchange runs: nothing may be stored and no engine may start behind the sign in box.
	si.Cancel()
	if r := result(t, si); r.Err == nil {
		t.Fatalf("result = %+v, want an error after the cancel", r)
	}
	if _, err := f.tk.Load(); !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("Load = %v, want nothing stored", err)
	}
	if f.a.current() != nil {
		t.Error("an engine runs after the cancel")
	}
}

func TestSignInAdapterRefusesAConfigHostThatDoesNotServeTheAccount(t *testing.T) {
	f := newSignInFixture(t, config.Config{Host: "www.wrike.com"}, 0)
	si, err := f.a.signIn("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	f.redirect(t, si)
	r := result(t, si)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "www.wrike.com") {
		t.Fatalf("result = %+v, want the config host named in the refusal", r)
	}
	if _, err := f.tk.Load(); !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("Load = %v, want nothing stored", err)
	}
}
