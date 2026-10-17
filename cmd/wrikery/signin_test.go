package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zalando/go-keyring"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
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

func TestSignInAdapterSavesTheSessionAndStartsTheEngine(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"access_token":"at1","refresh_token":"rt1","token_type":"bearer","expires_in":"3600","host":"app-eu.wrike.com"}`))
		case "/api/v4/contacts":
			_, _ = w.Write([]byte(`{"kind":"contacts","data":[{"id":"C1","firstName":"Ann","lastName":"Kowalska","me":true}]}`))
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
	cfg := config.Config{PollInterval: time.Minute, OAuth: config.OAuthConfig{ClientID: "id1", RedirectURI: "http://localhost:" + port + "/callback"}}
	a := &app{cfg: cfg, st: st, tokens: tk, prog: tea.NewProgram(quietModel{}),
		hc: &http.Client{Transport: hostRouter{srv1: srv, srv2: srv}}, loginTokenURL: srv.URL + "/oauth2/token"}
	// The program never runs, so the event forwarder blocks on its first send until the program is killed.
	t.Cleanup(func() { a.shutdown(); a.prog.Kill() })

	si, err := a.signIn("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(si.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://127.0.0.1:" + port + "/callback?code=c0de&state=" + u.Query().Get("state")) //nolint:gosec // the test's own loopback listener
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	select {
	case r := <-si.Result:
		if r.Err != nil || r.Name != "Ann Kowalska" {
			t.Fatalf("result = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	stored, err := tk.Load()
	if err != nil || !stored.OAuth() || stored.Host != "app-eu.wrike.com" || stored.ClientSecret != "s3cret" {
		t.Errorf("stored = %+v, %v, want the session with the host and the app", stored, err)
	}
	if a.current() == nil {
		t.Error("no engine runs after the sign in")
	}
}
