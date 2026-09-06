package ui_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

var fixedNow = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func seededStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := demo.Seed(context.Background(), st, fixedNow); err != nil {
		t.Fatal(err)
	}
	return st
}

func testOptions(st *store.Store) ui.Options {
	return ui.Options{
		Version: "test", Store: st, Demo: true,
		Config: config.UIConfig{Theme: "dark", ASCII: true, BranchTemplate: "{id}-{slug}"},
		Now:    func() time.Time { return fixedNow },
		Hooks:  ui.Hooks{Refresh: func() {}, WakeOutbox: func() {}},
	}
}

func press(tm *teatest.TestModel, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		case "esc":
			tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		case "tab":
			tm.Send(tea.KeyMsg{Type: tea.KeyTab})
		case "shift+tab":
			tm.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
		case "space":
			tm.Send(tea.KeyMsg{Type: tea.KeySpace})
		case "ctrl+f":
			tm.Send(tea.KeyMsg{Type: tea.KeyCtrlF})
		default:
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
}

// Reading teatest's output drains it, and bubbletea only repaints the lines that changed.
// Two waits for text drawn in the same frame would then see only the first one, so every model keeps the frames it has produced so far and each wait searches that whole history.
var frames sync.Map // *teatest.TestModel -> *bytes.Buffer

func seenOutput(t *testing.T, tm *teatest.TestModel) *bytes.Buffer {
	t.Helper()
	v, loaded := frames.LoadOrStore(tm, &bytes.Buffer{})
	if !loaded {
		t.Cleanup(func() { frames.Delete(tm) })
	}
	return v.(*bytes.Buffer)
}

func waitFor(t *testing.T, tm *teatest.TestModel, want string) {
	t.Helper()
	waitAfter(t, tm, 0, want)
}

// mark reads what has been drawn so far and returns its length.
// Text that was already on screen once, such as a pane title the first run box covered, needs it to prove the frame was drawn again.
func mark(t *testing.T, tm *teatest.TestModel) int {
	t.Helper()
	seen := seenOutput(t, tm)
	if _, err := io.Copy(seen, tm.Output()); err != nil {
		t.Fatalf("reading the program output: %v", err)
	}
	return seen.Len()
}

func waitAfter(t *testing.T, tm *teatest.TestModel, from int, want string) {
	t.Helper()
	seen := seenOutput(t, tm)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := io.Copy(seen, tm.Output()); err != nil {
			t.Fatalf("reading the program output: %v", err)
		}
		if strings.Contains(seen.String()[from:], want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %q, output so far:\n%s", want, seen.String()[from:])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// finalView quits the program and returns the last full frame, without cursor sequences, for goldens.
func finalView(t *testing.T, tm *teatest.TestModel) string {
	t.Helper()
	press(tm, "q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
	return tm.FinalModel(t).(ui.Model).View()
}

func TestShellGoldenAtThreeWidths(t *testing.T) {
	for _, w := range []int{160, 100, 70} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			st := seededStore(t)
			tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(w, 30))
			waitFor(t, tm, "Tasks")
			golden.RequireEqual(t, []byte(finalView(t, tm)))
		})
	}
}

func TestSidebarShowsFollowedSpaces(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 30))
	waitFor(t, tm, "Platform")
	waitFor(t, tm, "Mobile")
	view := finalView(t, tm)
	if !strings.Contains(view, "Design system") {
		t.Errorf("sidebar should list Design system under Platform:\n%s", view)
	}
}

func TestTabMovesFocusAndWindowSlides(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(100, 30))
	waitFor(t, tm, "Tasks")
	press(tm, "tab") // list -> detail, at 100 columns the sidebar leaves the window
	view := finalView(t, tm)
	// Only the detail pane draws the comment divider, so it stands for that pane being on screen.
	if strings.Contains(view, "Spaces") || !strings.Contains(view, "-- Comments (") {
		t.Errorf("after tab at 100 cols the window should show list+detail:\n%s", view)
	}
}

func TestDetailShowsTheSelectedTask(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "Tasks: My tasks")
	// The detail follows the list cursor.
	// The demo task with a queued comment sits far down the list, so the filter is the short way to it.
	press(tm, "/")
	press(tm, "fix auth")
	waitFor(t, tm, "#1200000")
	waitFor(t, tm, "Comments (")
	waitFor(t, tm, "Queued while offline.") // the comment the demo data leaves in the outbox
	press(tm, "enter")                      // leave the filter input, the filter itself stays
	press(tm, "tab")                        // focus the detail pane
	press(tm, "j", "j", "j")
	view := finalView(t, tm)
	for _, want := range []string{"#1200000", "Fix auth retry loop", "Time ("} {
		if !strings.Contains(view, want) {
			t.Errorf("after scrolling the detail should still show %q:\n%s", want, view)
		}
	}
}

