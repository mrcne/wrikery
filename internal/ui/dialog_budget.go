package ui

import (
	"math"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

// submitBudgetMsg is the budget box's answer, hours of 0 clear the budget.
type submitBudgetMsg struct {
	taskID string
	hours  float64
}

// budgetDialog is the one field box of the budget row, the forms of a time entry without its 24 hour cap.
type budgetDialog struct {
	taskID  string
	was     float64 // the budget the box opened with, 0 for none, so an unchanged value closes the box without a write
	input   textinput.Model
	errText string
	done    bool // submitted, a second enter already in the queue must not queue the write twice
}

func newBudgetDialog(task store.Task, b budgetInfo) (budgetDialog, tea.Cmd) {
	in := textinput.New()
	in.Prompt = "hours "
	in.Placeholder = "20h, 1h 30m, 0.5, empty clears"
	in.CharLimit = 20
	d := budgetDialog{taskID: task.ID, input: in}
	if b.has {
		d.input.SetValue(formatHours(b.hours))
		d.was = b.hours
	}
	// Focused on the box's own copy, a focus on the local value would be lost in the copy and the input would ignore every key.
	cmd := d.input.Focus()
	return d, cmd
}

func (d budgetDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	if d.done {
		return d, nil
	}
	if msg.Type == tea.KeyEnter {
		hours, err := parseDuration(d.input.Value())
		if err != nil {
			d.errText = err.Error()
			return d, nil
		}
		// Compared at the minute, the unit the box reads and writes, so 12:30 and 12h 30m are one value.
		if math.Round(hours*60) == math.Round(d.was*60) {
			return d, intent(closeDialogMsg{})
		}
		d.done = true
		return d, tea.Batch(intent(submitBudgetMsg{taskID: d.taskID, hours: hours}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	d.errText = ""
	return d, cmd
}

func (d budgetDialog) View(th Theme, width, height int) string {
	body := d.input.View()
	if d.errText != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(th.Error).Render(d.errText)
	}
	body += "\n\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("enter saves   esc cancel")
	width = min(width, 50)
	return th.box("Budget", body, width, lipgloss.Height(body)+2, true)
}
