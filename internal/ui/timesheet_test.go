package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// rootTestOptions builds Options for a Model exercised directly through Update, without a
// teatest program. A fresh, unseeded store keeps the outbox counts predictable: nothing but
// what the test itself enqueues shows up in them.
func rootTestOptions(t *testing.T) Options {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return Options{
		Store:  st,
		Config: config.UIConfig{Theme: "dark", ASCII: true},
		Now:    func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) },
		Hooks:  Hooks{Refresh: func() {}, WakeOutbox: func() {}},
	}
}

func TestWeekOf(t *testing.T) {
	for in, want := range map[string]string{"2026-09-03": "2026-08-31", "2026-08-31": "2026-08-31", "2026-09-06": "2026-08-31", "2026-09-07": "2026-09-07"} {
		d, _ := time.Parse("2006-01-02", in)
		if got := weekOf(d).Format("2006-01-02"); got != want {
			t.Errorf("weekOf(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestTimesheetGridTotalsAndCursor(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	var ts timesheetModel
	ts.keys = defaultKeyMap()
	ts.set(weekLoadedMsg{
		weekStart: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		logs: []store.Timelog{
			{ID: "a", TaskID: "T1", TrackedDate: "2026-08-31", Hours: 2},
			{ID: "b", TaskID: "T1", TrackedDate: "2026-09-01", Hours: 1.5},
			{ID: "c", TaskID: "T2", TrackedDate: "2026-09-02", Hours: 4, LockStatus: "Locked"},
			{ID: "d", TaskID: "T2", TrackedDate: "2026-09-02", Hours: 0.5},
			{ID: "local:9", TaskID: "T1", TrackedDate: "2026-09-04", Hours: 1},
		},
		titles: map[string]string{"T1": "Fix auth retry loop", "T2": "Rotate signing keys"},
	})
	if len(ts.rows) != 2 || ts.rows[0].title != "Fix auth retry loop" {
		t.Fatalf("rows = %+v", ts.rows)
	}
	out := ts.View(th, 100, 12)
	for _, want := range []string{"Mon", "Sun", "Total", "2.0", "1.5", "4.5", "~1.0", "Fix auth retry loop", "9.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("grid lacks %q:\n%s", want, out)
		}
	}
	if ts.title() != "Timesheet: 31 Aug - 6 Sep 2026" {
		t.Errorf("title = %q", ts.title())
	}
	ts, _ = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	ts, _ = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	ts, _ = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	_, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("e on a two entry cell should ask which one: %#v", msgs)
	}
	if pick, ok := msgs[0].(pickEntryMsg); !ok || len(pick.logs) != 2 {
		t.Errorf("got %#v", msgs[0])
	}
	_, cmd = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if msgs := collect(cmd); len(msgs) != 1 || msgs[0].(loadWeekMsg).start.Format("2006-01-02") != "2026-09-07" {
		t.Errorf("] -> %#v", msgs)
	}
}

// A box height of 12 leaves 8 lines for task rows (height minus the header, the "+ new task"
// row, the blank line and the totals line), well under the 30 rows built here, so the grid has
// to scroll the cursor into view rather than draw all 30 and let the surrounding box cut the
// bottom off, which is what used to drop the totals line for a task list longer than the pane.
func TestTimesheetGridScrollsTheCursorIntoView(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	var ts timesheetModel
	ts.keys = defaultKeyMap()
	ts.loaded = true
	ts.height = 12
	ts.weekStart = time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		ts.rows = append(ts.rows, tsRow{taskID: fmt.Sprintf("T%02d", i), title: fmt.Sprintf("Task %02d", i)})
	}

	for i := 0; i < 25; i++ {
		ts, _ = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if ts.cursorRow != 25 {
		t.Fatalf("cursorRow = %d, want 25", ts.cursorRow)
	}

	out := ts.View(th, 100, 12)
	if lines := strings.Count(out, "\n") + 1; lines > 12 {
		t.Errorf("View emitted %d lines for a height of 12:\n%s", lines, out)
	}
	for _, want := range []string{"Task 25", "Total", "+ new task"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q after scrolling to row 25:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Task 00") {
		t.Errorf("row 0 should have scrolled out of an 8 row window:\n%s", out)
	}
}

// A locked or approved entry is refused with a toast and nothing queued, for both edit and
// delete, and an unlocked delete still opens the confirm dialog: the same rule the edit path
// already covers has to hold for delete too, all the way through the root handlers.
func TestLockedEntryRefusedThroughRootHandlers(t *testing.T) {
	m := New(rootTestOptions(t))
	locked := store.Timelog{ID: "L1", TaskID: "T1", Hours: 2, TrackedDate: "2026-09-01", LockStatus: "Locked"}

	for _, msg := range []tea.Msg{editEntryMsg{log: locked}, deleteEntryMsg{log: locked}} {
		next, _ := m.Update(msg)
		nm := next.(Model)
		if nm.dialog != nil || nm.overlay != overlayNone {
			t.Errorf("%T on a locked entry should not open a dialog", msg)
		}
		// status.show mutates the toast text synchronously, its returned command only ticks the
		// toast's own expiry a few seconds later, so the refusal is read off the model, not the
		// command: running that command through collect would both block on the real tick and
		// return a message carrying no text at all.
		if !strings.Contains(nm.status.toast, "locked") {
			t.Errorf("%T on a locked entry should toast about it being locked, got %q", msg, nm.status.toast)
		}
	}

	pending, _, err := m.opts.Store.Outbox().Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Errorf("pending = %d, want 0: a locked entry must not queue a write", pending)
	}

	unlocked := store.Timelog{ID: "L2", TaskID: "T1", Hours: 1, TrackedDate: "2026-09-01"}
	next, _ := m.Update(deleteEntryMsg{log: unlocked})
	nm := next.(Model)
	if nm.overlay != overlayDialog {
		t.Fatal("delete on an unlocked entry should open the confirm dialog")
	}
	if _, ok := nm.dialog.(confirmDialog); !ok {
		t.Errorf("dialog = %T, want confirmDialog", nm.dialog)
	}
}

// The "+ new task" row has no task id of its own, so newEntryMsg hands off to search in pick
// mode first, carrying the row's date along as pendingDate, and only opens the timelog dialog
// once searchPickMsg answers with the task the user picked.
func TestNewEntryFromTheAddRowGoesThroughSearchPick(t *testing.T) {
	m := New(rootTestOptions(t))

	next, _ := m.Update(newEntryMsg{taskID: "", date: "2026-09-02"})
	nm := next.(Model)
	if nm.overlay != overlaySearch || !nm.search.pickMode {
		t.Fatalf("the add row should open search in pick mode, overlay=%v pickMode=%v", nm.overlay, nm.search.pickMode)
	}
	if nm.pendingDate != "2026-09-02" {
		t.Errorf("pendingDate = %q, want 2026-09-02", nm.pendingDate)
	}

	picked := store.Task{ID: "T9", Title: "Picked task"}
	next, _ = nm.Update(searchPickMsg{task: picked})
	nm = next.(Model)
	d, ok := nm.dialog.(timelogDialog)
	if !ok {
		t.Fatalf("dialog = %T, want timelogDialog", nm.dialog)
	}
	if d.taskID != "T9" {
		t.Errorf("taskID = %q, want T9", d.taskID)
	}
	if got := d.inputs[1].Value(); got != "2026-09-02" {
		t.Errorf("date input = %q, want 2026-09-02", got)
	}
}
