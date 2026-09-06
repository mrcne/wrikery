package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

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
	out := s.View(th, refData{statuses: map[string]store.CustomStatus{}}, 80, searchMaxRows(height))
	if h := lipgloss.Height(out); h > height {
		t.Errorf("search overlay is %d rows tall, does not fit a %d row terminal:\n%s", h, height, out)
	}
}
