package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	s.setTree(sampleNodes(), nil)
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
	s.setTree(sampleNodes(), nil)
	s.cursor = 2 // API
	nodes := sampleNodes()
	nodes = append(nodes[:1], append([]treeNode{{id: "S0", title: "Aaa", kind: nodeSpace}}, nodes[1:]...)...)
	// indexes shifted by one for every child reference
	for i := range nodes {
		for j := range nodes[i].children {
			nodes[i].children[j]++
		}
	}
	s.setTree(nodes, nil)
	if n, _ := s.current(); n.id != "P1" {
		t.Errorf("selection moved to %s", n.id)
	}
}

func TestSidebarCrumbJoinsAncestorTitles(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), nil)
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
	s.setTree(flatNodes(12), nil)
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
	s.setTree(sampleNodes(), nil)
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
	s.setTree(nodes, nil)
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
	s.setTree(nodes, nil)
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 24, 2, true)
	// prefix 2, marker 2, 20 cells for the name: 17 of text, 12 at the start and 5 at the end.
	if !strings.Contains(out, "Backend: Kaf...eline") {
		t.Errorf("view lacks the middle cut name:\n%s", out)
	}
}

func TestSidebarHidesConfiguredPrefixes(t *testing.T) {
	s := newSidebar(defaultKeyMap(), []string{"(MX)"})
	nodes := flatNodes(2)
	nodes[1].title = "(MX) Backend"
	s.setTree(nodes, nil)
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 24, 2, true)
	if strings.Contains(out, "(MX)") || !strings.Contains(out, " Backend") {
		t.Errorf("view should drop the configured prefix:\n%s", out)
	}
	// The filter matches what the row shows, so the hidden prefix finds nothing.
	s = typeKeys(s, "/mx")
	if got := visibleIDs(s); len(got) != 0 {
		t.Errorf("mx matched %v through a hidden prefix", got)
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
	// The row style is opened again behind it, so the part keeps its color and the background runs to the end.
	styled := "\x1b[31mo\x1b[0m API"
	if out := rowLine(th, styled, 20, false, true); !strings.Contains(out, "\x1b[31m") {
		t.Errorf("an unselected row keeps the styling of its parts:\n%q", out)
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	out := rowLine(th, styled, 20, true, true)
	if !strings.Contains(out, "\x1b[31m") {
		t.Errorf("a selected row keeps the styling of its parts:\n%q", out)
	}
	after := out[strings.Index(out, "\x1b[0m")+len("\x1b[0m"):]
	if !strings.Contains(after, "48;2;") {
		t.Errorf("the background should be set again after the part's reset:\n%q", out)
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
	s.setTree(sampleNodes(), nil)
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
	s.setTree(sampleNodes(), nil)
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
	if lines := strings.Split(out, "\n"); len(lines) != 4 || !strings.Contains(lines[3], "/ap") {
		t.Errorf("the query should stay on the last line:\n%s", out)
	}
}

func TestSidebarRevealExpandsCollapsedAncestors(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), nil)
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
	s.setTree(sampleNodes(), nil)
	s.cursor = 2 // API
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if got := s.takeChanges(); !reflect.DeepEqual(got, []pinChange{{id: "P1", on: true}}) {
		t.Fatalf("changes after pinning API = %+v", got)
	}
	s.cursor = 3 // Infra, collapsed
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "P1", "F1"}) {
		t.Fatalf("pinned only = %v", got)
	}
	if got := s.takeChanges(); !reflect.DeepEqual(got, []pinChange{{id: "F1", on: true}, {on: true}}) {
		t.Errorf("changes after pinning Infra and the toggle = %+v", got)
	}
	if got := s.takeChanges(); len(got) != 0 {
		t.Errorf("changes are handed over once, got %+v again", got)
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
	// Space on a pinned row unpins it, the row leaves the pinned tree and the cursor goes to its parent, not to the top.
	s.selectByID("P1")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "F1", "F2"}) {
		t.Errorf("after unpinning API = %v", got)
	}
	if n, _ := s.current(); n.id != "S1" {
		t.Errorf("after unpinning API the cursor is on %s, want its parent S1", n.id)
	}
	// Unpinning the last pin turns the toggle off, or My tasks would sit alone in the pane.
	s.selectByID("F1")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if s.pinnedOnly || len(visibleIDs(s)) != 7 {
		t.Errorf("after the last unpin pinnedOnly=%v visible=%v", s.pinnedOnly, visibleIDs(s))
	}
	if got := s.takeChanges(); !reflect.DeepEqual(got, []pinChange{{id: "P1"}, {id: "F1"}, {}}) {
		t.Errorf("changes after the two unpins = %+v", got)
	}
}

