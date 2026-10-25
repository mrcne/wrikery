package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestActionsDialogListsTheTaskActionsWithTheirKeys(t *testing.T) {
	table := taskActionTable(defaultKeyMap())
	d, _ := newActionsDialog(store.Task{ID: "T1", Title: "Fix auth retry loop"}, table)
	if d.title != "Fix auth retry loop" || !d.list.single {
		t.Fatalf("dialog = %+v", d)
	}
	if len(d.list.rows) != len(table) {
		t.Fatalf("%d rows, want %d", len(d.list.rows), len(table))
	}
	for i, r := range d.list.rows {
		if r.label != table[i].label() || r.hint != table[i].key.Help().Key {
			t.Errorf("row %d = %q/%q, want %q/%q", i, r.label, r.hint, table[i].label(), table[i].key.Help().Key)
		}
	}
	// The box shows twelve rows at most, the copy rows at the end sit behind the marker until the filter narrows the list.
	view := d.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 80, 40)
	for _, want := range []string{"Actions on Fix auth retry loop", "comment", "importance", "type to narrow down", "enter run   esc cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestActionsDialogNarrowsAndRunsTheHighlightedAction(t *testing.T) {
	table := taskActionTable(defaultKeyMap())
	d, _ := newActionsDialog(store.Task{ID: "T1"}, table)
	var dl dialog = d
	// A letter is a filter, never a key: c keeps the comment and copy rows and runs nothing. The filter's cursor blink is not a run.
	dl, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if msgs := runs(collect(cmd)); len(msgs) != 0 {
		t.Errorf("c ran something: %#v", msgs)
	}
	ad := dl.(actionsDialog)
	if len(ad.list.visible) == 0 {
		t.Fatal("c left no row")
	}
	for _, ri := range ad.list.visible {
		if label := ad.list.rows[ri].label; !strings.HasPrefix(label, "c") {
			t.Errorf("c left %q visible", label)
		}
	}
	// The word a person types for the two editor actions is editor, the label's word starts with a dollar sign.
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "editor" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	ad = dl.(actionsDialog)
	if len(ad.list.visible) != 2 {
		t.Errorf("editor left %d rows, want the two $EDITOR actions", len(ad.list.visible))
	}
	for _, ri := range ad.list.visible {
		if label := ad.list.rows[ri].label; !strings.HasSuffix(label, "in $EDITOR") {
			t.Errorf("editor left %q visible", label)
		}
	}
	// "cu" starts no word of any action, enter has nothing to run and sends nothing at all.
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "cu" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("enter with no row sent something: %#v", msgs)
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "dat" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Unfiltered on purpose: the run has to be the only message, a closeDialogMsg sent with it would race the box the run opens.
	msgs := collect(cmd)
	want := -1
	for i, a := range table {
		if a.label() == "dates" {
			want = i
		}
	}
	if len(msgs) != 1 || msgs[0] != (runActionMsg{index: want}) {
		t.Errorf("enter on dates sent %#v, want runActionMsg{%d} alone", msgs, want)
	}
}

// runs keeps the runActionMsg values of a batch, the filter input's cursor blink travels in the same commands.
func runs(msgs []tea.Msg) []tea.Msg {
	var out []tea.Msg
	for _, m := range msgs {
		if _, ok := m.(runActionMsg); ok {
			out = append(out, m)
		}
	}
	return out
}
