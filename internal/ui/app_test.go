package ui_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// testOptionsWithCopy adds a Copy hook that records what it was asked to copy, so a test can
// assert on the text without a real clipboard.
func testOptionsWithCopy(st *store.Store) (ui.Options, *[]string) {
	var copied []string
	o := testOptions(st)
	o.Hooks.Copy = func(text string) error {
		copied = append(copied, text)
		return nil
	}
	return o, &copied
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
			// The frame is captured as it stands, so the wait has to be for something only the finished reads draw.
			// The detail pane is in the window from 120 columns up, below that the list title is the last thing to land.
			loaded := "Tasks: My tasks ("
			if w >= 120 {
				loaded = "-- Comments ("
			}
			waitFor(t, tm, loaded)
			golden.RequireEqual(t, []byte(finalView(t, tm)))
		})
	}
}

// TestTaskActionGoldens covers the status dialog and the sync issues screen at 120 columns,
// the width where the detail pane enters the window (see TestShellGoldenAtThreeWidths).
// Both cases wait for "-- Comments (" first, the same loaded signal that test uses at 120
// columns and up, so the dialog or the issues screen is captured over a fully drawn frame
// and not one still mid-load.
func TestTaskActionGoldens(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		st := seededStore(t)
		tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(120, 40))
		waitFor(t, tm, "-- Comments (")
		from := mark(t, tm)
		press(tm, "s")
		waitAfter(t, tm, from, "Status")
		// finalView's "q" would reach the dialog instead of quitting, a dialog owns every key but
		// esc and ctrl+c, so the program is stopped with ctrl+c here to capture it still open.
		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
		golden.RequireEqual(t, []byte(tm.FinalModel(t).(ui.Model).View()))
	})
	t.Run("issues", func(t *testing.T) {
		st := seededStore(t)
		tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(120, 40))
		waitFor(t, tm, "-- Comments (")
		from := mark(t, tm)
		press(tm, "!")
		waitAfter(t, tm, from, "Sync issues (2)")
		golden.RequireEqual(t, []byte(finalView(t, tm)))
	})
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
	waitFor(t, tm, "Tasks: My tasks (")
	press(tm, "tab") // list -> detail, at 100 columns the sidebar leaves the window
	waitFor(t, tm, "-- Comments (")
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

// permalinkNumber reads the numeric task id off the end of a demo permalink,
// the same number the detail pane's title shows as "#<id>".
func permalinkNumber(t *testing.T, permalink string) string {
	t.Helper()
	const marker = "id="
	i := strings.LastIndex(permalink, marker)
	if i < 0 {
		t.Fatalf("permalink %q has no id", permalink)
	}
	return permalink[i+len(marker):]
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
	from := mark(t, tm)
	press(tm, "j") // move the cursor onto the second task, which only previews it
	// Wait for the detail pane to actually show the preview before the deliberate enter,
	// or a slow repaint could let enter race ahead of the cursor move and open the still previewed task.
	waitAfter(t, tm, from, "#"+permalinkNumber(t, opened.Permalink))
	press(tm, "enter") // and open that one
	deadline := time.Now().Add(5 * time.Second)
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
	waitFor(t, tm, "Tasks: My tasks (")
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
			waitFor(t, tm, "Tasks: My tasks (")
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
	waitFor(t, tm, "Tasks: My tasks (")
	// The list follows the sidebar cursor.
	// This runs first because the filter has to be the last thing on screen, only the frame the program ends on can be read row by row.
	press(tm, "shift+tab")
	press(tm, "j")
	waitFor(t, tm, "Tasks: Mobile")
	from := mark(t, tm)
	press(tm, "k")
	waitAfter(t, tm, from, "Tasks: My tasks (")
	press(tm, "tab")
	from = mark(t, tm)
	press(tm, "/")
	press(tm, "a", "u", "t", "h")
	waitAfter(t, tm, from, "Tasks: My tasks (")
	if n := taskListTitleCount(t, seenOutput(t, tm).String()[from:]); n >= 30 {
		t.Fatalf("filtering by 'auth' should narrow the list below 30, title count is %d", n)
	}
	press(tm, "enter") // leave the filter input, the filter itself stays
	view := finalView(t, tm)
	rows := taskListRows(view)
	if len(rows) == 0 {
		t.Fatalf("the filtered list should still hold rows:\n%s", view)
	}
	for _, row := range rows {
		if !strings.Contains(strings.ToLower(row), "auth") {
			t.Errorf("row %q is on screen although the filter is auth:\n%s", strings.TrimSpace(row), view)
		}
	}
}

// taskListRows cuts the task rows out of a full frame.
// The panes are drawn next to each other, so a line is split on the pane borders and the list is the second box,
// and a row is told from a blank filler or the filter input by the status glyph that follows the cursor column.
func taskListRows(view string) []string {
	var rows []string
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 || len(cells[3]) < 3 || !strings.ContainsRune("ovzx", rune(cells[3][2])) {
			continue
		}
		rows = append(rows, cells[3])
	}
	return rows
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
	waitFor(t, tm, "Tasks: My tasks (")
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

