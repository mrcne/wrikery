package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestDatesFrom(t *testing.T) {
	if d := datesFrom("", ""); d.Type != "Backlog" {
		t.Errorf("empty -> %+v", d)
	}
	if d := datesFrom("", "2026-09-12"); d.Type != "Planned" || d.Start != "2026-09-12" || d.Due != "2026-09-12" {
		t.Errorf("due only -> %+v", d)
	}
	if d := datesFrom("2026-09-08", "2026-09-12"); d.Type != "Planned" || d.Start != "2026-09-08" {
		t.Errorf("both -> %+v", d)
	}
}

func TestDatesDialogSubmitsQuickWord(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	task := store.Task{ID: "T1", Dates: &store.TaskDates{Type: "Planned", Start: "2026-09-01", Due: "2026-09-02"}}
	d, _ := newDatesDialog(task, now)
	var dl dialog = d
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "+3d" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	got, ok := msgs[0].(submitDatesMsg)
	if !ok || got.dates.Type != "Planned" || got.dates.Due != "2026-09-06" {
		t.Errorf("submit = %#v", msgs)
	}
}

// TestDatesDialogPrefillsTimedDatesAsDateOnly covers a task whose dates carry a time part, which
// parseDate rejects, so prefilling the raw stored value would block a save until both fields are retyped.
func TestDatesDialogPrefillsTimedDatesAsDateOnly(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	task := store.Task{ID: "T1", Dates: &store.TaskDates{
		Type: "Planned", Start: "2026-09-02T09:00:00", Due: "2026-09-02T09:00:00",
	}}
	d, _ := newDatesDialog(task, now)
	if got := d.inputs[0].Value(); got != "2026-09-02" {
		t.Errorf("start prefill = %q, want 2026-09-02", got)
	}
	if got := d.inputs[1].Value(); got != "2026-09-02" {
		t.Errorf("due prefill = %q, want 2026-09-02", got)
	}
	var dl dialog = d
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) == 0 {
		t.Fatalf("Update on enter emitted nothing, want submitDatesMsg")
	}
	got, ok := msgs[0].(submitDatesMsg)
	if !ok || got.dates.Start != "2026-09-02" || got.dates.Due != "2026-09-02" {
		t.Errorf("submit = %#v, want a same-day Planned range with the time part dropped", msgs)
	}
}

func TestDatesDialogRejectsDueBeforeStart(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	task := store.Task{ID: "T1", Dates: &store.TaskDates{Type: "Planned", Start: "2026-09-01", Due: "2026-09-02"}}
	d, _ := newDatesDialog(task, now)
	var dl dialog = d
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "2026-08-01" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	dl, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("due before start emitted %#v, want nothing", msgs)
	}
	if dd, ok := dl.(datesDialog); !ok || dd.errText == "" {
		t.Errorf("errText not set, dialog = %#v", dl)
	}
}

// A due date is ahead, so a bare weekday in the dates box stays the coming one while the time entry box reads the last one.
func TestDatesDialogReadsAWeekdayAsTheComingOne(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) // Thursday
	d, _ := newDatesDialog(store.Task{ID: "T1"}, now)
	var dl dialog = d
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "mon" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) == 0 {
		t.Fatal("enter emitted nothing, want submitDatesMsg")
	}
	if got, ok := msgs[0].(submitDatesMsg); !ok || got.dates.Due != "2026-09-07" {
		t.Errorf("submit = %#v, want the coming Monday", msgs)
	}
}

func TestDatesDialogShowsTheDaysItReads(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	task := store.Task{ID: "T1", Dates: &store.TaskDates{Type: "Planned", Start: "2026-09-01", Due: "2026-09-02"}}
	d, _ := newDatesDialog(task, now)
	view := squeeze(dialog(d).View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 80, 30))
	for _, want := range []string{"start 2026-09-01 Tue 1 Sep 2026", "due 2026-09-02 Wed 2 Sep 2026"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

// The list of forms in the parser's error is longer than the box is wide, it wraps instead of being cut at the edge.
func TestDatesDialogWrapsTheParserError(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	d, _ := newDatesDialog(store.Task{ID: "T1"}, now)
	var dl dialog = d
	for _, r := range "someday" {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 80, 30)
	if !strings.Contains(squeeze(view), "+3d or 12 sep") {
		t.Errorf("view cuts the error short:\n%s", view)
	}
}
