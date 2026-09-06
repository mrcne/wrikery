package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

func TestTimelogDialogSubmitsCreateAndEdit(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	d, _ := newTimelogDialog("T", "Fix", nil, "2026-09-02", now)
	var dl dialog = d
	for _, r := range "1:30" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitTimelogMsg{taskID: "T", hours: 1.5, date: "2026-09-02"}
	if len(msgs) != 2 || msgs[0] != want {
		t.Errorf("create -> %#v", msgs)
	}
	existing := &store.Timelog{ID: "L1", TaskID: "T", Hours: 2, TrackedDate: "2026-09-01", Comment: "old"}
	d2, _ := newTimelogDialog("T", "Fix", existing, "", now)
	_, cmd = dialog(d2).Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs = collect(cmd)
	if got, ok := msgs[0].(submitTimelogMsg); !ok || got.timelogID != "L1" || got.hours != 2 || got.comment != "old" {
		t.Errorf("edit -> %#v", msgs)
	}
}

// timelogLocked has no caller yet.
// The timesheet grid wires it to the edit and delete actions.
// This test keeps it from being flagged as unused in the meantime.
func TestTimelogLocked(t *testing.T) {
	if timelogLocked(store.Timelog{LockStatus: "Unlocked", ApprovalStatus: "Draft"}) {
		t.Error("unlocked draft entry reported locked")
	}
	if !timelogLocked(store.Timelog{LockStatus: "Locked", ApprovalStatus: "Draft"}) {
		t.Error("locked entry not reported locked")
	}
	if !timelogLocked(store.Timelog{LockStatus: "Unlocked", ApprovalStatus: "Approved"}) {
		t.Error("approved entry not reported locked")
	}
}
