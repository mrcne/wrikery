package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestTimelogDialogSubmitsCreateAndEdit(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	d, _ := newTimelogDialog("T", "Fix", nil, "2026-09-02", now, 80)
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
	d2, _ := newTimelogDialog("T", "Fix", existing, "", now, 80)
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

// Time is logged after the fact, so a bare weekday in the date field is the last one, not the coming one.
func TestTimelogDialogReadsAWeekdayAsTheLastOne(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) // Thursday
	d, _ := newTimelogDialog("T", "Fix", nil, "", now, 80)
	var dl dialog = d
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "mon" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) == 0 {
		t.Fatal("enter emitted nothing, want submitTimelogMsg")
	}
	if got, ok := msgs[0].(submitTimelogMsg); !ok || got.date != "2026-08-31" {
		t.Errorf("submit = %#v, want the Monday three days back", msgs)
	}
}

// The day a field reads to is shown next to it, so a wrong reading is visible before it is saved,
// and a value the parser refuses gets the parser's own error on enter.
func TestTimelogDialogShowsTheDayItReads(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) // Thursday
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d, _ := newTimelogDialog("T", "Fix", nil, "", now, 80)
	var dl dialog = d
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if view := squeeze(dl.View(th, 80, 30)); !strings.Contains(view, "today Thu 3 Sep 2026") {
		t.Errorf("view does not name the day today reads to:\n%s", view)
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "last wed" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if view := squeeze(dl.View(th, 80, 30)); !strings.Contains(view, "last wed Wed 2 Sep 2026") {
		t.Errorf("view does not name the day last wed reads to:\n%s", view)
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "someday" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if view := squeeze(dl.View(th, 80, 30)); strings.Contains(view, "Sep 2026") {
		t.Errorf("view names a day for a value the parser refuses:\n%s", view)
	}
	dl, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("an unreadable date emitted %#v, want nothing", msgs)
	}
	if td, ok := dl.(timelogDialog); !ok || !strings.HasPrefix(td.errText, "date: could not read the date") {
		t.Errorf("errText = %q, want the parser's error", td.errText)
	}
	// The list of forms is longer than the box is wide, it wraps instead of being cut at the edge.
	if view := squeeze(dl.View(th, 80, 30)); !strings.Contains(view, "+3d or 12 sep") {
		t.Errorf("view cuts the error short:\n%s", dl.View(th, 80, 30))
	}
}

// The note takes up to 200 characters and the box is 70 wide, so the field has to scroll with the cursor.
// Without a width the input draws its whole value and the box cuts the line at its border, end of text and cursor outside it.
func TestTimelogDialogScrollsALongNote(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d, _ := newTimelogDialog("T", "Fix", nil, "", now, 80)
	var dl dialog = d
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "START " + strings.Repeat("abcdefghij ", 8) + "the end" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	view := dl.View(th, 80, 30)
	if !strings.Contains(view, "the end") {
		t.Errorf("the end of a long note is outside the box:\n%s", view)
	}
	if strings.Contains(view, "START") {
		t.Errorf("the field shows the start of the note instead of the part under the cursor:\n%s", view)
	}
	// An edit opens with the cursor after the existing note, so the box shows its end before any key.
	existing := &store.Timelog{ID: "L1", TaskID: "T", Hours: 2, TrackedDate: "2026-09-01", Comment: "START " + strings.Repeat("abcdefghij ", 8) + "the end"}
	d2, _ := newTimelogDialog("T", "Fix", existing, "", now, 80)
	if view := d2.View(th, 80, 30); !strings.Contains(view, "the end") || strings.Contains(view, "START") {
		t.Errorf("an edit of a long note does not open on its end:\n%s", view)
	}
}
