package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

type quietModel struct{}

func (quietModel) Init() tea.Cmd                       { return nil }
func (quietModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return quietModel{}, nil }
func (quietModel) View() string                        { return "" }

func TestStartEngineDoesNothingAfterShutdown(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "shutdown.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	a := &app{st: st, prog: tea.NewProgram(quietModel{})}
	a.shutdown()

	// verifyToken runs on a command goroutine, so a token can land after the program returned and the store is closing.
	a.startEngine(wrike.StaticToken("x"), wrike.DefaultHost)
	if e := a.current(); e != nil {
		t.Fatalf("an engine was started after shutdown: %#v", e)
	}
}

// A running engine would need the network, so the fields a real one leaves behind are set by hand.
func TestConcurrentShutdownDoesNotHangAndClearsTheEngine(t *testing.T) {
	a := &app{}
	cancelled, done := make(chan struct{}), make(chan struct{})
	a.cancel = func() { close(cancelled) }
	a.done = done
	go func() {
		<-cancelled
		close(done)
	}()

	returned := make(chan struct{}, 2)
	for range 2 {
		go func() {
			a.shutdown()
			returned <- struct{}{}
		}()
	}
	for range 2 {
		select {
		case <-returned:
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown did not return")
		}
	}
	select {
	case <-done:
	default:
		t.Error("shutdown returned before the engine goroutine finished")
	}
	if !a.closed || a.cancel != nil || a.done != nil {
		t.Errorf("shutdown left state behind: closed=%v cancel=%v done=%v", a.closed, a.cancel != nil, a.done != nil)
	}
}

// R in offline mode ends here: the engine starts the way a start without --offline starts it, on the stored session.
func TestGoOnlineStartsTheEngine(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "online.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	asked := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case asked <- r.URL.Path:
		default:
		}
		_, _ = w.Write([]byte(`{"kind":"contacts","data":[{"id":"C1","firstName":"Ann","lastName":"Kowalska","me":true}]}`))
	}))
	t.Cleanup(srv.Close)
	a := &app{cfg: config.Config{Host: wrike.EUHost, PollInterval: time.Minute}, st: st, prog: tea.NewProgram(quietModel{}),
		session: auth.Session{AccessToken: "tok"}, hc: &http.Client{Transport: hostRouter{srv1: srv, srv2: srv}}}
	// The program never runs, so the event forwarder blocks on its first send until the program is killed.
	t.Cleanup(func() { a.shutdown(); a.prog.Kill() })

	a.goOnline()
	if a.current() == nil {
		t.Fatal("going online started no engine")
	}
	select {
	case path := <-asked:
		if !strings.HasPrefix(path, "/api/v4/") {
			t.Errorf("the engine asked %s", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the engine never asked Wrike")
	}
}
