package ui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mrcne/wrikery/internal/store"
)

type submitDatesMsg struct {
	taskID string
	dates  store.TaskDates
}

type datesDialog struct {
	taskID    string
	milestone bool // the task is a milestone, it stays one while the start is left empty
	inputs    [2]textinput.Model
	focus     int
	errText   string
	now       time.Time
}

func newDatesDialog(task store.Task, now time.Time) (datesDialog, tea.Cmd) {
	d := datesDialog{taskID: task.ID, milestone: task.Dates != nil && task.Dates.Type == store.DatesMilestone, now: now}
	for i, label := range []string{"start", "due"} {
		in := newInput()
		in.Prompt = fmt.Sprintf("%-6s", label)
		in.Placeholder = "2026-09-12, fri, last fri, +3d, today, empty clears"
		in.CharLimit = 20
		if task.Dates != nil {
			v := []string{task.Dates.Start, task.Dates.Due}[i]
			if len(v) > 10 {
				// This dialog edits the day, not the time, and Wrike accepts the date alone.
				v = v[:10]
			}
			in.SetValue(v)
		}
		d.inputs[i] = in
	}
	return d, d.inputs[0].Focus()
}

func (d datesDialog) Update(msg tea.KeyPressMsg) (dialog, tea.Cmd) {
	switch msg.String() {
	case "tab", "down", "up", "shift+tab":
		d.inputs[d.focus].Blur()
		d.focus = 1 - d.focus
		return d, d.inputs[d.focus].Focus()
	case "enter":
		start, err := ParseDay(d.inputs[0].Value(), d.now)
		if err != nil {
			d.errText = "start: " + err.Error()
			return d, nil
		}
		due, err := ParseDay(d.inputs[1].Value(), d.now)
		if err != nil {
			d.errText = "due: " + err.Error()
			return d, nil
		}
		if start != "" && due != "" && due < start {
			d.errText = "due is before start"
			return d, nil
		}
		return d, tea.Batch(intent(submitDatesMsg{taskID: d.taskID, dates: DatesFrom(start, due, d.milestone)}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
	d.errText = ""
	return d, cmd
}

// ParseDay reads one word of the dates box into a day, yyyy-MM-dd, the forms parseDate takes with a bare weekday
// or a day and month read as the coming one. Empty stays empty.
// Exported for the dates command, so a word means the same day on the command line as in the box.
func ParseDay(word string, now time.Time) (string, error) {
	return parseDate(word, now, dateAhead)
}

// DatesFrom maps a start and a due day to a Wrike dates block, what the dates box and the dates command write,
// see the Update Task reference at https://developers.wrike.com/reference/puttaskssingle.
// dates.type is Backlog, Milestone or Planned.
// duration is optional for a Planned task and Wrike computes it itself (one Wrike day is 480 minutes), so DatesFrom leaves it unset.
// A start date has to travel with a due date or a duration, and a due date sent alone turns the task into a Milestone,
// so a Planned task here always carries both start and due.
// Dates are yyyy-MM-dd with an optional time part.
// No dates is a Backlog task.
// One date makes a one day Planned task, which is what the web app does when you set only a due date.
// A milestone whose due date moves stays a milestone, a start makes it a Planned task. Milestones are not created here.
func DatesFrom(start, due string, milestone bool) store.TaskDates {
	switch {
	case start == "" && due == "":
		return store.TaskDates{Type: store.DatesBacklog}
	case start == "" && milestone:
		return store.TaskDates{Type: store.DatesMilestone, Due: due}
	case start == "":
		start = due
	case due == "":
		due = start
	}
	return store.TaskDates{Type: store.DatesPlanned, Start: start, Due: due}
}

func (d datesDialog) View(th Theme, width, height int) string {
	body := d.inputs[0].View() + dateHint(th, d.inputs[0].Value(), d.now, dateAhead) + "\n" +
		d.inputs[1].View() + dateHint(th, d.inputs[1].Value(), d.now, dateAhead) + "\n\n"
	width = min(width, 60)
	if d.errText != "" {
		body += th.errorLine(d.errText, width-2) + "\n"
	}
	body += lipgloss.NewStyle().Foreground(th.Muted).Render("tab switch   enter save   esc cancel")
	return th.box("Dates", body, width, lipgloss.Height(body)+2, true)
}
