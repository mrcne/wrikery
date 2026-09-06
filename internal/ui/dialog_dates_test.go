package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
