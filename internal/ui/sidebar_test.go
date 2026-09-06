package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	var s sidebarModel
	s.keys = defaultKeyMap()
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
	var s sidebarModel
	s.keys = defaultKeyMap()
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
	var s sidebarModel
	s.keys = defaultKeyMap()
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
	var s sidebarModel
	s.keys = defaultKeyMap()
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
	var s sidebarModel
	s.keys = defaultKeyMap()
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