func TestSidebarPinnedOnlyWithNothingPinnedSaysSo(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), nil)
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if s.pinnedOnly || cmd == nil {
		t.Fatalf("pinnedOnly=%v cmd=%v, want the toggle refused with a toast", s.pinnedOnly, cmd)
	}
	if _, ok := cmd().(toastMsg); !ok {
		t.Errorf("message = %#v, want a toast", cmd())
	}
	// My tasks cannot be pinned, it is not a folder.
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil || len(s.pinned) != 0 || len(s.takeChanges()) != 0 {
		t.Errorf("space on My tasks pinned %v", s.pinned)
	}
}

// A pin whose folder left the followed tree is not a pin the pane can show, so it counts for nothing:
// the toggle stays off and P says there is nothing pinned, instead of a Pinned pane holding My tasks alone.
func TestSidebarIgnoresPinsOutsideTheTree(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), &pinState{ids: []string{"GONE"}, only: true})
	if s.pinnedOnly || len(visibleIDs(s)) != 6 {
		t.Fatalf("pinnedOnly=%v visible=%v with a pin outside the tree", s.pinnedOnly, visibleIDs(s))
	}
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if s.pinnedOnly || cmd == nil {
		t.Errorf("P with only a stale pin: pinnedOnly=%v cmd=%v, want the toast", s.pinnedOnly, cmd)
	}
}

// Expansion lives in memory, so after a restart every pin below the first level sits behind a collapsed parent.
// The pinned view opens the way to its pins the way a query opens the way to its matches.
func TestSidebarPinnedOnlyOpensTheWayToAPin(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), &pinState{ids: []string{"F2"}, only: true})
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"me", "S1", "F1", "F2"}) {
		t.Fatalf("pinned only with On-call pinned = %v", got)
	}
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 28, 6, true)
	if !strings.Contains(out, "v Infra") {
		t.Errorf("Infra draws its children, so it should show the expanded chevron:\n%s", out)
	}
}

// The tree was scrolled down, the query leaves three rows for the three lines above the input,
// and the window has to come back up or the ancestors are above it.
func TestSidebarFilterKeepsTheAncestorsInView(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setSize(28, 4)
	s.setTree(sampleNodes(), nil)
	s = typeKeys(s, "G/on")
	if s.offset != 0 {
		t.Errorf("offset = %d, want 0 so Platform stays in view", s.offset)
	}
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 28, 4, true)
	if !strings.Contains(out, "Platform") || !strings.Contains(out, "v Infra") {
		t.Errorf("view under the query:\n%s", out)
	}
	if lines := strings.Split(out, "\n"); !strings.HasPrefix(lines[len(lines)-1], "/on") {
		t.Errorf("the input uses the / prompt of the task list, got %q", lines[len(lines)-1])
	}
}

func TestSidebarRevealOfAnUnknownIDChangesNothing(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), nil)
	s = typeKeys(s, "/ap")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s.selectByID("P1")
	if s.reveal("ZZZ") {
		t.Fatal("reveal of an unknown id returned true")
	}
	if n, _ := s.current(); n.id != "P1" || s.filter.Value() != "ap" {
		t.Errorf("after a failed reveal the cursor is on %s with query %q, want P1 and ap", n.id, s.filter.Value())
	}
}

func TestSidebarEscWithNoMatchGoesBackToTheNodeBefore(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), nil)
	s.selectByID("P1")
	s = typeKeys(s, "/zzz")
	if _, ok := s.current(); ok {
		t.Fatal("zzz should match nothing")
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if n, _ := s.current(); n.id != "P1" {
		t.Errorf("after esc the cursor is on %s, want P1", n.id)
	}
}

