package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

func TestImportanceDialogStartsOnTheCurrentValue(t *testing.T) {
	d := newImportanceDialog(store.Task{ID: "T1", Importance: "Low"}, defaultKeyMap())
	if d.cursor != 2 {
		t.Fatalf("cursor = %d, want Low at 2", d.cursor)
	}
	dl, _ := dialog(d).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (submitImportanceMsg{taskID: "T1", importance: "Normal"}) {
		t.Errorf("k enter -> %#v", msgs)
	}
}

// A cache row from before importance was synced has none, and the box treats that as Normal.
func TestImportanceDialogClosesWithoutAWriteWhenUnchanged(t *testing.T) {
	d := newImportanceDialog(store.Task{ID: "T1"}, defaultKeyMap())
	if d.cursor != 1 {
		t.Fatalf("cursor = %d, want Normal at 1", d.cursor)
	}
	_, cmd := dialog(d).Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 || msgs[0] != (closeDialogMsg{}) {
		t.Errorf("enter on the same value -> %#v, want only the close", msgs)
	}
}
