package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
)

func sampleNodes() []treeNode {
	return []treeNode{
		{id: "me", title: "My tasks", kind: nodeMe, count: 12},
		{id: "S1", title: "Platform", kind: nodeSpace, children: []int{2, 3}, expanded: true},
		{id: "P1", title: "API", kind: nodeProject, depth: 1, statusGroup: "Active"},
		{id: "F1", title: "Infra", kind: nodeFolder, depth: 1, children: []int{4}},
		{id: "F2", title: "On-call", kind: nodeFolder, depth: 2},
		{id: "S2", title: "Mobile", kind: nodeSpace, children: []int{6}},
		{id: "P2", title: "Android app", kind: nodeProject, depth: 1, statusGroup: "Active"},
		{id: "F9", title: "Archive", kind: nodeFolder},
	}
}

func TestSidebarVisibleFollowsExpansion(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	ids := func() []string {
		var out []string
		for _, i := range s.visible {
			out = append(out, s.nodes[i].id)
		}
		return out
	}
	if got := ids(); !reflect.DeepEqual(got, []string{"me", "S1", "P1", "F1", "S2", "F9"}) {
		t.Fatalf("visible = %v", got)
	}
	s.cursor = 3 // Infra
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if got := ids(); !reflect.DeepEqual(got, []string{"me", "S1", "P1", "F1", "F2", "S2", "F9"}) {
		t.Errorf("after expand = %v", got)
	}
	s.cursor = 4 // On-call, a leaf
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if n, _ := s.current(); n.id != "F1" {
		t.Errorf("h on a leaf should jump to the parent, cursor on %s", n.id)
	}
}

func TestSidebarSetNodesKeepsSelectionByID(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	s.cursor = 2 // API
	nodes := sampleNodes()
	nodes = append(nodes[:1], append([]treeNode{{id: "S0", title: "Aaa", kind: nodeSpace}}, nodes[1:]...)...)
	// indexes shifted by one for every child reference
	for i := range nodes {
		for j := range nodes[i].children {
			nodes[i].children[j]++
		}
	}
	s.setNodes(nodes)
	if n, _ := s.current(); n.id != "P1" {
		t.Errorf("selection moved to %s", n.id)
	}
}

func TestSidebarCrumbJoinsAncestorTitles(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	if got := s.crumb(s.nodes[4]); got != "Platform / Infra / On-call" {
		t.Errorf("crumb = %q, want %q", got, "Platform / Infra / On-call")
	}
	if got := s.crumb(s.nodes[1]); got != "Platform" {
		t.Errorf("crumb of a root = %q, want %q", got, "Platform")
	}
}

func flatNodes(n int) []treeNode {
	nodes := make([]treeNode, n)
	for i := range nodes {
		nodes[i] = treeNode{id: fmt.Sprintf("n%d", i), title: fmt.Sprintf("Node %d", i), kind: nodeFolder}
	}
	return nodes
}

func TestSidebarScrollsToKeepTheCursorVisible(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.height = 5
	s.setNodes(flatNodes(12))
	for range 8 {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if s.offset != 4 {
		t.Fatalf("offset after 8 down = %d, want 4", s.offset)
	}
	cur, _ := s.current()
	if out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 28, 5, true); !strings.Contains(out, cur.title) {
		t.Errorf("view lacks the cursor row %q:\n%s", cur.title, out)
	}
	for range 6 {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	}
	if s.offset != 2 {
		t.Errorf("offset after 6 up = %d, want 2", s.offset)
	}
}

func TestSidebarViewMarksCursorAndGlyphs(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	out := s.View(th, 28, 10, true)
	for _, want := range []string{"> My tasks", "12", "v Platform", "  o API", "> Infra", "> Mobile", "    Archive"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "> Archive") {
		t.Errorf("Archive has no children, it should not draw a chevron:\n%s", out)
	}
}

