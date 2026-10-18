package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

// Enter is not an answer, a newcomer who meant another key presses it next, and a box answered once ignores a second press.
func TestConfirmDialogAnswersToYOnly(t *testing.T) {
	type payload struct{ n int }
	d := dialog(confirmDialog{prompt: "Sure?", onYes: payload{7}})
	if _, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Errorf("enter answered the box with %#v", collect(cmd))
	}
	d, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if msgs := collect(cmd); len(msgs) != 2 {
		t.Fatalf("y -> %#v", msgs)
	}
	if _, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); cmd != nil {
		t.Errorf("a second y before the box closed sent %#v again", collect(cmd))
	}
}

// A long status name or a narrow terminal must not cut the end of the prompt, the target status is the point of the box.
func TestConfirmDialogWrapsItsPrompt(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d := confirmDialog{prompt: "L moves \"Document rate limit test\" from Waiting for customer feedback to Ready for QA review."}
	for _, width := range []int{60, 44} {
		out := d.View(th, width, 40)
		text := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(out), "|", " ")), " ")
		if !strings.Contains(text, "to Ready for QA review.") {
			t.Errorf("at width %d the prompt lost its end:\n%s", width, out)
		}
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
// squeeze strips the colours and runs of spaces, the cursor cell counts for one.
func squeeze(view string) string {
	return strings.Join(strings.Fields(ansi.Strip(view)), " ")
}

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
