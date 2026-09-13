package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitDatesMsg struct {
	taskID string
	dates  store.TaskDates
}

type datesDialog struct {
	taskID  string
	inputs  [2]textinput.Model
	focus   int
	errText string
	now     time.Time
}

func newDatesDialog(task store.Task, now time.Time) (datesDialog, tea.Cmd) {
	d := datesDialog{taskID: task.ID, now: now}
	for i, label := range []string{"start", "due"} {
		in := textinput.New()
		in.Prompt = fmt.Sprintf("%-6s", label)
		in.Placeholder = "2026-09-12, fri, +3d, today, empty clears"
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

func (d datesDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab, tea.KeyDown, tea.KeyUp, tea.KeyShiftTab:
		d.inputs[d.focus].Blur()
		d.focus = 1 - d.focus
		return d, d.inputs[d.focus].Focus()
	case tea.KeyEnter:
		start, err := parseDate(d.inputs[0].Value(), d.now)
		if err != nil {
			d.errText = "start: " + err.Error()
			return d, nil
		}
		due, err := parseDate(d.inputs[1].Value(), d.now)
		if err != nil {
			d.errText = "due: " + err.Error()
			return d, nil
		}
		if start != "" && due != "" && due < start {
			d.errText = "due is before start"
			return d, nil
		}
		return d, tea.Batch(intent(submitDatesMsg{taskID: d.taskID, dates: datesFrom(start, due)}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
	d.errText = ""
	return d, cmd
}

// datesFrom maps two dates to a Wrike dates block, see the Update Task reference at https://developers.wrike.com/reference/puttaskssingle.
// dates.type is Backlog, Milestone or Planned.
// duration is optional for a Planned task and Wrike computes it itself (one Wrike day is 480 minutes), so datesFrom leaves it unset.
// A start date has to travel with a due date or a duration, and a due date sent alone turns the task into a Milestone,
// so a Planned task here always carries both start and due.
// Dates are yyyy-MM-dd with an optional time part.
// No dates is a Backlog task.
// One date makes a one day Planned task, which is what the web app does when you set only a due date.
// Milestones are not created here.
func datesFrom(start, due string) store.TaskDates {
	if start == "" && due == "" {
		return store.TaskDates{Type: "Backlog"}
	}
	if start == "" {
		start = due
	}
	if due == "" {
		due = start
	}
	return store.TaskDates{Type: "Planned", Start: start, Due: due}
}

func (d datesDialog) View(th Theme, width, height int) string {
	body := d.inputs[0].View() + "\n" + d.inputs[1].View() + "\n\n"
	if d.errText != "" {
		body += lipgloss.NewStyle().Foreground(th.Error).Render(d.errText) + "\n"
	}
	body += lipgloss.NewStyle().Foreground(th.Muted).Render("tab switch   enter save   esc cancel")
	return th.box("Dates", body, min(width, 60), lipgloss.Height(body)+2, true)
}
