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
	// A treeNode nobody has selected yet has to differ from My tasks, or the root would read a zero value as a real selection.
	nodeNone nodeKind = iota
	nodeMe
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
	height  int // set by the root from the computed layout, View cannot remember it on its own
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
	s.scroll()
}

// scroll clamps offset so the cursor row stays inside the pane.
// View has a value receiver, so it cannot persist the offset it would otherwise compute itself, this is done here instead.
func (s *sidebarModel) scroll() {
	if s.height <= 0 {
		return
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+s.height {
		s.offset = s.cursor - s.height + 1
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
			s.scroll()
			return s, nil
		}
		return s, intent(focusMsg{pane: paneList})
	case key.Matches(msg, s.keys.Left):
		if n.expanded {
			s.nodes[idx].expanded = false
			s.rebuild()
			s.scroll()
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
	s.scroll()
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
	// offset lives on the model and is advanced by scroll().
	// This only guards against it landing past the end, for example right after the node list shrinks.
	offset := s.offset
	if last := len(s.visible) - 1; offset > last {
		offset = max(0, last)
	}
	var b strings.Builder
	for row := 0; row < height && offset+row < len(s.visible); row++ {
		vi := offset + row
		n := s.nodes[s.visible[vi]]
		// My tasks carries no chevron. A project shows its status glyph instead of expand state.
		// A space or folder shows the expand/collapse chevron only when it actually has children,
		// everything else gets a blank space so titles still line up.
		var label string
		switch n.kind {
		case nodeMe:
			label = strings.Repeat("  ", n.depth) + n.title
		default:
			var marker string
			switch {
			case n.kind == nodeProject:
				marker = lipgloss.NewStyle().Foreground(th.StatusColor(store.CustomStatus{Group: n.statusGroup})).Render(th.StatusGlyph(n.statusGroup))
			case len(n.children) > 0:
				marker = th.Glyphs.Collapsed
				if n.expanded {
					marker = th.Glyphs.Expanded
				}
			default:
				marker = " "
			}
			label = strings.Repeat("  ", n.depth) + marker + " " + n.title
		}
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

// rowLine renders one list row and is shared by every list in the package.
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
