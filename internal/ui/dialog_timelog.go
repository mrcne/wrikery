package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitTimelogMsg struct {
	taskID, timelogID string
	hours             float64
	date, comment     string
}
type deleteTimelogMsg struct{ id string }

type timelogDialog struct {
	taskID, timelogID, title string
	inputs                   [3]textinput.Model // hours, date, comment
	focus                    int
	errText                  string
	now                      time.Time
	// The notes of the task's earlier entries, loaded after the box opens, see notesLoadedMsg.
	// noteIdx is the one in the field, -1 for what was typed, kept in draft while a note is shown.
	notes   []string
	noteIdx int
	draft   string
}

// earlierNotes is what the note field steps through: each wording once, own entries first, newest first.
// Other people's notes come after, on a shared task they may be exactly what is wanted.
// current is the wording the field holds when the box opens, an edit would get its own note back on the first ctrl+p.
func earlierNotes(logs []store.Timelog, meID, current string) []string {
	sorted := slices.Clone(logs)
	slices.SortStableFunc(sorted, func(a, b store.Timelog) int {
		if (a.UserID == meID) != (b.UserID == meID) {
			if a.UserID == meID {
				return -1
			}
			return 1
		}
		if a.TrackedDate != b.TrackedDate {
			return strings.Compare(b.TrackedDate, a.TrackedDate)
		}
		return strings.Compare(b.CreatedDate, a.CreatedDate)
	})
	var notes []string
	for _, l := range sorted {
		if c := strings.TrimSpace(l.Comment); c != "" && c != strings.TrimSpace(current) && !slices.Contains(notes, c) {
			notes = append(notes, c)
		}
	}
	return notes
}

// timelogBoxWidth is the most the box takes, the fields are sized from it when the box opens.
const timelogBoxWidth = 70

func newTimelogDialog(taskID, title string, existing *store.Timelog, date string, now time.Time, width int) (timelogDialog, tea.Cmd) {
	d := timelogDialog{taskID: taskID, title: title, now: now, noteIdx: -1}
	labels := []string{"hours", "date", "note"}
	values := []string{"", date, ""}
	if date == "" {
		values[1] = "today"
	}
	if existing != nil {
		d.timelogID = existing.ID
		values = []string{formatHours(existing.Hours), existing.TrackedDate, existing.Comment}
	}
	for i := range d.inputs {
		in := textinput.New()
		in.Prompt = fmt.Sprintf("%-6s", labels[i])
		if i < 2 {
			// Hours and a date are a few characters, the limit keeps them inside the box and the day hint right after the date.
			in.CharLimit = 24
		}
		// The note has no limit: it scrolls, Wrike documents none for a timelog comment,
		// and a limit set on the field would cut a longer note written in the web app when its hours are edited.
		in.SetValue(values[i])
		d.inputs[i] = in
	}
	d.inputs[0].Placeholder = "1.5, 1:30, 90m, 1h 20m"
	d.setWidth(width)
	return d, d.inputs[0].Focus()
}

// setWidth sizes the note field to the box: its inner width less the prompt and one cell for the cursor after the last character.
// A field without a width draws its whole value, and the box cuts the line at its border without a mark.
// The box is drawn at the width of each frame, so a resize while it is open comes through here too.
func (d *timelogDialog) setWidth(width int) {
	in := &d.inputs[2]
	in.Width = min(width, timelogBoxWidth) - 2 - len(in.Prompt) - 1
	// The scroll window is computed when the cursor moves, not when the width changes.
	in.SetCursor(in.Position())
}

