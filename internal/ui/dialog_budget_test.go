package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestBudgetDialogPrefillsAndSubmits(t *testing.T) {
	d, _ := newBudgetDialog(store.Task{ID: "T1"}, budgetInfo{has: true, hours: 12.5})
	if got := d.input.Value(); got != "12:30" {
		t.Errorf("prefill = %q, want 12:30", got)
	}
	var dl dialog = d
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "200h" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (submitBudgetMsg{taskID: "T1", hours: 200}) {
		t.Errorf("enter sent %#v", msgs)
	}

	empty, _ := newBudgetDialog(store.Task{ID: "T1"}, budgetInfo{})
	if empty.input.Value() != "" {
		t.Errorf("no budget prefills %q", empty.input.Value())
	}
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	view := empty.View(th, 80, 20)
	for _, want := range []string{"Budget", "20h, 1h 30m, 0.5, empty clears", "enter saves"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	dl = empty
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 2 || msgs[0] != (submitBudgetMsg{taskID: "T1"}) {
		t.Errorf("enter on an empty box sent %#v, want a clear", msgs)
	}
	for _, r := range "soon" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	dl, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 || dl.(budgetDialog).errText == "" {
		t.Errorf("a word must stay in the box with an error, sent %#v, err %q", msgs, dl.(budgetDialog).errText)
	}
}
