package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

// runActionMsg asks the root to run the entry at index of taskActionTable.
// The box sends it alone, with no closeDialogMsg: the run may open a box of its own, and a close that landed after the run
// would shut that one, so the root closes the actions box itself before it runs the entry.
type runActionMsg struct{ index int }

// actionsDialog is the list of every action on the selected task, the keyed ones with their key, narrowed as you type.
type actionsDialog struct {
	title string
	list  checklist
}

func newActionsDialog(task store.Task, actions []taskAction) (actionsDialog, tea.Cmd) {
	rows := make([]checkRow, len(actions))
	for i, a := range actions {
		rows[i] = checkRow{id: strconv.Itoa(i), label: a.label(), hint: a.key.Help().Key}
	}
	list, cmd := newChecklist(rows, nil, "type an action")
	list.single = true
	return actionsDialog{title: task.Title, list: list}, cmd
}

func (d actionsDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		r, ok := d.list.current()
		if !ok {
			return d, nil
		}
		index, _ := strconv.Atoi(r.id)
		return d, intent(runActionMsg{index: index})
	}
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
