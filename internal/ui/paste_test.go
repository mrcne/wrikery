package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A paste is text for the field that has the keys, also when the text is the name of a key.
func TestPasteGoesIntoTheFieldThatHasTheKeys(t *testing.T) {
	m := demoModel(t)
	m = pump(m, typed("e"))
	d, ok := m.dialog.(titleDialog)
	if !ok {
		t.Fatalf("e opened %T, want the title box", m.dialog)
	}
	want := d.input.Value()
	for _, text := range []string{" and the login page", "enter", "q", " "} {
		m = pump(m, tea.PasteMsg{Content: text})
		want += text
		d, ok = m.dialog.(titleDialog)
		if !ok {
			t.Fatalf("the paste of %q closed the box, the dialog is %T", text, m.dialog)
		}
		if got := d.input.Value(); got != want {
			t.Fatalf("after the paste of %q the field holds %q, want %q", text, got, want)
		}
	}
}

func TestPasteKeepsTheLinesOfAComment(t *testing.T) {
	m := demoModel(t)
	m = pump(m, typed("c"))
	if _, ok := m.dialog.(commentDialog); !ok {
		t.Fatalf("c opened %T, want the comment box", m.dialog)
	}
	m = pump(m, tea.PasteMsg{Content: "first line\nsecond line"})
	if got := m.dialog.(commentDialog).ta.Value(); got != "first line\nsecond line" {
		t.Errorf("the comment holds %q", got)
	}
}

// Space ticks a row of a checklist, a pasted name with a space in it only narrows the list.
func TestPasteWithASpaceTicksNothingInAChecklist(t *testing.T) {
	m := demoModel(t)
	m = pump(m, typed("a"))
	if _, ok := m.dialog.(assigneeDialog); !ok {
		t.Fatalf("a opened %T, want the assignee box", m.dialog)
	}
	m = pump(m, tea.PasteMsg{Content: "ada nowak"})
	list := m.dialog.(assigneeDialog).list
	if got := list.filter.Value(); got != "ada nowak" {
		t.Errorf("the filter holds %q", got)
	}
	if add, remove := list.diff(); len(add)+len(remove) != 0 {
		t.Errorf("the paste ticked rows: add %v, remove %v", add, remove)
	}
}

// Without a field a paste does nothing: typed, its letters would run whatever they are bound to.
func TestPasteWithoutAFieldIsDropped(t *testing.T) {
	m := demoModel(t)
	for _, text := range []string{"q", "x", "enter", "some words"} {
		next, cmd := m.Update(tea.PasteMsg{Content: text})
		if cmd != nil || next.(Model).overlay != overlayNone {
			t.Errorf("a paste of %q on the main screen ran a key", text)
		}
	}
	// The status box has the keys and no field, j would move its cursor.
	m = pump(m, typed("s"))
	d, ok := m.dialog.(statusDialog)
	if !ok {
		t.Fatalf("s opened %T, want the status box", m.dialog)
	}
	m = pump(m, tea.PasteMsg{Content: "j"})
	if got := m.dialog.(statusDialog).cursor; got != d.cursor {
		t.Errorf("a paste moved the cursor of the status box from %d to %d", d.cursor, got)
	}
}