func TestCopyBindingsCopyTheRightText(t *testing.T) {
	st := seededStore(t)
	opts, copied := testOptionsWithCopy(st)
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "#1200033")
	press(tm, "ctrl+f")
	waitFor(t, tm, "Search")
	from := mark(t, tm)
	press(tm, "fix auth retry")
	waitAfter(t, tm, from, "Fix auth retry loop")
	from = mark(t, tm)
	press(tm, "enter")
	waitAfter(t, tm, from, "#1200000")

	from = mark(t, tm)
	press(tm, "Y")
	waitAfter(t, tm, from, "Copied")
	if want := "1200000-fix-auth-retry-loop"; len(*copied) != 1 || (*copied)[0] != want {
		t.Fatalf("copy branch: got %v, want [%q]", *copied, want)
	}

	from = mark(t, tm)
	press(tm, "i")
	waitAfter(t, tm, from, "Copied")
	if want := "IEAATASK00"; len(*copied) != 2 || (*copied)[1] != want {
		t.Fatalf("copy id: got %v, want id %q at index 1", *copied, want)
	}

	from = mark(t, tm)
	press(tm, "y")
	waitAfter(t, tm, from, "Copied")
	if want := "https://www.wrike.com/open.htm?id=1200000"; len(*copied) != 3 || (*copied)[2] != want {
		t.Fatalf("copy permalink: got %v, want permalink %q at index 2", *copied, want)
	}
}

func TestCommentDialogQueuesAndMarks(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "#1200033")
	press(tm, "ctrl+f")
	waitFor(t, tm, "Search")
	from := mark(t, tm)
	// "Fix auth retry loop" is IEAATASK00 and the only task matching all three words, so the search lands on it.
	press(tm, "fix auth retry")
	waitAfter(t, tm, from, "Fix auth retry loop")
	from = mark(t, tm)
	press(tm, "enter")
	waitAfter(t, tm, from, "#1200000")

	from = mark(t, tm)
	press(tm, "c")
	waitAfter(t, tm, from, "Comment on")

	from = mark(t, tm)
	press(tm, "hello from the test")
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})
	waitAfter(t, tm, from, "Comment queued")
	waitAfter(t, tm, from, "(sending)")

	view := finalView(t, tm)
	if !strings.Contains(view, "hello from the test") {
		t.Errorf("new comment not in the detail pane:\n%s", view)
	}
	comments, err := st.Comments().ListForTask(context.Background(), "IEAATASK00")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range comments {
		found = found || (c.Text == "hello from the test" && strings.HasPrefix(c.ID, store.LocalIDPrefix))
	}
	if !found {
		t.Error("comment not queued as a local row")
	}
}

func TestStatusDialogQueuesAndMarks(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	// #1200033 (IEAATASK33) is whatever the demo's My tasks list preselects, currently Blocked
	// in the Engineering workflow. j moves the cursor down one row, to Done, the next status in
	// workflow order.
	waitFor(t, tm, "#1200033")

	from := mark(t, tm)
	press(tm, "s")
	waitAfter(t, tm, from, "Status")

	from = mark(t, tm)
	press(tm, "j", "enter")
	waitAfter(t, tm, from, "Status set to Done")

	task, err := st.Tasks().Get(context.Background(), "IEAATASK33")
	if err != nil {
		t.Fatal(err)
	}
	if task.CustomStatusID != "IEAAST15" || task.Status != "Completed" {
		t.Errorf("task after status change = %+v, want IEAAST15/Completed", task)
	}
}

func TestAssigneeDialogAddsAResponsible(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	// IEAATASK33, the demo's My tasks preselection, starts out responsible to Ada (me) and Celina.
	// "dawid" narrows the contact list to Dawid Mroz alone.
	waitFor(t, tm, "#1200033")

	from := mark(t, tm)
	press(tm, "a")
	waitAfter(t, tm, from, "Assignees")

	from = mark(t, tm)
	press(tm, "dawid", "space", "enter")
	waitAfter(t, tm, from, "Assignees updated")

	task, err := st.Tasks().Get(context.Background(), "IEAATASK33")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(task.ResponsibleIDs, "KUAAAAD1") {
		t.Errorf("responsibles after assignee change = %v, want KUAAAAD1 added", task.ResponsibleIDs)
	}
}

