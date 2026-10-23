package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// A keystroke bumps seq and fires a runSearchMsg carrying it.
// Results tagged with a seq the model has since moved past are a stale answer to an earlier query and must be dropped, while results tagged with the current seq are the answer to the latest keystroke and must be applied.
func TestSearchResultsIgnoresStaleSeq(t *testing.T) {
	s := newSearch(defaultKeyMap())
	s.seq = 2
	current := []store.Task{{ID: "IEAATASK00", Title: "Fix auth retry loop"}}

	s, _ = s.Update(searchResultsMsg{seq: 1, tasks: []store.Task{{ID: "IEAATASK99", Title: "stale"}}})
	if len(s.results) != 0 {
		t.Fatalf("a result tagged with an older seq should be ignored, got %v", s.results)
	}

	s, _ = s.Update(searchResultsMsg{seq: 2, tasks: current})
	if len(s.results) != 1 || s.results[0].ID != "IEAATASK00" {
		t.Fatalf("a result tagged with the current seq should be applied, got %v", s.results)
	}
}

// The overlay leaves its input focused so a stray blink does not survive it closing.
func TestSearchBlurStopsTheCursor(t *testing.T) {
	s := newSearch(defaultKeyMap())
	_ = s.reset()
	if !s.input.Focused() {
		t.Fatal("reset should focus the input")
	}
	s.blur()
	if s.input.Focused() {
		t.Error("blur should leave the input unfocused")
	}
}

// Pick mode is how the timesheet's "+ new task" row reuses search to choose a task instead of
// opening it, so enter has to hand the pick back rather than jump the screen to the task.
func TestSearchPickModeEmitsSearchPickMsg(t *testing.T) {
	s := newSearch(defaultKeyMap())
	s.pickMode = true
	s.results = []store.Task{{ID: "IEAATASK00", Title: "Fix auth retry loop"}}

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter in pick mode should emit exactly one message: %#v", msgs)
	}
	pick, ok := msgs[0].(searchPickMsg)
	if !ok || pick.task.ID != "IEAATASK00" {
		t.Errorf("got %#v, want a searchPickMsg for IEAATASK00, not a searchOpenMsg", msgs[0])
	}

	_ = s.reset()
	if s.pickMode {
		t.Error("reset should clear pickMode, or a plain search after a cancelled pick would stay in pick mode")
	}
}

func TestSearchMaxRowsCapsAndFloors(t *testing.T) {
	cases := map[int]int{40: 12, 20: 12, 14: 6, 8: 3, 0: 3}
	for height, want := range cases {
		if got := searchMaxRows(height); got != want {
			t.Errorf("searchMaxRows(%d) = %d, want %d", height, got, want)
		}
	}
}

// A short terminal fed a full 30-result page must still draw a box that fits it, not one that overflows past the bottom of the screen.
func TestSearchViewFitsAShortTerminal(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	s := newSearch(defaultKeyMap())
	for i := 0; i < 30; i++ {
		s.results = append(s.results, store.Task{ID: "IEAATASK00", Title: "Fix auth retry loop"})
	}
	const height = 14
	s.visible = searchMaxRows(height)
	out := s.View(th, refData{statuses: map[string]store.CustomStatus{}}, 80)
	if h := lipgloss.Height(out); h > height {
		t.Errorf("search overlay is %d rows tall, does not fit a %d row terminal:\n%s", h, height, out)
	}
}

