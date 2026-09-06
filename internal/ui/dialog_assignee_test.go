package ui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

func TestAssigneeDialogFiltersTogglesAndDiffs(t *testing.T) {
	ref := refData{meID: "ME", contacts: map[string]store.Contact{
		"ME": {ID: "ME", FirstName: "Ada", LastName: "Nowak", Me: true},
		"B":  {ID: "B", FirstName: "Bartek", LastName: "Lis"},
		"C":  {ID: "C", FirstName: "Celina", LastName: "Wrona"},
		"X":  {ID: "X", FirstName: "Gone", LastName: "Person", Deleted: true},
	}}
	d, _ := newAssigneeDialog(store.Task{ID: "T", ResponsibleIDs: []string{"B"}}, ref, defaultKeyMap())
	if len(d.visible) != 3 || d.contacts[d.visible[0]].ID != "ME" {
		t.Fatalf("visible = %v (me first, deleted hidden)", d.visible)
	}
	var dl dialog = d
	for _, r := range "cel" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggles Celina
	for range 3 {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyDown})  // to Bartek
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace}) // untoggles Bartek
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) == 0 {
		t.Fatalf("Update on enter emitted nothing, want submitAssigneesMsg")
	}
	got, ok := msgs[0].(submitAssigneesMsg)
	if !ok || !reflect.DeepEqual(got.add, []string{"C"}) || !reflect.DeepEqual(got.remove, []string{"B"}) {
		t.Errorf("diff = %#v", msgs)
	}
}