// Opening a task tells the syncer to refresh its thread, so it must follow a deliberate enter and not the cursor.
func TestOpeningATaskMarksItOpened(t *testing.T) {
	st := seededStore(t)
	ctx := context.Background()
	tasks, err := st.Tasks().ListForResponsible(ctx, demo.MeID)
	if err != nil {
		t.Fatal(err)
	}
	previewed, opened := tasks[0], tasks[1]
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "Tasks: My tasks")
	press(tm, "j")     // move the cursor onto the second task, which only previews it
	press(tm, "enter") // and open that one
	deadline := time.Now().Add(3 * time.Second)
	for {
		t2, err := st.Tasks().Get(ctx, opened.ID)
		if err != nil {
			t.Fatal(err)
		}
		if t2.LastOpenedAt != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("enter should have marked %s opened", opened.ID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = finalView(t, tm)
	t1, err := st.Tasks().Get(ctx, previewed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if t1.LastOpenedAt != "" {
		t.Errorf("the cursor passing over %s should not open it, last opened %q", previewed.ID, t1.LastOpenedAt)
	}
}

func TestStatusBarReactsToEngineMessages(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Tasks")
	tm.Send(ui.OutboxChangedMsg{Pending: 2, Failed: 1})
	waitFor(t, tm, "2 pending")
	waitFor(t, tm, "1 failed")
	tm.Send(ui.SyncStateMsg{State: "offline"})
	waitFor(t, tm, "offline since")
}

// The short hints are in the goldens, the overlay is not, and bubbles reaches for a bullet and an ellipsis of its own.
func TestHelpOverlayStaysASCII(t *testing.T) {
	for _, w := range []int{160, 120, 70} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			st := seededStore(t)
			tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(w, 30))
			waitFor(t, tm, "Tasks")
			press(tm, "?")
			waitFor(t, tm, "esc or ? to close")
			for _, r := range seenOutput(t, tm).String() {
				if r > 127 {
					t.Fatalf("ASCII mode drew %q", r)
				}
			}
			press(tm, "esc")
			_ = finalView(t, tm)
		})
	}
}

func TestTaskListFiltersAndFollowsTheSidebar(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "Tasks: My tasks")
	from := mark(t, tm)
	press(tm, "/")
	press(tm, "a", "u", "t", "h")
	waitAfter(t, tm, from, "Tasks: My tasks (")
	if n := taskListTitleCount(t, seenOutput(t, tm).String()[from:]); n >= 30 {
		t.Fatalf("filtering by 'auth' should narrow the list below 30, title count is %d", n)
	}
	press(tm, "esc")
	press(tm, "shift+tab")
	press(tm, "j")
	waitFor(t, tm, "Tasks: Mobile")
}

// taskListTitleCount reads the "(N)" count off the last "Tasks: My tasks (" title drawn in the given output.
func taskListTitleCount(t *testing.T, output string) int {
	t.Helper()
	idx := strings.LastIndex(output, "Tasks: My tasks (")
	if idx < 0 {
		t.Fatalf("no task list title found in:\n%s", output)
	}
	rest := output[idx+len("Tasks: My tasks ("):]
	end := strings.IndexByte(rest, ')')
	if end < 0 {
		t.Fatalf("unterminated task list title in:\n%s", output)
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		t.Fatalf("task list title count %q: %v", rest[:end], err)
	}
	return n
}

func TestHelpOverlayListsBindings(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Tasks")
	press(tm, "?")
	waitFor(t, tm, "sync issues")
	press(tm, "esc")
	view := finalView(t, tm)
	if strings.Contains(view, "esc or ? to close") {
		t.Error("help still open after esc")
	}
}

func TestQuickSearchJumpsToTask(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	// #1200033 is whatever the demo's My tasks list preselects, not the task the search will find.
	// Asserting it changes is what proves enter actually jumped, "#12000" alone is a prefix every demo task shares.
	waitFor(t, tm, "#1200033")
	press(tm, "ctrl+f")
	waitFor(t, tm, "Search")
	from := mark(t, tm)
	press(tm, "fix auth retry") // the only task matching all three words is IEAATASK00, Fix auth retry loop
	waitAfter(t, tm, from, "Fix auth retry loop")
	// A permalink is on screen from the very first task the detail pane ever showed, so the wait needs a fresh mark:
	// only a frame drawn after enter proves the overlay actually closed.
	from = mark(t, tm)
	press(tm, "enter")
	waitAfter(t, tm, from, "#1200000")
	view := finalView(t, tm)
	if !strings.Contains(view, "#1200000") || strings.Contains(view, "#1200033") || strings.Contains(view, "Search") {
		t.Errorf("enter should close the search and show the matched task:\n%s", view)
	}
}