// A query shaped like a task reference the cache lacks gets one more row, last, that fetches the task from Wrike on enter.
func TestSearchShowsTheReachRowAndAsksOnEnter(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	s := newSearch(defaultKeyMap())
	s.seq = 1
	s, _ = s.Update(searchResultsMsg{seq: 1, ref: "MAAAAAEPXpuV"})
	if v := s.View(th, testWorkflows(), 60); !strings.Contains(v, "open from Wrike: MAAAAAEPXpuV") || strings.Contains(v, "no matches") {
		t.Errorf("view without hits:\n%s", v)
	}
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := reachMsg{ref: store.TaskRef{Kind: store.RefID, Value: "MAAAAAEPXpuV"}, text: "MAAAAAEPXpuV", seq: 1, origin: reachFromSearch}
	if len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("enter on the row sent %#v, want %#v", msgs, want)
	}
	s.fetching = true
	if v := s.View(th, testWorkflows(), 60); !strings.Contains(v, "fetching from Wrike...") {
		t.Errorf("view while fetching:\n%s", v)
	}
	if _, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Error("a second enter while fetching sent something")
	}
	s.reachFailed("MAAAAAEPXpuV", "no task with this id on Wrike, or no access")
	if v := s.View(th, testWorkflows(), 60); !strings.Contains(v, "no task with this id on Wrike, or no access") || s.fetching {
		t.Errorf("view after the failure:\n%s", v)
	}
	s.seq = 2
	s, _ = s.Update(searchResultsMsg{seq: 2})
	if v := s.View(th, testWorkflows(), 60); strings.Contains(v, "open from Wrike") || strings.Contains(v, "no access") {
		t.Errorf("the row survived a new query:\n%s", v)
	}
}

func TestSearchKeepsTheTitleHitsAboveTheReachRow(t *testing.T) {
	s := newSearch(defaultKeyMap())
	s.seq = 1
	s, _ = s.Update(searchResultsMsg{seq: 1, ref: "ReleaseNotes", tasks: []store.Task{
		{ID: "IEAATASK00", Title: "Release notes"}, {ID: "IEAATASK01", Title: "Release notes, again"}}})
	down := tea.KeyMsg{Type: tea.KeyCtrlN}
	s, _ = s.Update(down)
	s, _ = s.Update(down)
	s, _ = s.Update(down)
	if s.cursor != 2 {
		t.Fatalf("cursor = %d, want the third row, the reach row, and no further", s.cursor)
	}
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 1 || msgs[0].(reachMsg).ref != (store.TaskRef{Kind: store.RefID, Value: "ReleaseNotes"}) {
		t.Errorf("enter on the last row sent %#v", msgs)
	}
	s.cursor = 0
	_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 1 || msgs[0].(openTaskMsg).task.ID != "IEAATASK00" {
		t.Errorf("enter on the first hit sent %#v", msgs)
	}
}

func TestSearchPickModeReachesWithThePickOrigin(t *testing.T) {
	s := newSearch(defaultKeyMap())
	s.pickMode, s.seq = true, 1
	s, _ = s.Update(searchResultsMsg{seq: 1, ref: "4552825749"})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	want := reachMsg{ref: store.TaskRef{Kind: store.RefNumber, Value: "4552825749"}, text: "4552825749", seq: 1, origin: reachFromPick}
	if msgs := collect(cmd); len(msgs) != 1 || msgs[0] != want {
		t.Errorf("enter in pick mode sent %#v", msgs)
	}
}

// With more hits than the box shows, the reach row stays on screen under the count of the hidden ones,
// and the cursor reaches it right after the last visible hit.
func TestSearchKeepsTheReachRowVisibleBehindManyHits(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	s := newSearch(defaultKeyMap())
	s.seq, s.visible = 1, 4
	var hits []store.Task
	for i := range 10 {
		hits = append(hits, store.Task{ID: fmt.Sprintf("IEAATASK%02d", i), Title: fmt.Sprintf("Release %d", i)})
	}
	s, _ = s.Update(searchResultsMsg{seq: 1, ref: "2026", tasks: hits})
	v := ansi.Strip(s.View(th, testWorkflows(), 60))
	lines := strings.Split(v, "\n")
	var more, row int
	for i, l := range lines {
		if strings.Contains(l, "... 7 more") {
			more = i
		}
		if strings.Contains(l, "open from Wrike: 2026") {
			row = i
		}
	}
	if more == 0 || row != more+1 || strings.Contains(v, "Release 3") {
		t.Errorf("want three hits, the count of the hidden ones and the reach row on the next line:\n%s", v)
	}
	down := tea.KeyMsg{Type: tea.KeyCtrlN}
	for range 5 {
		s, _ = s.Update(down)
	}
	if s.cursor != 3 {
		t.Fatalf("cursor = %d, want the reach row right after the third hit", s.cursor)
	}
	if _, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter}); len(collect(cmd)) != 1 {
		t.Error("enter on the reach row sent nothing")
	}
}
