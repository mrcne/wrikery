package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitStatusMsg struct{ taskID, statusID, name, group string }

type statusDialog struct {
	taskID string
	items  []store.CustomStatus
	cursor int
	keys   KeyMap
}

// newStatusDialog offers the statuses of the workflow the task is in, and nothing when the cache holds no such workflow.
// Any other workflow would be the wrong offer: a status picked from it moves the task onto that workflow.
func newStatusDialog(task store.Task, ref refData, keys KeyMap) statusDialog {
	var wf *store.Workflow
	for i := range ref.workflows {
		for _, cs := range ref.workflows[i].CustomStatuses {
			if cs.ID == task.CustomStatusID {
				wf = &ref.workflows[i]
				break
			}
		}
		if wf != nil {
			break
		}
	}
	d := statusDialog{taskID: task.ID, keys: keys}
	if wf == nil {
		return d
	}
	for _, cs := range wf.CustomStatuses {
		if cs.Hidden {
			continue
		}
		if cs.ID == task.CustomStatusID {
			d.cursor = len(d.items)
		}
		d.items = append(d.items, cs)
	}
	return d
}

func (d statusDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch {
	case key.Matches(msg, d.keys.Down):
		if d.cursor < len(d.items)-1 {
			d.cursor++
		}
	case key.Matches(msg, d.keys.Up):
		if d.cursor > 0 {
			d.cursor--
		}
	case msg.Type == tea.KeyEnter:
		if d.cursor < len(d.items) {
			cs := d.items[d.cursor]
			return d, tea.Batch(intent(submitStatusMsg{taskID: d.taskID, statusID: cs.ID, name: cs.Name, group: cs.Group}), intent(closeDialogMsg{}))
		}
	}
	return d, nil
}

func (d statusDialog) View(th Theme, width int) string {
	var b strings.Builder
	group := ""
	for i, cs := range d.items {
		if cs.Group != group {
			group = cs.Group
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(lipgloss.NewStyle().Foreground(th.Muted).Render(group) + "\n")
		}
		label := lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group)) + " " + cs.Name
		b.WriteString(rowLine(th, label, width-2, i == d.cursor, true) + "\n")
	}
	if len(d.items) == 0 {
		b.WriteString("no workflow known for this task yet, refresh and try again\n")
	}
	body := strings.TrimRight(b.String(), "\n")
	return th.box("Status", body, min(width, 50), lipgloss.Height(body)+2, true)
}
