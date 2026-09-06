package ui

import (
	"fmt"
	"strconv"
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
}

func newTimelogDialog(taskID, title string, existing *store.Timelog, date string, now time.Time) (timelogDialog, tea.Cmd) {
	d := timelogDialog{taskID: taskID, title: title, now: now}
	labels := []string{"hours", "date", "note"}
	values := []string{"", date, ""}
	if date == "" {
		values[1] = "today"
	}
	if existing != nil {
		d.timelogID = existing.ID
		values = []string{strconv.FormatFloat(existing.Hours, 'f', -1, 64), existing.TrackedDate, existing.Comment}
	}
	for i := range d.inputs {
		in := textinput.New()
		in.Prompt = fmt.Sprintf("%-6s", labels[i])
		in.SetValue(values[i])
		in.CharLimit = 200
		d.inputs[i] = in
	}
	d.inputs[0].Placeholder = "1.5, 1:30, 90m"
	return d, d.inputs[0].Focus()
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
	case tea.KeyEnter:
		hours, err := parseHours(d.inputs[0].Value())
		if err != nil {
			d.errText = err.Error()
			return d, nil
		}
		date, err := parseDate(d.inputs[1].Value(), d.now)
		if err != nil || date == "" {
			d.errText = "date: a tracked date is required"
			return d, nil
		}
		// TODO: TimelogUpdatePayload.Comment carries omitempty, so an edit that clears the note
		// leaves the old one in place. Accepted for v1.
		// Revisit once the outbox payload can carry an explicit "clear this field" marker.
		return d, tea.Batch(intent(submitTimelogMsg{taskID: d.taskID, timelogID: d.timelogID, hours: hours, date: date, comment: strings.TrimSpace(d.inputs[2].Value())}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
	d.errText = ""
	return d, cmd
}

func (d timelogDialog) View(th Theme, width int) string {
	title := "Log time on " + d.title
	if d.timelogID != "" {
		title = "Edit time on " + d.title
	}
	body := d.inputs[0].View() + "\n" + d.inputs[1].View() + "\n" + d.inputs[2].View() + "\n\n"
	if d.errText != "" {
		body += lipgloss.NewStyle().Foreground(th.Error).Render(d.errText) + "\n"
	}
	body += lipgloss.NewStyle().Foreground(th.Muted).Render("tab next field   enter save   esc cancel")
	return th.box(title, body, min(width, 70), lipgloss.Height(body)+2, true)
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
		if d.forDelete {
			prompt := fmt.Sprintf("Delete %.1f h on %s?", l.Hours, l.TrackedDate)
			return d, tea.Batch(intent(openConfirmMsg{prompt: prompt, onYes: deleteTimelogMsg{id: l.ID}}), intent(closeDialogMsg{}))
		}
		return d, tea.Batch(intent(editEntryMsg{log: l}), intent(closeDialogMsg{}))
	}
	return d, nil
}

func (d entryPicker) View(th Theme, width int) string {
	var b strings.Builder
	for i, l := range d.logs {
		label := fmt.Sprintf("%.1f h  %s", l.Hours, l.Comment)
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
