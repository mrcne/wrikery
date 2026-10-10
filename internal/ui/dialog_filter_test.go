package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func filterRows(d filterDialog) []string {
	var out []string
	for _, r := range d.list.rows {
		out = append(out, r.section+": "+r.label)
	}
	return out
}

func TestFilterBoxListsLevelsPeopleInViewAndTheStatusesInUse(t *testing.T) {
	l := testBoardList()
	d, _ := newFilterDialog(&l)
	// The statuses come in the order of the workflows in view, the one most rows use first, each name once.
	want := []string{
		"Importance: High", "Importance: Normal", "Importance: Low",
		"People: Ada Nowak (me)", "People: Bartek Lis", "People: Unassigned",
		"Status: New", "Status: In Progress", "Status: On Hold", "Status: Completed", "Status: Planned", "Status: Done",
	}
	if got := filterRows(d); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if d.list.checkedCount() != 0 {
		t.Errorf("nothing is ticked without a filter, %d ticked", d.list.checkedCount())
	}
	// You are listed without a task in view, a choice made elsewhere stays listed so it can be unticked,
	// and a name two workflows share is listed once.
	l.narrow = rowFilter{person: "C9", statuses: setOf("done")}
	l.ref.contacts["C9"] = store.Contact{ID: "C9", FirstName: "Celina", LastName: "Wrona"}
	l.ref.workflows = append(l.ref.workflows, store.Workflow{ID: "W3", Name: "Other", CustomStatuses: []store.CustomStatus{{ID: "O1", Name: "NEW", Group: "Active"}}})
	l.ref.statuses["O1"] = l.ref.workflows[2].CustomStatuses[0]
	l.setRows("F1", "API", []store.Task{{ID: "x", CustomStatusID: "S1", ResponsibleIDs: []string{"B1"}}, {ID: "y", CustomStatusID: "O1"}}, nil, "", false)
	d, _ = newFilterDialog(&l)
	want = []string{
		"Importance: High", "Importance: Normal", "Importance: Low",
		"People: Ada Nowak (me)", "People: Bartek Lis", "People: Celina Wrona", "People: Unassigned",
		"Status: New", "Status: In Progress", "Status: On Hold", "Status: Completed", "Status: Done",
	}
	if got := filterRows(d); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if !d.list.checked["p:C9"] || !d.list.checked["s:done"] || d.list.checkedCount() != 2 {
		t.Errorf("the filter's own choices are ticked: %v", d.list.checked)
	}
}

func TestFilterBoxTicksOnePersonAndSubmitsTheChange(t *testing.T) {
	l := testBoardList()
	l.narrow = rowFilter{importance: setOf("High")}
	d, _ := newFilterDialog(&l)
	var dl dialog = d
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 1 {
		t.Fatalf("enter with nothing changed should only close, got %#v", msgs)
	} else if _, ok := msgs[0].(closeDialogMsg); !ok {
		t.Fatalf("enter with nothing changed should only close, got %#v", msgs)
	}
	type_ := func(s string) {
		for _, r := range s {
			dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	clear := func(n int) {
		for range n {
			dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
	}
	type_("ada")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	clear(3)
	type_("bar")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace}) // Bartek takes Ada's place, one person at a time
	clear(3)
	type_("in")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace}) // In Progress
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 {
		t.Fatalf("enter should submit and close, got %#v", msgs)
	}
	want := rowFilter{importance: setOf("High"), person: "B1", statuses: setOf("in progress")}
	if got, ok := msgs[0].(submitFilterMsg); !ok || !reflect.DeepEqual(got.filter, want) {
		t.Errorf("submitted %#v, want %#v", msgs[0], want)
	}
	if _, ok := msgs[1].(closeDialogMsg); !ok {
		t.Errorf("the box should close after the submit, got %#v", msgs[1])
	}
}

func TestFilterBoxViewShowsSectionsAndTheHint(t *testing.T) {
	l := testBoardList()
	d, _ := newFilterDialog(&l)
	view := ansi.Strip(d.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 80, 30))
	for _, want := range []string{"Filter", "Importance", "[ ] High", "People", "[ ] Ada Nowak (me)", "Status", "space toggle   enter apply   esc cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}
