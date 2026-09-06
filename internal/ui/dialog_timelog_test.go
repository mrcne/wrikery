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

// A closeDialogMsg batched alongside editEntryMsg or deleteEntryMsg would race with the dialog either of those opens:
// tea.Batch runs its commands in separate goroutines,
// so a close landing second would wipe the dialog the other message just opened.
// The picker leaves closing to openDialog instead, which is why enter here must produce exactly one message, not two.
func TestEntryPickerEmitsOneMessageForTheRowUnderTheCursor(t *testing.T) {
	logs := []store.Timelog{
		{ID: "a", Hours: 1, TrackedDate: "2026-09-01"},
		{ID: "b", Hours: 2, TrackedDate: "2026-09-02"},
	}
	d := dialog(newEntryPicker(logs, false))
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("edit mode enter should emit exactly one message: %#v", msgs)
	}
	if got, ok := msgs[0].(editEntryMsg); !ok || got.log.ID != "b" {
		t.Errorf("got %#v, want editEntryMsg for b", msgs[0])
	}

	d = dialog(newEntryPicker(logs, true))
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs = collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("delete mode enter should emit exactly one message: %#v", msgs)
	}
	if got, ok := msgs[0].(deleteEntryMsg); !ok || got.log.ID != "b" {
		t.Errorf("got %#v, want deleteEntryMsg for b", msgs[0])
	}
}

// timelogLocked is the one rule both the edit and the delete refusal depend on,
// so it earns its own test rather than only being exercised indirectly through theirs.
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
