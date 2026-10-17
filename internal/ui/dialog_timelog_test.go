package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	// A note written in the Wrike web app can be longer than anything typed here, an edit of the hours sends it back whole.
	long := strings.Repeat("abcdefghij", 25)
	d3, _ := newTimelogDialog("T", "Fix", &store.Timelog{ID: "L2", TaskID: "T", Hours: 1, TrackedDate: "2026-09-01", Comment: long}, "", now, 80)
	_, cmd = dialog(d3).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got, ok := collect(cmd)[0].(submitTimelogMsg); !ok || got.comment != long {
		t.Errorf("an edit cut a %d character note to %d characters", len(long), len(got.comment))
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

// The earlier notes of a task come own first and newest first, then other people's, each wording once.
func TestEarlierNotesOwnFirstNewestFirstDistinct(t *testing.T) {
	logs := []store.Timelog{
		{ID: "1", UserID: "me", TrackedDate: "2026-09-01", CreatedDate: "2026-09-01T10:00:00Z", Comment: "Review"},
		{ID: "2", UserID: "U2", TrackedDate: "2026-09-03", CreatedDate: "2026-09-03T10:00:00Z", Comment: "Pairing"},
		{ID: "3", UserID: "me", TrackedDate: "2026-09-02", CreatedDate: "2026-09-02T10:00:00Z", Comment: " Tests "},
		{ID: "4", UserID: "me", TrackedDate: "2026-09-03", CreatedDate: "2026-09-03T09:00:00Z", Comment: "Review"},
		{ID: "5", UserID: "me", TrackedDate: "2026-09-03", CreatedDate: "2026-09-03T11:00:00Z", Comment: ""},
		{ID: "6", UserID: "U2", TrackedDate: "2026-08-20", CreatedDate: "2026-08-20T10:00:00Z", Comment: "Tests"},
		{ID: "7", UserID: "me", TrackedDate: "2026-09-03", CreatedDate: "2026-09-03T11:30:00Z", Comment: "Standup"},
	}
	got := earlierNotes(logs, "me", "")
	if want := []string{"Standup", "Review", "Tests", "Pairing"}; !slices.Equal(got, want) {
		t.Errorf("earlierNotes = %q, want %q", got, want)
	}
	// The wording already in the field is not an earlier note, an edit would get its own text back on the first ctrl+p.
	if got, want := earlierNotes(logs, "me", "Review"), []string{"Standup", "Tests", "Pairing"}; !slices.Equal(got, want) {
		t.Errorf("earlierNotes with the field on Review = %q, want %q", got, want)
	}
	if got := earlierNotes(nil, "me", ""); len(got) != 0 {
		t.Errorf("no entries gave notes %q", got)
	}
}

// Ctrl+p in the note field steps to an older note of the task and ctrl+n back, past the newest to what was typed.
// The other fields keep their keys, and the hint under the note says where the field is.
func TestTimelogDialogStepsThroughEarlierNotes(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d, _ := newTimelogDialog("T", "Fix", nil, "", now, 80)
	before := d.View(th, 80, 30)
	if view := squeeze(before); strings.Contains(view, "ctrl+p") {
		t.Errorf("a task without earlier notes offers them:\n%s", view)
	}
	d.notes = []string{"Review", "Tests"}
	var dl dialog = d
	after := dl.View(th, 80, 30)
	if view := squeeze(after); !strings.Contains(view, "ctrl+p earlier note") {
		t.Errorf("the hint is missing:\n%s", view)
	}
	// The notes land a frame after the box opens, a box that grows then moves on the screen.
	if lipgloss.Height(before) != lipgloss.Height(after) {
		t.Errorf("the box grows from %d to %d lines when the notes land", lipgloss.Height(before), lipgloss.Height(after))
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if hours := dl.(timelogDialog).inputs[0].Value(); hours != "2" {
		t.Errorf("ctrl+p in the hours field changed it to %q", hours)
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "dr" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	note := func() string { return dl.(timelogDialog).inputs[2].Value() }
	steps := []struct {
		key  tea.KeyType
		want string
	}{
		{tea.KeyCtrlP, "Review"}, {tea.KeyCtrlP, "Tests"}, {tea.KeyCtrlP, "Tests"},
		{tea.KeyCtrlN, "Review"}, {tea.KeyCtrlN, "dr"}, {tea.KeyCtrlN, "dr"},
	}
	for i, s := range steps {
		dl, _ = dl.Update(tea.KeyMsg{Type: s.key})
		if got := note(); got != s.want {
			t.Errorf("step %d: note = %q, want %q", i, got, s.want)
		}
		if i == 0 {
			if view := squeeze(dl.View(th, 80, 30)); !strings.Contains(view, "note 1 of 2") {
				t.Errorf("the hint does not say where the field is:\n%s", view)
			}
		}
	}
	// A change to a taken note makes it the typed text: the hint drops the position, and the steps start over from it.
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if view := squeeze(dl.View(th, 80, 30)); strings.Contains(view, "note 1 of 2") || !strings.Contains(view, "ctrl+p earlier note") {
		t.Errorf("the hint still counts a changed note:\n%s", view)
	}
	for i, s := range []struct {
		key  tea.KeyType
		want string
	}{{tea.KeyCtrlN, "Reviews"}, {tea.KeyCtrlP, "Review"}, {tea.KeyCtrlN, "Reviews"}} {
		dl, _ = dl.Update(tea.KeyMsg{Type: s.key})
		if got := note(); got != s.want {
			t.Errorf("after the change, step %d: note = %q, want %q", i, got, s.want)
		}
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if got, ok := msgs[0].(submitTimelogMsg); !ok || got.comment != "Review" {
		t.Errorf("a taken note is not what enter saves: %#v", msgs)
	}
}

// A note of another entry can be longer than anything typed here, ctrl+p lands it whole.
func TestTimelogDialogTakesALongNoteWhole(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	long := strings.Repeat("abcdefghij", 25)
	d, _ := newTimelogDialog("T", "Fix", nil, "", now, 80)
	d.notes = []string{long}
	var dl dialog = d
	for _, k := range []tea.KeyType{tea.KeyTab, tea.KeyTab, tea.KeyCtrlP} {
		dl, _ = dl.Update(tea.KeyMsg{Type: k})
	}
	if got := dl.(timelogDialog).inputs[2].Value(); got != long {
		t.Errorf("ctrl+p cut a %d character note to %d characters", len(long), len(got))
	}
}

// The box is drawn at the width of each frame, so the field follows a resize while the box is open.
func TestTimelogDialogFollowsAResize(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	existing := &store.Timelog{ID: "L1", TaskID: "T", Hours: 2, TrackedDate: "2026-09-01", Comment: "START " + strings.Repeat("abcdefghij ", 4) + "the end"}
	d, _ := newTimelogDialog("T", "Fix", existing, "", now, 80)
	d.setWidth(40)
	if view := d.View(th, 40, 30); !strings.Contains(view, "the end") || strings.Contains(view, "START") {
		t.Errorf("after a resize to 40 the field does not show the end of the note:\n%s", view)
	}
	m := New(Options{Now: func() time.Time { return now }})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = mm.(Model)
	m.openTimelogDialog("T", "Fix", existing, "")
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	m = mm.(Model)
	if view := m.dialog.View(th, m.dialogWidth(), 30); !strings.Contains(view, "the end") || strings.Contains(view, "START") {
		t.Errorf("a resize of the terminal did not reach the open box:\n%s", view)
	}
}

// The notes land only in a box still open on their task, a read for another task or a closed box is dropped,
// and a second read starts the steps over so the hint cannot count past the new list.
func TestNotesLandOnlyInTheOpenBox(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	m := New(Options{Now: func() time.Time { return now }})
	mm, _ := m.Update(notesLoadedMsg{taskID: "T", notes: []string{"Review"}})
	m = mm.(Model)
	if m.dialog != nil {
		t.Fatalf("notes without a box opened one: %#v", m.dialog)
	}
	m.openTimelogDialog("T", "Fix", nil, "")
	mm, _ = m.Update(notesLoadedMsg{taskID: "U", notes: []string{"Other"}})
	m = mm.(Model)
	if d := m.dialog.(timelogDialog); len(d.notes) != 0 {
		t.Errorf("the notes of another task landed in the box: %q", d.notes)
	}
	mm, _ = m.Update(notesLoadedMsg{taskID: "T", notes: []string{"Review", "Tests"}})
	m = mm.(Model)
	for _, k := range []tea.KeyType{tea.KeyTab, tea.KeyTab, tea.KeyCtrlP, tea.KeyCtrlP} {
		mm, _ = m.Update(tea.KeyMsg{Type: k})
		m = mm.(Model)
	}
	if view := squeeze(m.dialog.View(th, 80, 30)); !strings.Contains(view, "note 2 of 2") {
		t.Fatalf("the keys did not reach the box:\n%s", view)
	}
	mm, _ = m.Update(notesLoadedMsg{taskID: "T", notes: []string{"Review"}})
	m = mm.(Model)
	if view := squeeze(m.dialog.View(th, 80, 30)); !strings.Contains(view, "ctrl+p earlier note") {
		t.Errorf("the hint counts a position in the old list:\n%s", view)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = mm.(Model)
	if got := m.dialog.(timelogDialog).inputs[2].Value(); got != "Review" {
		t.Errorf("ctrl+p after a second read: note = %q, want the first of the new list", got)
	}
}

// The notes only feed a hint, so a failed read is logged and the box works without them instead of showing the raw error.
func TestLoadNotesDropsAFailedRead(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	m := New(Options{Store: st, Now: time.Now})
	if msg := m.loadNotes("T", "")(); msg != nil {
		t.Errorf("a failed read of the notes gave %#v, want nothing", msg)
	}
}
