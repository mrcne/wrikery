package ui

import (
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

// A keystroke bumps seq and fires a runSearchMsg carrying it, so results tagged with a seq the
// model has since moved past are a stale answer to an earlier query and must be dropped, while
// results tagged with the current seq are the answer to the latest keystroke and must be applied.
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
