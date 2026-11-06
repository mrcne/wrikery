package main

import (
	"context"
	"errors"
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
	"github.com/mrcne/wrikery/internal/ui"
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

// An offline start sends nothing, but the rows a process that died mid-send left in flight come back to the queue.
func TestOfflineStartPutsTheRowsInFlightBack(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []store.Task{{ID: "T1", Title: "a", Status: "Active"}}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "stranded")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	a := &app{st: st, lock: filepath.Join(t.TempDir(), "sync.lock")}
	a.startOffline()
	if row, err := st.Outbox().Get(ctx, id); err != nil || row.State != store.StatePending {
		t.Errorf("row = %+v, %v, want it back in the queue", row, err)
	}
}

// recorder is a program model that hands every message to the test, the window size ones aside.
type recorder struct{ got chan tea.Msg }

func (r recorder) Init() tea.Cmd { return nil }
func (r recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.WindowSizeMsg); !ok {
		r.got <- msg
	}
	return r, nil
}
func (r recorder) View() string { return "" }

// An answer about the token opens the sign in screen, no answer is offline mode with the reason.
func TestStartFailureTellsTheTokenAnswersFromTheNetwork(t *testing.T) {
	for _, err := range []error{&wrike.APIError{StatusCode: http.StatusUnauthorized}, errNoDataCenter} {
		if msg := startFailure(err); msg != (ui.SyncStateMsg{State: "auth_required"}) {
			t.Errorf("startFailure(%v) = %#v, want the sign in screen", err, msg)
		}
	}
	err := &probeError{hosts: []string{"x"}, errs: map[string]error{"x": errors.New("i/o timeout")}}
	off, ok := startFailure(err).(ui.WentOfflineMsg)
	if !ok || !strings.Contains(off.Reason, "x: i/o timeout") {
		t.Errorf("startFailure(%v) = %#v, want offline mode with the reason", err, startFailure(err))
	}
}

// Nothing stored and no answer: the interface goes offline with the reason, no engine runs and nothing talks to www.wrike.com.
func TestGoOnlineWithoutAHostGoesOfflineWhenNothingAnswers(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "nohost.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	got := make(chan tea.Msg, 4)
	a := &app{cfg: config.Config{PollInterval: time.Minute}, st: st, prog: tea.NewProgram(recorder{got}, tea.WithInput(nil), tea.WithoutRenderer()),
		session: auth.Session{AccessToken: "tok"}, hc: &http.Client{Transport: hostRouter{srv1: down, srv2: down}}}
	go func() { _, _ = a.prog.Run() }()
	t.Cleanup(func() { a.shutdown(); a.prog.Kill() })

	a.goOnline()
	select {
	case msg := <-got:
		// The five second deadline cuts the client's retries short, the reason must still say what the host did.
		off, ok := msg.(ui.WentOfflineMsg)
		if !ok || !strings.Contains(off.Reason, "www.wrike.com: dial tcp") || !strings.Contains(off.Reason, "connection refused") {
			t.Fatalf("got %#v, want offline mode naming the hosts and their refusal", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the interface was not told")
	}
	if a.current() != nil {
		t.Fatal("an engine started on a host nobody found")
	}
}
