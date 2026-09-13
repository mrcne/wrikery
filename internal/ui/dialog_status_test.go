package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestStatusDialogListsTheTaskWorkflowWithoutHidden(t *testing.T) {
	ref := refData{statuses: map[string]store.CustomStatus{}}
	ref.workflows = []store.Workflow{
		{ID: "W1", Standard: true, CustomStatuses: []store.CustomStatus{{ID: "A", Name: "New", Group: "Active"}, {ID: "B", Name: "Done", Group: "Completed"}}},
		{ID: "W2", CustomStatuses: []store.CustomStatus{{ID: "C", Name: "Backlog", Group: "Active"}, {ID: "D", Name: "Secret", Group: "Active", Hidden: true}, {ID: "E", Name: "Shipped", Group: "Completed"}}},
	}
	for _, w := range ref.workflows {
		for _, cs := range w.CustomStatuses {
			ref.statuses[cs.ID] = cs
		}
	}
	d := newStatusDialog(store.Task{ID: "T", CustomStatusID: "E"}, ref, defaultKeyMap())
	if len(d.items) != 2 || d.items[0].ID != "C" || d.items[1].ID != "E" || d.cursor != 1 {
		t.Errorf("items = %+v cursor = %d", d.items, d.cursor)
	}
	d3, _ := dialog(d).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	_, cmd := d3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (submitStatusMsg{taskID: "T", statusID: "C", name: "Backlog", group: "Active"}) {
		t.Errorf("enter -> %#v", msgs)
	}
}

// A status no cached workflow holds means the task is on a workflow the cache does not have.
// Offering the standard workflow instead would move the task onto it with the first pick, so the dialog offers nothing.
func TestStatusDialogRefusesATaskWhoseWorkflowIsNotCached(t *testing.T) {
	ref := refData{statuses: map[string]store.CustomStatus{}}
	ref.workflows = []store.Workflow{
		{ID: "W1", Standard: true, CustomStatuses: []store.CustomStatus{{ID: "A", Name: "New", Group: "Active"}}},
	}
	d := newStatusDialog(store.Task{ID: "T", CustomStatusID: "unknown"}, ref, defaultKeyMap())
	if len(d.items) != 0 {
		t.Errorf("items = %+v, want none", d.items)
	}
	_, cmd := dialog(d).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("enter -> %#v, want nothing queued", msgs)
	}
	if view := d.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40); !strings.Contains(view, "no workflow known") {
		t.Errorf("view = %q, want the refusal", view)
	}
}