func TestSidebarFilterInputScrollsALongQuery(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setSize(16, 4)
	s.setTree(sampleNodes(), nil)
	s = typeKeys(s, "/abcdefghijklmnopqrstuvwxyz")
	out := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 16, 4, true)
	lines := strings.Split(out, "\n")
	last := lines[len(lines)-1]
	if lipgloss.Width(last) > 16 || !strings.Contains(last, "xyz") {
		t.Errorf("the input line is %d cells and reads %q, want at most 16 with the end of the query", lipgloss.Width(last), last)
	}
}

// P on a row outside the pins keeps the cursor near where it was, on a drawn ancestor or at the same height, not on My tasks.
func TestSidebarPinnedOnlyKeepsTheCursorNearby(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), &pinState{ids: []string{"P1"}})
	s.selectByID("F1") // Infra, under Platform which leads to the pin
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if n, _ := s.current(); n.id != "S1" {
		t.Errorf("P with the cursor on Infra lands on %s, want its parent S1", n.id)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	s.selectByID("F9") // Archive, a root with no pinned relative
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if n, _ := s.current(); n.id != "P1" {
		t.Errorf("P with the cursor on Archive lands on %s, want the last row P1", n.id)
	}
}

func TestSidebarRevealKeepsAFilterTheNodeMatches(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), nil)
	s = typeKeys(s, "/ap")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.reveal("P2") || s.filter.Value() != "ap" {
		t.Errorf("reveal of a match should keep the query, got %q", s.filter.Value())
	}
	if n, _ := s.current(); n.id != "P2" {
		t.Errorf("cursor on %s, want P2", n.id)
	}
	if !s.reveal("F2") || s.filter.Value() != "" {
		t.Errorf("reveal of a node the query hides should clear it, got %q", s.filter.Value())
	}
	if n, _ := s.current(); n.id != "F2" {
		t.Errorf("cursor on %s, want F2", n.id)
	}
}

func TestSidebarWidthIgnoresTheFilterAndHiddenPrefixes(t *testing.T) {
	s := newSidebar(defaultKeyMap(), []string{"(MX)"})
	nodes := sampleNodes()
	nodes[4].title = "(MX) On-call rota for the platform team" // under collapsed Infra
	s.setTree(nodes, nil)
	// Without the prefix the name is 35 cells, with depth 2 and the chrome that is over the cap, so the cap applies.
	if w := s.width(); w != 32 {
		t.Fatalf("width = %d, want the cap of 32 from the collapsed long name", w)
	}
	s = typeKeys(s, "/zzz")
	if w := s.width(); w != 32 {
		t.Errorf("width under a query matching nothing = %d, want 32 still", w)
	}
	nodes[4].title = "(MX) On-call"
	s.setTree(nodes, nil)
	if w := s.width(); w != 24 {
		t.Errorf("width = %d, want 24: the hidden prefix does not count", w)
	}
}

func TestSidebarRevealLeavesPinnedOnlyForANodeOutsideIt(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setTree(sampleNodes(), nil)
	s.setTree(s.nodes, &pinState{ids: []string{"P1"}, only: true})
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

func TestSidebarHalfPageMovesHalfTheVisibleRows(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	s.setSize(28, 8)
	s.setTree(flatNodes(20), nil)
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if s.cursor != 4 {
		t.Fatalf("ctrl+d from the top lands on %d, want 4 with eight rows in view", s.cursor)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if s.cursor != 4 {
		t.Errorf("pgdown then ctrl+u lands on %d, want 4", s.cursor)
	}
	s = typeKeys(s, "G")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if s.cursor != 19 {
		t.Errorf("ctrl+d on the last row stays at 19, got %d", s.cursor)
	}
}

// A typed emoji usually carries its variation selector, which the drawn title no longer has.
func TestSidebarFilterMatchesATypedEmojiWithItsSelector(t *testing.T) {
	s := newSidebar(defaultKeyMap(), nil)
	nodes := sampleNodes()
	nodes[7].title = "\u26a0\ufe0f Risks"
	s.setTree(nodes, nil)
	s = typeKeys(s, "/\u26a0\ufe0f")
	if got := visibleIDs(s); !reflect.DeepEqual(got, []string{"F9"}) {
		t.Errorf("visible under the query = %v, want F9", got)
	}
}
