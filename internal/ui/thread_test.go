package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// A pick from the search or a jump lands on a task whose detail is still to load, so enter cannot ask for the thread yet.
// The ask follows the load at once instead of waiting for the rest, and a jump away and back asks again.
func TestEnterOnATaskNotLoadedYetAsksWhenItsDetailLands(t *testing.T) {
	var asked []string
	m := New(Options{Config: config.Config{UI: config.UIConfig{Theme: "dark", ASCII: true}},
		Hooks: Hooks{FetchThread: func(ctx context.Context, id string) (bool, error) { asked = append(asked, id); return true, nil }}})
	land := func(id string) {
		t.Helper()
		next, _ := m.jumpToTask(id, nil, true)
		m = next.(Model)
		next, cmd := m.Update(taskLoadedMsg{asked: id, task: store.Task{ID: id, Title: "picked", UpdatedDate: "2026-09-03T10:00:00Z"}})
		m = next.(Model)
		if cmd == nil {
			t.Fatalf("no command after the load of %s", id)
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("the command after the load of %s waited, a rest timer instead of the fetch", id)
		}
	}
	land("T1")
	land("T2")
	land("T1")
	if len(asked) != 3 || asked[0] != "T1" || asked[1] != "T2" || asked[2] != "T1" {
		t.Errorf("asked = %v, want T1, T2 and T1 again after the jump back", asked)
	}
}

// In offline mode and while the account question is open nothing is sent, and the task must not count as asked,
// or it stays unasked when the state clears.
func TestAskThreadMarksOnlyWhatItSends(t *testing.T) {
	var asked []string
	m := New(Options{Offline: true, Config: config.Config{UI: config.UIConfig{Theme: "dark", ASCII: true}},
		Hooks: Hooks{FetchThread: func(ctx context.Context, id string) (bool, error) { asked = append(asked, id); return true, nil }}})
	m.selectedTaskID = "T1"
	m.detail.set(taskLoadedMsg{task: store.Task{ID: "T1", Title: "offline", UpdatedDate: "2026-09-03T10:00:00Z"}})
	if cmd := m.askThread("T1"); cmd != nil || m.threadAsked != "" {
		t.Errorf("offline: cmd %v, asked %q, want nothing sent and nothing marked", cmd != nil, m.threadAsked)
	}
	m.opts.Offline = false
	cmd := m.askThread("T1")
	if cmd == nil || m.threadAsked != "T1" {
		t.Fatalf("online: cmd %v, asked %q, want the fetch and the mark", cmd != nil, m.threadAsked)
	}
	// The command answers with the outcome, a failure clears the mark so the next rest or enter asks again.
	next, _ := m.Update(threadFetchedMsg{id: "T1", err: context.DeadlineExceeded})
	if m = next.(Model); m.threadAsked != "" {
		t.Errorf("after a failed fetch asked = %q, want cleared", m.threadAsked)
	}
	if cmd := m.askThread("T1"); cmd == nil {
		t.Error("the same selection must ask again after a failure")
	}
}