func TestDatesDialogSetsDue(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "#1200033")
	press(tm, "ctrl+f")
	waitFor(t, tm, "Search")
	from := mark(t, tm)
	// "Fix auth retry loop" is IEAATASK00, the same task the comment flow test picks, and it
	// starts with no dates block, so typing straight into the due field needs no clearing first.
	press(tm, "fix auth retry")
	waitAfter(t, tm, from, "Fix auth retry loop")
	from = mark(t, tm)
	press(tm, "enter")
	waitAfter(t, tm, from, "#1200000")

	from = mark(t, tm)
	press(tm, "d")
	waitAfter(t, tm, from, "Dates")

	from = mark(t, tm)
	press(tm, "tab", "tomorrow", "enter")
	waitAfter(t, tm, from, "Dates updated")

	task, err := st.Tasks().Get(context.Background(), "IEAATASK00")
	if err != nil {
		t.Fatal(err)
	}
	want := testOptions(st).Now().AddDate(0, 0, 1).Format("2006-01-02")
	if task.Dates == nil || task.Dates.Due != want {
		t.Errorf("dates after edit = %+v, want due %s", task.Dates, want)
	}
}

func TestSyncIssuesScreenRetriesAndDiscards(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "#1200033")

	from := mark(t, tm)
	press(tm, "!")
	waitAfter(t, tm, from, "Sync issues (2)")
	waitAfter(t, tm, from, "Task not found")

	// Move to the second failure and confirm it is listed too, before acting on it.
	from = mark(t, tm)
	press(tm, "j")
	waitAfter(t, tm, from, "Timesheet is locked")

	from = mark(t, tm)
	press(tm, "x")
	waitAfter(t, tm, from, "Discard this write?")

	from = mark(t, tm)
	press(tm, "y")
	waitAfter(t, tm, from, "Sync issues (1)")

	from = mark(t, tm)
	press(tm, "r")
	waitAfter(t, tm, from, "Sync issues (0)")

	pending, failed, err := st.Outbox().Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pending != 2 || failed != 0 {
		t.Errorf("outbox counts after retry = pending %d, failed %d, want 2, 0", pending, failed)
	}
}

func TestSyncIssuesEnterOpensTheTask(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "#1200033")

	// Put the sidebar's selected node on IEAATASK00's folder (ProjectAPI), which is not
	// IEAATASK01's folder (Web), so a reload keyed off a stale selection cannot find IEAATASK01
	// by coincidence and the assertions below actually exercise the fix.
	from := mark(t, tm)
	press(tm, "ctrl+f")
	waitFor(t, tm, "Search")
	press(tm, "fix auth retry")
	waitAfter(t, tm, from, "Fix auth retry loop")
	from = mark(t, tm)
	press(tm, "enter")
	waitAfter(t, tm, from, "#1200000")

	from = mark(t, tm)
	press(tm, "!")
	waitAfter(t, tm, from, "Sync issues (2)")

	// The cursor starts on the task update failure, IEAATASK01, permalink #1200001.
	from = mark(t, tm)
	press(tm, "enter")
	waitAfter(t, tm, from, "#1200001")

	// A later reload (any outbox or store change) must not knock the detail off the task just
	// opened: openTaskMsg has to put its parent folder (Web) in the sidebar's selected node, the
	// same way openFromSearch does, or the list reload keyed off the stale API selection fires a
	// taskSelectedMsg for whatever row is there instead. A fixed sleep, not a wait for specific
	// text, is used here: with the fix the reload is idempotent and repaints nothing new to wait
	// for, so the only reliable way to let its goroutine settle before the assertion is to wait.
	tm.Send(ui.OutboxChangedMsg{Pending: 2, Failed: 0})
	time.Sleep(150 * time.Millisecond)

	view := finalView(t, tm)
	if strings.Contains(view, "Sync issues") {
		t.Errorf("enter should leave the issues screen for the task detail:\n%s", view)
	}
	if !strings.Contains(view, "#1200001") {
		t.Errorf("the opened task should still be selected after the outbox message:\n%s", view)
	}
}

// TestSyncIssuesRoutesKeysToTheScreen checks that a key the main screen binds to a task action
// (here s for the status dialog) does nothing on the issues screen, since there is no task pane
// underneath it to act on and the key would otherwise reach whatever task was selected before.
func TestSyncIssuesRoutesKeysToTheScreen(t *testing.T) {
	st := seededStore(t)
	tm := teatest.NewTestModel(t, ui.New(testOptions(st)), teatest.WithInitialTermSize(160, 40))
	waitFor(t, tm, "#1200033")

	from := mark(t, tm)
	press(tm, "!")
	waitAfter(t, tm, from, "Sync issues (2)")

	press(tm, "s")
	// Give a stray Update a moment to land before asserting nothing happened.
	time.Sleep(50 * time.Millisecond)

	view := finalView(t, tm)
	// Nothing on the issues screen itself says "Status", the detail pane label of the same
	// name is part of the main screen this box replaces, so its presence means the dialog opened.
	if strings.Contains(view, "Status") || !strings.Contains(view, "Sync issues") {
		t.Errorf("s should not open the status dialog on the issues screen:\n%s", view)
	}
}
