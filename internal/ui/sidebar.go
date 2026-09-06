package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type nodeKind int

const (
	nodeMe nodeKind = iota
	nodeSpace
	nodeProject
	nodeFolder
)

type treeNode struct {
	id, title   string
	kind        nodeKind
	depth       int
	children    []int
	expanded    bool
	statusGroup string // projects only
	count       int    // nodeMe only
}

type sidebarModel struct {
	nodes   []treeNode
	visible []int
	cursor  int
	offset  int
	keys    KeyMap
}

func (s *sidebarModel) setNodes(nodes []treeNode) {
	prev := ""
	if n, ok := s.current(); ok {
		prev = n.id
	}
	// Keep expansion state across reloads, nodes are matched by id.
	expanded := map[string]bool{}
	for _, n := range s.nodes {
		expanded[n.id] = n.expanded
	}
	for i := range nodes {
		if was, ok := expanded[nodes[i].id]; ok {
			nodes[i].expanded = was
		}
	}
	s.nodes = nodes
	s.rebuild()
	if !s.selectByID(prev) {
		s.cursor = 0
	}
}

// rebuild flattens the tree into draw order, skipping the children of collapsed nodes.
func (s *sidebarModel) rebuild() {
	s.visible = s.visible[:0]
	var walk func(i int)
	walk = func(i int) {
		s.visible = append(s.visible, i)
		if s.nodes[i].expanded {
			for _, c := range s.nodes[i].children {
				walk(c)
			}
		}
	}
	for i, n := range s.nodes {
		if n.depth == 0 {
			walk(i)
		}
	}
	if s.cursor >= len(s.visible) {
		s.cursor = max(0, len(s.visible)-1)
	}
}

func (s sidebarModel) current() (treeNode, bool) {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return treeNode{}, false
	}
	return s.nodes[s.visible[s.cursor]], true
}

func (s *sidebarModel) selectByID(id string) bool {
	for vi, ni := range s.visible {
		if s.nodes[ni].id == id {
			s.cursor = vi
			return true
		}
	}
	return false
}

func (s sidebarModel) parentOf(idx int) int {
	for i, n := range s.nodes {
		for _, c := range n.children {
			if c == idx {
				return i
			}
		}
	}
	return -1
}

// crumb joins a node's ancestors with the root first, for the list pane title.
func (s sidebarModel) crumb(n treeNode) string {
	var titles []string
	idx := -1
	for i, cand := range s.nodes {
		if cand.id == n.id {
			idx = i
			break
		}
	}
	titles = append(titles, n.title)
	for idx >= 0 {
		p := s.parentOf(idx)
		if p < 0 {
			break
		}
		titles = append([]string{s.nodes[p].title}, titles...)
		idx = p
	}
	return strings.Join(titles, " / ")
}

func (s sidebarModel) Update(msg tea.KeyMsg) (sidebarModel, tea.Cmd) {
	n, ok := s.current()
	if !ok {
		return s, nil
	}
	idx := s.visible[s.cursor]
	switch {
	case key.Matches(msg, s.keys.Down):
		if s.cursor < len(s.visible)-1 {
			s.cursor++
		}
	case key.Matches(msg, s.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(msg, s.keys.Top):
		s.cursor = 0
	case key.Matches(msg, s.keys.Bottom):
		s.cursor = len(s.visible) - 1
	case key.Matches(msg, s.keys.Right):
		if len(n.children) > 0 && !n.expanded {
			s.nodes[idx].expanded = true
			s.rebuild()
			return s, nil
		}
		return s, intent(focusMsg{pane: paneList})
	case key.Matches(msg, s.keys.Left):
		if n.expanded {
			s.nodes[idx].expanded = false
			s.rebuild()
			return s, nil
		}
		if p := s.parentOf(idx); p >= 0 {
			s.selectByID(s.nodes[p].id)
		}
	case key.Matches(msg, s.keys.Enter):
		return s, tea.Batch(intent(nodeSelectedMsg{node: n}), intent(focusMsg{pane: paneList}))
	default:
		return s, nil
	}
	// Moving the cursor previews the node in the list, the same way lazygit follows the cursor.
	if cur, ok := s.current(); ok && cur.id != n.id {
		return s, intent(nodeSelectedMsg{node: cur})
	}
	return s, nil
}

func (s sidebarModel) width() int {
	longest := 0
	for _, i := range s.visible {
		n := s.nodes[i]
		if w := lipgloss.Width(n.title) + 2*n.depth + 8; w > longest {
			longest = w
		}
	}
	return min(32, max(24, longest))
}

func (s sidebarModel) View(th Theme, width, height int, focused bool) string {
	if height <= 0 {
		return ""
	}
	// Keep the cursor row inside the pane.
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+height {
		s.offset = s.cursor - height + 1
	}
	var b strings.Builder
	for row := 0; row < height && s.offset+row < len(s.visible); row++ {
		vi := s.offset + row
		n := s.nodes[s.visible[vi]]
		// My tasks carries no chevron, a project shows its status glyph instead of expand state,
		// everything else (space, folder) shows the expand/collapse chevron even with no children yet.
		var marker string
		switch n.kind {
		case nodeMe:
		case nodeProject:
			glyph := lipgloss.NewStyle().Foreground(th.StatusColor(store.CustomStatus{Group: n.statusGroup})).Render(th.StatusGlyph(n.statusGroup))
			marker = glyph + " "
		default:
			g := th.Glyphs.Collapsed
			if n.expanded {
				g = th.Glyphs.Expanded
			}
			marker = g + " "
		}
		label := strings.Repeat("  ", n.depth) + marker + n.title
		if n.kind == nodeMe {
			count := fmt.Sprintf("%d", n.count)
			pad := width - lipgloss.Width(label) - lipgloss.Width(count) - 3
			if pad > 0 {
				label += strings.Repeat(" ", pad) + lipgloss.NewStyle().Foreground(th.Muted).Render(count)
			}
		}
		b.WriteString(rowLine(th, label, width, vi == s.cursor, focused))
		if row < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// rowLine renders one list row: cursor glyph on the selected row, accent background when the pane has focus.
// Shared with the task list and other lists that come later.
func rowLine(th Theme, label string, width int, selected, focused bool) string {
	prefix := "  "
	if selected {
		prefix = th.Glyphs.Cursor + " "
	}
	line := prefix + label
	style := lipgloss.NewStyle().MaxWidth(width).Width(width)
	if selected && focused {
		style = style.Background(th.Accent).Foreground(th.Text)
	} else if selected {
		style = style.Foreground(th.Accent)
	}
	return style.Render(line)
}
