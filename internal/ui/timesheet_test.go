package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestWeekOf(t *testing.T) {
	for in, want := range map[string]string{"2026-09-03": "2026-08-31", "2026-08-31": "2026-08-31", "2026-09-06": "2026-08-31", "2026-09-07": "2026-09-07"} {
		d, _ := time.Parse("2006-01-02", in)
		if got := weekOf(d).Format("2006-01-02"); got != want {
			t.Errorf("weekOf(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestTimesheetGridTotalsAndCursor(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	var ts timesheetModel
	ts.keys = defaultKeyMap()
	ts.set(weekLoadedMsg{
		weekStart: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		logs: []store.Timelog{
			{ID: "a", TaskID: "T1", TrackedDate: "2026-08-31", Hours: 2},
			{ID: "b", TaskID: "T1", TrackedDate: "2026-09-01", Hours: 1.5},
			{ID: "c", TaskID: "T2", TrackedDate: "2026-09-02", Hours: 4, LockStatus: "Locked"},
			{ID: "d", TaskID: "T2", TrackedDate: "2026-09-02", Hours: 0.5},
			{ID: "local:9", TaskID: "T1", TrackedDate: "2026-09-04", Hours: 1},
		},
		titles: map[string]string{"T1": "Fix auth retry loop", "T2": "Rotate signing keys"},
	})
	if len(ts.rows) != 2 || ts.rows[0].title != "Fix auth retry loop" {
		t.Fatalf("rows = %+v", ts.rows)
	}
	out := ts.View(th, 100, 12)
	for _, want := range []string{"Mon", "Sun", "Total", "2.0", "1.5", "4.5", "~1.0", "Fix auth retry loop", "9.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("grid lacks %q:\n%s", want, out)
		}
	}
	if ts.title() != "Timesheet: 31 Aug - 6 Sep 2026" {
		t.Errorf("title = %q", ts.title())
	}
	ts, _ = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	ts, _ = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	ts, _ = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	_, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("e on a two entry cell should ask which one: %#v", msgs)
	}
	if pick, ok := msgs[0].(pickEntryMsg); !ok || len(pick.logs) != 2 {
		t.Errorf("got %#v", msgs[0])
	}
	_, cmd = ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if msgs := collect(cmd); len(msgs) != 1 || msgs[0].(loadWeekMsg).start.Format("2006-01-02") != "2026-09-07" {
		t.Errorf("] -> %#v", msgs)
	}
}
