package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func typed(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// Enter closes the actions box and runs the row in the same update, so a second enter already in the queue
// meets the box the run opened and never runs the row again.
func TestEnterInTheActionsBoxRunsTheRowAtOnce(t *testing.T) {
	m := demoModel(t)
	m = pump(m, typed("."))
	if _, ok := m.dialog.(actionsDialog); !ok {
		t.Fatalf(". opened %T, want the actions box", m.dialog)
	}
	m = pump(m, typed("a"), typed("s"), typed("s"))
	// The command is not run on purpose: the run must not travel through a message.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if _, ok := m.dialog.(assigneeDialog); !ok {
		t.Errorf("after enter the dialog is %T, want the assignee box in the same update", m.dialog)
	}
}

// The list can move under an open box, a sync that completes or moves the task, and the keys read the selection at run time.
func TestEnterInTheActionsBoxRefusesWhenTheSelectionMoved(t *testing.T) {
	m := demoModel(t)
	before, _ := m.selectedTask()
	m = pump(m, typed("."))
	m.list = pressKey(m.list, "j")
	if after, _ := m.selectedTask(); after.ID == before.ID {
		t.Fatal("j did not move the selection")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.dialog != nil {
		t.Errorf("after enter the dialog is %T, want none", m.dialog)
	}
	if !strings.Contains(m.status.toast, "selection changed") {
		t.Errorf("toast = %q, want the selection changed", m.status.toast)
	}
}

func TestHintBarNamesTheActionsKey(t *testing.T) {
	m := demoModel(t)
	names := func() bool {
		return slices.ContainsFunc(m.hintBindings(), func(b key.Binding) bool { return b.Help().Key == "." })
	}
	if m.focus = paneList; !names() {
		t.Error("the list hints lack .")
	}
	if m.focus = paneDetail; !names() {
		t.Error("the detail hints lack .")
	}
	if m.shape, m.focus = shapeBoard, paneBoard; !names() {
		t.Error("the board hints lack .")
	}
}
