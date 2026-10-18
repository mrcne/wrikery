package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dialog is a centered box that owns its keys until it emits closeDialogMsg. esc is handled by the root.
type dialog interface {
	Update(msg tea.KeyMsg) (dialog, tea.Cmd)
	// View gets the width and height it may use, a box that lists things sizes its list from the height.
	View(th Theme, width, height int) string
}

type closeDialogMsg struct{}

// dateHint names the day a date field reads to, so a wrong reading shows before it is saved.
// An empty field and a value the parser refuses get no hint, the refusal comes with enter.
func dateHint(th Theme, value string, now time.Time, dir dateDirection) string {
	d, err := parseDate(value, now, dir)
	if err != nil || d == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return ""
	}
	return lipgloss.NewStyle().Foreground(th.Muted).Render("  " + t.Format("Mon 2 Jan 2006"))
}

// writeQueuedMsg reports a write that reached the outbox. pending and failed are the fresh counts
// read right after, so the status bar updates without waiting for the sync engine's own event.
type writeQueuedMsg struct {
	toast           string
	pending, failed int
	selectID        string // a row the write made, the list lands on it after the reload
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

func (d commentDialog) View(th Theme, width, height int) string {
	hint := lipgloss.NewStyle().Foreground(th.Muted).Render("ctrl+s or ctrl+d send   esc cancel")
	body := d.ta.View() + "\n\n" + hint
	return th.box("Comment on "+d.title, body, width, lipgloss.Height(body)+2, true)
}

// confirmDialog asks before a write that is hard to take back.
// y is the only yes, as the hint says: enter is what a newcomer presses next after a key they did not mean.
// Once answered the box ignores further keys, a second y before the close lands would queue the write twice.
type confirmDialog struct {
	prompt   string
	onYes    tea.Msg
	answered bool
}

func (d confirmDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	if d.answered {
		return d, nil
	}
	switch msg.String() {
	case "y":
		d.answered = true
		return d, tea.Batch(intent(d.onYes), intent(closeDialogMsg{}))
	case "n":
		d.answered = true
		return d, intent(closeDialogMsg{})
	}
	return d, nil
}

func (d confirmDialog) View(th Theme, width, height int) string {
	w := min(width, 60)
	// The box cuts a line at its inner width, so the prompt is wrapped to it first:
	// a long status name or a narrow terminal must not lose the end of a sentence.
	prompt := lipgloss.NewStyle().Width(w - 2).Render(d.prompt)
	body := prompt + "\n\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("y confirm   n or esc cancel")
	return th.box("Confirm", body, w, lipgloss.Height(body)+2, true)
}