// Twelve folders in a pane of eight rows, two names wider than the pane and one of them in view.
// A wrapped name used to take two lines, so the pane overflowed and the box cut the cursor row off the bottom.
func TestSidebarDrawsOneLinePerNodeWhenNamesAreLong(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.height = 8
	nodes := flatNodes(12)
	nodes[3].title = "A folder with a name far too long for the sidebar pane"
	nodes[10].title = "Another folder with a name far too long for the pane"
	s.setNodes(nodes)
	for range 11 {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 28, 8, true)
	if got := strings.Count(out, "\n") + 1; got != 8 {
		t.Errorf("view has %d lines, want 8:\n%s", got, out)
	}
	if !strings.Contains(out, "Node 11") {
		t.Errorf("view lacks the cursor row:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 28 {
			t.Errorf("line is %d cells wide, want at most 28: %q", w, line)
		}
	}
}

func TestSidebarCutsLongNamesInTheMiddle(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	nodes := flatNodes(2)
	nodes[1].title = "Backend: Kafka processing pipeline"
	s.setNodes(nodes)
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 24, 2, true)
	// prefix 2, marker 2, 20 cells for the name: 17 of text, 12 at the start and 5 at the end.
	if !strings.Contains(out, "Backend: Kaf...eline") {
		t.Errorf("view lacks the middle cut name:\n%s", out)
	}
}

func TestSidebarHidesConfiguredPrefixes(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	nodes := flatNodes(2)
	nodes[1].title = "(MX) Backend"
	s.setNodes(nodes)
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true, HidePrefixes: []string{"(MX)"}}), 24, 2, true)
	if strings.Contains(out, "(MX)") || !strings.Contains(out, " Backend") {
		t.Errorf("view should drop the configured prefix:\n%s", out)
	}
}

func TestFitName(t *testing.T) {
	cases := []struct {
		name  string
		width int
		want  string
	}{
		{"Platform", 22, "Platform"},
		{"Exactly twenty-two ch.", 22, "Exactly twenty-two ch."},
		{"Backend: Kafka processing pipeline", 22, "Backend: Kafk...peline"},
		{"Backend: Kafka processing pipeline", 6, "Bac..."},
		// Wide characters are cut by cell, never through the middle of one.
		{"\u65e5\u672c\u8a9e\u306e\u9577\u3044\u30d5\u30a9\u30eb\u30c0\u540d", 11, "\u65e5\u672c\u8a9e...\u540d"},
	}
	for _, c := range cases {
		if got := fitName(c.name, c.width); got != c.want {
			t.Errorf("fitName(%q, %d) = %q, want %q", c.name, c.width, got, c.want)
		}
		if w := lipgloss.Width(fitName(c.name, c.width)); w > c.width {
			t.Errorf("fitName(%q, %d) is %d cells wide", c.name, c.width, w)
		}
	}
}

func TestRowLineKeepsOneLineAndOneStyle(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	long := strings.Repeat("word ", 12)
	if out := rowLine(th, long, 20, false, false); strings.Contains(out, "\n") || lipgloss.Width(out) != 20 {
		t.Errorf("a long label should be cut to one line of 20 cells, got %d cells:\n%s", lipgloss.Width(out), out)
	}
	// A part the caller styled on its own ends in a reset, which would end the selection background mid row.
	styled := "\x1b[31mo\x1b[0m API"
	if out := rowLine(th, styled, 20, true, true); strings.Contains(out, "\x1b[31m") {
		t.Errorf("a selected row should drop the styling of its parts:\n%q", out)
	}
	if out := rowLine(th, styled, 20, false, true); !strings.Contains(out, "\x1b[31m") {
		t.Errorf("an unselected row keeps the styling of its parts:\n%q", out)
	}
}

func typeKeys(s sidebarModel, keys string) sidebarModel {
	for _, r := range keys {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return s
}

func visibleIDs(s sidebarModel) []string {
	var out []string
	for _, i := range s.visible {
		out = append(out, s.nodes[i].id)
	}
	return out
}

// Infra is collapsed and hides On-call. The query opens the path to the match, parks the cursor on the match
// and not on its ancestors, and esc puts the tree back with the match still selected.
func TestSidebarFilterShowsMatchesWithTheirAncestors(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	s = typeKeys(s, "/on")
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"S1", "F1", "F2"}) {
		t.Fatalf("visible under the query = %v", got)
	}
	if n, _ := s.current(); n.id != "F2" {
		t.Errorf("cursor on %s, want the match F2", n.id)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "P1", "F1", "F2", "S2", "F9"}) {
		t.Errorf("visible after esc = %v, want the whole tree with Infra opened", got)
	}
	if n, _ := s.current(); n.id != "F2" || s.filtering || s.filter.Value() != "" {
		t.Errorf("after esc the cursor is on %s with filtering=%v value=%q", n.id, s.filtering, s.filter.Value())
	}
}

