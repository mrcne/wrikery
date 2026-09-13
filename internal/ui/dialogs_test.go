package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
)

func TestConfirmDialogEmitsOnYes(t *testing.T) {
	type payload struct{ n int }
	d := dialog(confirmDialog{prompt: "Sure?", onYes: payload{7}})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (payload{7}) || msgs[1] != (closeDialogMsg{}) {
		t.Errorf("msgs = %#v", msgs)
	}
	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if msgs := collect(cmd); len(msgs) != 1 || msgs[0] != (closeDialogMsg{}) {
		t.Errorf("n -> %#v", msgs)
	}
}

// ctrl+d sends a comment as well as ctrl+s, the hint under the box has to say so.
func TestCommentDialogHintNamesBothSendKeys(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d, _ := newCommentDialog("T1", "Fix auth retry loop", 60)
	out := d.View(th, 60, 40)
	if !strings.Contains(out, "ctrl+s") || !strings.Contains(out, "ctrl+d") {
		t.Errorf("hint names only one send key:\n%s", out)
	}
}

// collect runs a command tree and returns the messages it produces. tea.Batch returns a BatchMsg of commands.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collect(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}
