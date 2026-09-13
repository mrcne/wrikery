package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitTitleMsg struct{ taskID, title string }

type titleDialog struct {
	taskID  string
	was     string
	input   textinput.Model
	errText string
}

func newTitleDialog(task store.Task, width int) (titleDialog, tea.Cmd) {
	in := textinput.New()
	in.Prompt = ""
	in.SetValue(task.Title)
	in.Width = width - 4
	in.CursorEnd()
	return titleDialog{taskID: task.ID, was: task.Title, input: in}, in.Focus()
}

func (d titleDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		title := strings.Join(strings.Fields(d.input.Value()), " ")
		if title == "" {
			d.errText = "a title cannot be empty"
			return d, nil
		}
		if title == d.was {
			return d, intent(closeDialogMsg{})
		}
		return d, tea.Batch(intent(submitTitleMsg{taskID: d.taskID, title: title}), intent(closeDialogMsg{}))
	}
	d.errText = ""
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

func (d titleDialog) View(th Theme, width int) string {
	hint := lipgloss.NewStyle().Foreground(th.Muted).Render("enter saves   esc cancels")
	if d.errText != "" {
		hint = lipgloss.NewStyle().Foreground(th.Error).Render(d.errText)
	}
	body := d.input.View() + "\n\n" + hint
	return th.box("Title", body, width, lipgloss.Height(body)+2, true)
}
