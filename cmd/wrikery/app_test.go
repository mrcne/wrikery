package main

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
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
	a.startEngine("x")
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
