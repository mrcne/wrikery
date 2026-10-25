package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

// actionsDialog is the list of every action on the selected task, the keyed ones with their key, narrowed as you type.
// Enter is the root's, see runPickedAction: the box closes and the row runs in the same update, no message in between.
type actionsDialog struct {
	taskID string // the task the box opened on, the run is refused when the selection has moved since
	title  string
	list   checklist
}

func newActionsDialog(task store.Task, actions []taskAction) (actionsDialog, tea.Cmd) {
	rows := make([]checkRow, len(actions))
	for i, a := range actions {
		rows[i] = checkRow{id: strconv.Itoa(i), label: a.label(), hint: a.key.Help().Key}
	}
	list, cmd := newChecklist(rows, nil, "type an action")
	list.single = true
	return actionsDialog{taskID: task.ID, title: task.Title, list: list}, cmd
}

// picked is the index in the table of the highlighted row, false when the filter left none.
func (d actionsDialog) picked() (int, bool) {
	r, ok := d.list.current()
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(r.id)
	return index, err == nil
}

func (d actionsDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	cmd := d.list.update(msg)
	return d, cmd
}

func (d actionsDialog) View(th Theme, width, height int) string {
	width = min(width, 50)
	// Border, filter, blank, blank and the hint take six lines, the rest is the list, twelve at most.
	body := d.list.view(th, width-2, max(min(12, height-6), 3))
	body += "\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("enter run   esc cancel")
	return th.box("Actions on "+d.title, body, width, lipgloss.Height(body)+2, true)
}
