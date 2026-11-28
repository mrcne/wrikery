package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mrcne/wrikery/internal/store"
)

func TestAssigneeDialogFiltersTogglesAndDiffs(t *testing.T) {
	ref := refData{meID: "ME", contacts: map[string]store.Contact{
		"ME": {ID: "ME", FirstName: "Ada", LastName: "Nowak", Me: true},
		"B":  {ID: "B", FirstName: "Bartek", LastName: "Lis"},
		"C":  {ID: "C", FirstName: "Celina", LastName: "Wrona"},
		"X":  {ID: "X", FirstName: "Gone", LastName: "Person", Deleted: true},
	}}
	d, _ := newAssigneeDialog(store.Task{ID: "T", ResponsibleIDs: []string{"B"}}, ref)
	if len(d.list.visible) != 3 || d.list.rows[d.list.visible[0]].id != "ME" {
		t.Fatalf("visible = %v (me first, deleted hidden)", d.list.visible)
	}
	var dl dialog = d
	for _, r := range "cel" {
		dl, _ = dl.Update(keyRunes(string(r)))
	}
	dl, _ = dl.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // toggles Celina
	for range 3 {
		dl, _ = dl.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	dl, _ = dl.Update(tea.KeyPressMsg{Code: tea.KeyDown})             // to Bartek
	dl, _ = dl.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // untoggles Bartek
	_, cmd := dl.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) == 0 {
		t.Fatalf("Update on enter emitted nothing, want submitAssigneesMsg")
	}
	got, ok := msgs[0].(submitAssigneesMsg)
	if !ok || !reflect.DeepEqual(got.add, []string{"C"}) || !reflect.DeepEqual(got.remove, []string{"B"}) {
		t.Errorf("diff = %#v", msgs)
	}
}
