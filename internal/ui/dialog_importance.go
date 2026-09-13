package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitImportanceMsg struct{ taskID, importance string }

// The three values Wrike accepts for a task's importance (https://developers.wrike.com/api/v4/tasks/).
var importanceLevels = []string{"High", "Normal", "Low"}

type importanceDialog struct {
	taskID string
	cursor int
	was    int
	keys   KeyMap
}

func newImportanceDialog(task store.Task, keys KeyMap) importanceDialog {
	d := importanceDialog{taskID: task.ID, cursor: 1, keys: keys}
	for i, level := range importanceLevels {
		if level == task.Importance {
			d.cursor = i
		}
	}
	d.was = d.cursor
	return d
}

func (d importanceDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch {
	case key.Matches(msg, d.keys.Down):
		if d.cursor < len(importanceLevels)-1 {
			d.cursor++
		}
	case key.Matches(msg, d.keys.Up):
		if d.cursor > 0 {
			d.cursor--
		}
	case msg.Type == tea.KeyEnter:
		if d.cursor == d.was {
			return d, intent(closeDialogMsg{})
		}
		level := importanceLevels[d.cursor]
		return d, tea.Batch(intent(submitImportanceMsg{taskID: d.taskID, importance: level}), intent(closeDialogMsg{}))
	}
	return d, nil
}

func (d importanceDialog) View(th Theme, width, height int) string {
	var b strings.Builder
	for i, level := range importanceLevels {
		label := "  " + level
		if level == "High" {
			label = lipgloss.NewStyle().Foreground(th.Warn).Render(th.Glyphs.Important) + " " + level
		}
		b.WriteString(rowLine(th, label, width-2, i == d.cursor, true) + "\n")
	}
	body := strings.TrimRight(b.String(), "\n")
	return th.box("Importance", body, min(width, 30), lipgloss.Height(body)+2, true)
}
