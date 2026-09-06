package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dialog is a centered box that owns its keys until it emits closeDialogMsg. esc is handled by the root.
type dialog interface {
	Update(msg tea.KeyMsg) (dialog, tea.Cmd)
	View(th Theme, width int) string
}

type closeDialogMsg struct{}

// writeQueuedMsg reports a write that reached the outbox. pending and failed are the fresh counts
// read right after, so the status bar updates without waiting for the sync engine's own event.
type writeQueuedMsg struct {
	toast           string
	pending, failed int
}
type submitCommentMsg struct{ taskID, text string }

type commentDialog struct {
	taskID string
	title  string
	ta     textarea.Model
}

func newCommentDialog(taskID, title string, width int) (commentDialog, tea.Cmd) {
	ta := textarea.New()
	ta.Placeholder = "write a comment, ctrl+s sends"
	ta.SetWidth(width - 4)
	ta.SetHeight(6)
	ta.ShowLineNumbers = false
	return commentDialog{taskID: taskID, title: title, ta: ta}, ta.Focus()
}

func (d commentDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	if msg.Type == tea.KeyCtrlS || msg.Type == tea.KeyCtrlD {
		text := strings.TrimSpace(d.ta.Value())
		if text == "" {
			return d, nil
		}
		return d, tea.Batch(intent(submitCommentMsg{taskID: d.taskID, text: text}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	d.ta, cmd = d.ta.Update(msg)
	return d, cmd
}

func (d commentDialog) View(th Theme, width int) string {
	hint := lipgloss.NewStyle().Foreground(th.Muted).Render("ctrl+s send   esc cancel")
	body := d.ta.View() + "\n\n" + hint
	return th.box("Comment on "+d.title, body, width, lipgloss.Height(body)+2, true)
}

type confirmDialog struct {
	prompt string
	onYes  tea.Msg
}

func (d confirmDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		return d, tea.Batch(intent(d.onYes), intent(closeDialogMsg{}))
	case "n":
		return d, intent(closeDialogMsg{})
	}
	return d, nil
}

func (d confirmDialog) View(th Theme, width int) string {
	body := d.prompt + "\n\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("y confirm   n or esc cancel")
	return th.box("Confirm", body, min(width, 60), lipgloss.Height(body)+2, true)
}
