package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func typeRunes(d dialog, text string) dialog {
	for _, r := range text {
		d, _ = d.Update(keyRunes(string(r)))
	}
	return d
}

func TestTitleDialogSubmitsTheEditedTitle(t *testing.T) {
	d, _ := newTitleDialog(store.Task{ID: "T1", Title: "Fix login"}, 60)
	dl := typeRunes(dialog(d), " now")
	_, cmd := dl.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (submitTitleMsg{taskID: "T1", title: "Fix login now"}) {
		t.Errorf("enter -> %#v", msgs)
	}
}

func TestTitleDialogClosesWithoutAWriteWhenUnchanged(t *testing.T) {
	d, _ := newTitleDialog(store.Task{ID: "T1", Title: "Fix  login   (MX)"}, 60)
	_, cmd := dialog(d).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 || msgs[0] != (closeDialogMsg{}) {
		t.Errorf("enter on the same title -> %#v, want only the close", msgs)
	}
}

func TestTitleDialogRefusesAnEmptyTitle(t *testing.T) {
	d, _ := newTitleDialog(store.Task{ID: "T1", Title: "Fix login"}, 60)
	dl, _ := dialog(d).Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	dl, cmd := dl.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("enter on an empty title -> %#v, want nothing", msgs)
	}
	view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40)
	if !strings.Contains(view, "cannot be empty") {
		t.Errorf("view = %q, want the refusal", view)
	}
}
