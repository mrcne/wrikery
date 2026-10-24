package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zalando/go-keyring"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// movedAccount is the account C1 of Acme after its move: the US host answers 300 for it, the EU host answers.
func movedAccount(t *testing.T) *http.Client {
	t.Helper()
	us := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	t.Cleanup(us.Close)
	eu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/contacts":
			_, _ = w.Write([]byte(`{"kind":"contacts","data":[{"id":"C1","firstName":"Ann","lastName":"Kowalska","me":true}]}`))
		case "/api/v4/account":
			_, _ = w.Write([]byte(`{"kind":"accounts","data":[{"id":"ACC1","name":"Acme"}]}`))
		default:
			_, _ = w.Write([]byte(`{"kind":"folders","data":[]}`))
		}
	}))
	t.Cleanup(eu.Close)
	return &http.Client{Transport: hostRouter{srv1: us, srv2: eu}}
}

// switchApp is an app whose cache was built from Acme on the US host, with one comment queued offline.
func switchApp(t *testing.T) (*app, auth.Tokens) {
	t.Helper()
	keyring.MockInit()
	st, err := store.Open(filepath.Join(t.TempDir(), "switch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.AdoptHome(ctx, store.Home{AccountID: "ACC1", AccountName: "Acme", Host: wrike.DefaultHost, ContactID: "C1"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "wrikery")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tk := auth.Tokens{TokenFile: filepath.Join(dir, "token"), SessionFile: filepath.Join(dir, "oauth.json"), LockFile: filepath.Join(dir, "auth.lock")}
	a := &app{cfg: config.Config{PollInterval: time.Minute}, st: st, tokens: tk, prog: tea.NewProgram(quietModel{}),
		hc: movedAccount(t), dbFile: "/home/ann/.local/share/wrikery/wrike.db"}
	// The program never runs, so the event forwarder blocks on its first send until the program is killed.
	t.Cleanup(func() { a.shutdown(); a.prog.Kill() })
	return a, tk
}

func TestFinishAsksBeforeStoringOnAnotherHost(t *testing.T) {
	a, tk := switchApp(t)
	_, err := a.verifyToken(context.Background(), "new-token")
	var sw *ui.SwitchNeeded
	if !errors.As(err, &sw) {
		t.Fatalf("verifyToken = %v, want the question", err)
	}
	if sw.From != "Acme on www.wrike.com" || sw.To != "Acme on app-eu.wrike.com" || sw.DBFile != "/home/ann/.local/share/wrikery/wrike.db" {
		t.Errorf("box = %+v", sw)
	}
	if _, err := tk.Load(); !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("a token was stored before the answer: %v", err)
	}
	if a.current() != nil {
		t.Error("an engine started before the answer")
	}
	if h, _ := a.st.Home(context.Background()); h.Host != wrike.DefaultHost {
		t.Errorf("the record moved before the answer: %+v", h)
	}
	name, err := sw.Confirm()
	if err != nil || name != "Ann Kowalska" {
		t.Fatalf("confirm = %q, %v", name, err)
	}
	if s, err := tk.Load(); err != nil || s.AccessToken != "new-token" {
		t.Errorf("stored = %+v, %v", s, err)
	}
	if a.current() == nil {
		t.Error("no engine after the yes")
	}
	h, _ := a.st.Home(context.Background())
	pending, _ := a.st.SwitchPending(context.Background())
	if h.Host != wrike.EUHost || !pending {
		t.Errorf("record = %+v, check pending %v, want the EU host and the queue check due", h, pending)
	}
}

// Esc on the sign in screen cancels the sign in's context, the box comes later and its yes must not depend on it.
func TestConfirmRunsAfterTheSignInContextEnded(t *testing.T) {
	a, tk := switchApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := a.verifyToken(ctx, "new-token")
	cancel()
	var sw *ui.SwitchNeeded
	if !errors.As(err, &sw) {
		t.Fatalf("verifyToken = %v", err)
	}
	if _, err := sw.Confirm(); err != nil {
		t.Fatalf("confirm after the sign in ended = %v", err)
	}
	if _, err := tk.Load(); err != nil {
		t.Errorf("nothing stored: %v", err)
	}
}

// WRIKERY_TOKEN went through no sign in, the host it is served on is asked for, not taken from the cache.
func TestEnvTokenProbesTheHost(t *testing.T) {
	a, _ := switchApp(t)
	if err := a.st.SetMeta(context.Background(), store.MetaKeyHost, wrike.DefaultHost); err != nil {
		t.Fatal(err)
	}
	a.tokenFromEnv, a.session = true, auth.Session{AccessToken: "env-token"}
	host, err := a.engineHost(context.Background())
	if err != nil || host != wrike.EUHost {
		t.Errorf("host = %q, %v, want the EU host the token is served on", host, err)
	}
}