func (d timelogDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab, tea.KeyDown:
		d.inputs[d.focus].Blur()
		d.focus = (d.focus + 1) % 3
		return d, d.inputs[d.focus].Focus()
	case tea.KeyShiftTab, tea.KeyUp:
		d.inputs[d.focus].Blur()
		d.focus = (d.focus + 2) % 3
		return d, d.inputs[d.focus].Focus()
	case tea.KeyCtrlP, tea.KeyCtrlN:
		if d.focus == 2 && len(d.notes) > 0 {
			d.stepNote(msg.Type == tea.KeyCtrlP)
			d.errText = ""
			return d, nil
		}
	case tea.KeyEnter:
		hours, err := parseHours(d.inputs[0].Value())
		if err != nil {
			d.errText = err.Error()
			return d, nil
		}
		date, err := parseDate(d.inputs[1].Value(), d.now, dateBack)
		if err != nil {
			d.errText = "date: " + err.Error()
			return d, nil
		}
		if date == "" {
			d.errText = "date: a tracked date is required"
			return d, nil
		}
		// TODO: TimelogUpdatePayload.Comment carries omitempty, so an edit that clears the note
		// leaves the old one in place. Accepted for v1.
		// Revisit once the outbox payload can carry an explicit "clear this field" marker.
		return d, tea.Batch(intent(submitTimelogMsg{taskID: d.taskID, timelogID: d.timelogID, hours: hours, date: date, comment: strings.TrimSpace(d.inputs[2].Value())}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	before := d.inputs[d.focus].Value()
	d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
	if d.focus == 2 && d.noteIdx >= 0 && d.inputs[2].Value() != before {
		// A changed note is the typed text again, the steps start over from it.
		d.noteIdx = -1
	}
	d.errText = ""
	return d, cmd
}

// stepNote moves through the earlier notes like a shell history, older on ctrl+p and newer on ctrl+n,
// past the newest back to what was typed.
func (d *timelogDialog) stepNote(older bool) {
	next := d.noteIdx - 1
	if older {
		next = d.noteIdx + 1
	}
	if next < -1 || next >= len(d.notes) {
		return
	}
	if d.noteIdx == -1 {
		d.draft = d.inputs[2].Value()
	}
	d.noteIdx = next
	if next == -1 {
		d.inputs[2].SetValue(d.draft)
	} else {
		d.inputs[2].SetValue(d.notes[next])
	}
	d.inputs[2].CursorEnd()
}

// noteHint is the line under the note field, the keys to the earlier notes once the task is known to have some.
// View keeps the line when it is empty: the notes land a frame after the box opens, and a box that grows then moves.
func (d timelogDialog) noteHint(th Theme) string {
	if len(d.notes) == 0 {
		return ""
	}
	hint := "ctrl+p earlier note"
	if d.noteIdx >= 0 {
		hint = fmt.Sprintf("note %d of %d   ctrl+p older   ctrl+n newer", d.noteIdx+1, len(d.notes))
	}
	return lipgloss.NewStyle().Foreground(th.Muted).Render("      " + hint)
}

func (d timelogDialog) View(th Theme, width, height int) string {
	title := "Log time on " + d.title
	if d.timelogID != "" {
		title = "Edit time on " + d.title
	}
	body := d.inputs[0].View() + "\n" + d.inputs[1].View() + dateHint(th, d.inputs[1].Value(), d.now, dateBack) + "\n" + d.inputs[2].View() + "\n" + d.noteHint(th) + "\n\n"
	width = min(width, timelogBoxWidth)
	if d.errText != "" {
		body += th.errorLine(d.errText, width-2) + "\n"
	}
	body += lipgloss.NewStyle().Foreground(th.Muted).Render("tab next field   enter save   esc cancel")
	return th.box(title, body, width, lipgloss.Height(body)+2, true)
}

// entryPicker asks which of several entries in one grid cell a key acted on, edit or delete.
type entryPicker struct {
	logs      []store.Timelog
	forDelete bool
	cursor    int
}

func newEntryPicker(logs []store.Timelog, forDelete bool) entryPicker {
	return entryPicker{logs: logs, forDelete: forDelete}
}

func (d entryPicker) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if d.cursor < len(d.logs)-1 {
			d.cursor++
		}
	case "k", "up":
		if d.cursor > 0 {
			d.cursor--
		}
	case "enter":
		if d.cursor >= len(d.logs) {
			return d, nil
		}
		l := d.logs[d.cursor]
		// No closeDialogMsg here: tea.Batch runs its commands in separate goroutines and delivers them in any order.
		// Batched with a message that opens its own dialog (editEntryMsg, deleteEntryMsg),
		// the close could land second and wipe that dialog.
		// openDialog overwrites m.dialog on its own, which replaces this picker deterministically.
		if d.forDelete {
			return d, intent(deleteEntryMsg{log: l})
		}
		return d, intent(editEntryMsg{log: l})
	}
	return d, nil
}

func (d entryPicker) View(th Theme, width, height int) string {
	var b strings.Builder
	for i, l := range d.logs {
		label := hoursText(l.Hours) + "  " + l.Comment
		b.WriteString(rowLine(th, label, width-2, i == d.cursor, true) + "\n")
	}
	body := strings.TrimRight(b.String(), "\n")
	title := "Edit which entry"
	if d.forDelete {
		title = "Delete which entry"
	}
	return th.box(title, body, min(width, 50), lipgloss.Height(body)+2, true)
}

// timelogLocked is true when Wrike would reject an edit: the entry sits in a locked or approved timesheet.
// Values from https://developers.wrike.com/api/v4/timelogs/ (lockStatus is Locked or Unlocked,
// approvalStatus is Draft, NotRequired, Approved, Rejected, Cancelled or Pending).
// The doc page does not say which of these reject an edit.
// This app treats a locked or an approved entry as read only.
func timelogLocked(l store.Timelog) bool {
	return l.LockStatus == "Locked" || l.ApprovalStatus == "Approved"
}