func TestSidebarFilterEnterKeepsTheQuery(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	s = typeKeys(s, "/ap")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// API and Android app, each under its space, Mobile opened by the query.
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"S1", "P1", "S2", "P2"}) {
		t.Fatalf("visible after enter = %v", got)
	}
	if s.filtering {
		t.Fatal("enter should leave the input")
	}
	s = typeKeys(s, "G")
	if n, _ := s.current(); n.id != "P2" {
		t.Errorf("G after enter lands on %s, want P2: the keys move again", n.id)
	}
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 28, 4, true)
	if lines := strings.Split(out, "\n"); len(lines) != 4 || !strings.Contains(lines[3], "> ap") {
		t.Errorf("the query should stay on the last line:\n%s", out)
	}
}

func TestSidebarRevealExpandsCollapsedAncestors(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	if !s.reveal("F2") {
		t.Fatal("reveal returned false for a node in the tree")
	}
	if n, _ := s.current(); n.id != "F2" || !s.nodes[3].expanded {
		t.Errorf("after reveal the cursor is on %s and Infra expanded=%v", n.id, s.nodes[3].expanded)
	}
	if s.reveal("nope") {
		t.Error("reveal returned true for an unknown id")
	}
}

func TestSidebarPinsNarrowTheTree(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	s.cursor = 2 // API
	var cmd tea.Cmd
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("pinning should ask the root to remember it")
	}
	if msg, ok := cmd().(pinChangedMsg); !ok || msg.id != "P1" || !msg.pinned {
		t.Fatalf("pin message = %#v", cmd())
	}
	s.cursor = 3 // Infra, collapsed
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "P1", "F1"}) {
		t.Fatalf("pinned only = %v", got)
	}
	if msg, ok := cmd().(pinnedOnlyMsg); !ok || !msg.on {
		t.Errorf("toggle message = %#v", cmd())
	}
	// What sits under a pinned node follows the expand state as usual.
	s = typeKeys(s, "l")
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "P1", "F1", "F2"}) {
		t.Errorf("pinned only with Infra expanded = %v", got)
	}
	// The filter narrows the pinned tree, Mobile is outside it.
	s = typeKeys(s, "/mob")
	if got := visibleIDs(s); len(got) != 0 {
		t.Errorf("mob in pinned only = %v, want nothing", got)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 28, 6, true)
	if !strings.Contains(out, "API *") || !strings.Contains(out, "Infra *") || strings.Contains(out, "Platform *") {
		t.Errorf("pinned rows carry the mark:\n%s", out)
	}
	// Space on a pinned row unpins it, and the row leaves the pinned tree.
	s.selectByID("P1")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "F1", "F2"}) {
		t.Errorf("after unpinning API = %v", got)
	}
}

func TestSidebarPinnedOnlyWithNothingPinnedSaysSo(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if s.pinnedOnly || cmd == nil {
		t.Fatalf("pinnedOnly=%v cmd=%v, want the toggle refused with a toast", s.pinnedOnly, cmd)
	}
	if _, ok := cmd().(toastMsg); !ok {
		t.Errorf("message = %#v, want a toast", cmd())
	}
	// My tasks cannot be pinned, it is not a folder.
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil || len(s.pinned) != 0 {
		t.Errorf("space on My tasks pinned %v", s.pinned)
	}
}

func TestSidebarRevealLeavesPinnedOnlyForANodeOutsideIt(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setNodes(sampleNodes())
	s.setPins([]string{"P1"}, true)
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "P1"}) {
		t.Fatalf("pinned only from the store = %v", got)
	}
	if !s.reveal("P2") || s.pinnedOnly {
		t.Errorf("reveal of Android app: pinnedOnly=%v", s.pinnedOnly)
	}
	if n, _ := s.current(); n.id != "P2" {
		t.Errorf("cursor on %s, want P2", n.id)
	}
}
