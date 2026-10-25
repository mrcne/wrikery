package ui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/demo"
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

func TestBudgetRowOpensTheBoxOrSaysWhy(t *testing.T) {
	m := demoModel(t)
	m = pump(m, typed("."))
	// Without a budget source the row says so, and the list is gone.
	m = pump(m, typed("b"), typed("u"), typed("d"))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.dialog != nil || !strings.Contains(m.status.toast, "No budget source is set") {
		t.Errorf("dialog %T, toast %q", m.dialog, m.status.toast)
	}
	// With the demo field named, the box opens on the detail's budget of the first task, Fix auth retry loop with 20h.
	cfg := rootTestOptions(t).Config
	cfg.Budget = config.BudgetConfig{Source: "field", Field: demo.BudgetField}
	m = demoModelWith(t, cfg)
	// The API folder opens on another task, the jump lands the detail on the budgeted one.
	first, err := m.opts.Store.Tasks().Get(context.Background(), "IEAATASK00")
	if err != nil {
		t.Fatal(err)
	}
	m = pump(m, openTaskMsg{task: first})
	if !m.detail.loaded || m.detail.task.ID != first.ID || !m.detail.budget.has {
		t.Fatalf("the detail did not load the budget: loaded %v, task %q, %+v", m.detail.loaded, m.detail.task.ID, m.detail.budget)
	}
	m = pump(m, typed("."), typed("b"), typed("u"), typed("d"))
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if d, ok := m.dialog.(budgetDialog); !ok || d.input.Value() != "20" {
		t.Errorf("after enter the dialog is %T, want the budget box prefilled with 20, toast %q", m.dialog, m.status.toast)
	}
}

func TestKeylessActionsNameTheBudget(t *testing.T) {
	if got := KeylessActions(); !slices.Equal(got, []string{"budget"}) {
		t.Errorf("KeylessActions = %q", got)
	}
	k := defaultKeyMap()
	for _, b := range k.task() {
		if len(b.Keys()) == 0 {
			t.Errorf("task() lists a binding without a key: %+v", b.Help())
		}
	}
}
