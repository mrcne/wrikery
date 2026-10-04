package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	nodes      []treeNode
	visible    []int
	cursor     int
	offset     int
	height     int // set by the root from the computed layout, View cannot remember it on its own
	keys       KeyMap
	hide       []string // title prefixes the rows leave out, the filter matches what is shown
	filter     textinput.Model
	filtering  bool
	pinned     map[string]bool
	pinnedOnly bool
}

func newSidebar(keys KeyMap, hide []string) sidebarModel {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "filter"
	in.CharLimit = 60
	return sidebarModel{keys: keys, hide: hide, filter: in, pinned: map[string]bool{}}
}

// setPins replaces the pins and the toggle with what the store holds.
// The toggle without a pin would show My tasks alone, so it only counts with one.
func (s *sidebarModel) setPins(ids []string, only bool) {
	s.pinned = map[string]bool{}
	for _, id := range ids {
		s.pinned[id] = true
	}
	s.pinnedOnly = only && len(ids) > 0
	s.rebuildKeeping()
}

// reveal selects a node whatever hides it: the filter is cleared, collapsed ancestors are expanded,
// and pinned-only is left when the node is outside it. False when the id is not in the tree.
func (s *sidebarModel) reveal(id string) bool {
	s.filter.SetValue("")
	s.filtering = false
	s.filter.Blur()
	idx := -1
	for i, n := range s.nodes {
		if n.id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.rebuild()
		s.scroll()
		return false
	}
	for p := s.parentOf(idx); p >= 0; p = s.parentOf(p) {
		s.nodes[p].expanded = true
	}
	s.rebuild()
	if !s.selectByID(id) {
		s.pinnedOnly = false
		s.rebuild()
		s.selectByID(id)
	}
	s.scroll()
	return true
}

// rebuildKeeping rebuilds the rows and keeps the selected node when it is still among them.
func (s *sidebarModel) rebuildKeeping() {
	prev := ""
	if n, ok := s.current(); ok {
		prev = n.id
	}
	s.rebuild()
	if !s.selectByID(prev) {
		s.cursor = 0
	}
	s.scroll()
}

func (s sidebarModel) query() string { return strings.ToLower(strings.TrimSpace(s.filter.Value())) }

func (s sidebarModel) matches(n treeNode, q string) bool {
	return strings.Contains(strings.ToLower(displayTitle(n.title, s.hide)), q)
}

// rows is the line count left for the tree once the filter takes the last line.
func (s sidebarModel) rows(height int) int {
	if s.filtering || s.filter.Value() != "" {
		return height - 1
	}
	return height
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
	rows := s.rows(s.height)
	if rows <= 0 {
		return
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+rows {
		s.offset = s.cursor - rows + 1
	}
}

// rebuild flattens the tree into draw order.
// Without a query it skips the children of collapsed nodes, and in pinned-only mode everything but My tasks,
// the pinned nodes, their ancestors and what hangs under a pinned node.
// A query opens the path to every match and keeps the matches with their ancestors, nothing else.
func (s *sidebarModel) rebuild() {
	s.visible = s.visible[:0]
	q := s.query()
	n := len(s.nodes)
	under := make([]bool, n)  // an ancestor is pinned
	hasPin := make([]bool, n) // the node or a descendant is pinned
	keep := make([]bool, n)   // the node is drawn, once its parents are open
	// A child always comes after its parent in nodes, so one pass forward settles under and one pass back the rest.
	for i, node := range s.nodes {
		for _, c := range node.children {
			under[c] = under[c] || under[i] || s.pinned[node.id]
		}
	}
	for i := n - 1; i >= 0; i-- {
		node := s.nodes[i]
		hasPin[i] = s.pinned[node.id]
		for _, c := range node.children {
			hasPin[i] = hasPin[i] || hasPin[c]
		}
		inScope := !s.pinnedOnly || node.kind == nodeMe || under[i] || hasPin[i]
		keep[i] = inScope && (q == "" || s.matches(node, q))
		for _, c := range node.children {
			keep[i] = keep[i] || keep[c]
		}
	}
	var walk func(i int)
	walk = func(i int) {
		if !keep[i] {
			return
		}
		s.visible = append(s.visible, i)
		if s.nodes[i].expanded || q != "" {
			for _, c := range s.nodes[i].children {
				walk(c)
			}
		}
	}
	for i, node := range s.nodes {
		if node.depth == 0 {
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

// The list pane title shows the path from the space down to the selected node.
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
	before, _ := s.current()
	if s.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			// The match under the cursor stays selected, which may mean opening its parents.
			s.reveal(before.id)
		case tea.KeyEnter:
			s.filtering = false
			s.filter.Blur()
		default:
			var cmd tea.Cmd
			s.filter, cmd = s.filter.Update(msg)
			s.rebuild()
			s.cursorToFirstMatch(before.id)
			s.scroll()
			return s.afterMove(before, cmd)
		}
		s.scroll()
		return s.afterMove(before, nil)
	}
	switch {
	case key.Matches(msg, s.keys.Filter):
		s.filtering = true
		s.scroll()
		return s, s.filter.Focus()
	case key.Matches(msg, s.keys.Pinned):
		if len(s.pinned) == 0 {
			return s, intent(toastMsg{text: "Nothing pinned, space pins the row under the cursor"})
		}
		s.pinnedOnly = !s.pinnedOnly
		s.rebuildKeeping()
		return s.afterMove(before, intent(pinnedOnlyMsg{on: s.pinnedOnly}))
	}
	n, ok := s.current()
	if !ok {
		return s, nil
	}
	idx := s.visible[s.cursor]
	q := s.query()
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
		// Under a query every path is open already, so the key goes on to the list.
		if q == "" && len(n.children) > 0 && !n.expanded {
			s.nodes[idx].expanded = true
			s.rebuild()
			s.scroll()
			return s, nil
		}
		return s, intent(focusMsg{pane: paneList})
	case key.Matches(msg, s.keys.Left):
		if q == "" && n.expanded {
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
	case key.Matches(msg, s.keys.Pin):
		if n.kind == nodeMe {
			return s, nil
		}
		cmds := []tea.Cmd{intent(pinChangedMsg{id: n.id, pinned: !s.pinned[n.id]})}
		if s.pinned[n.id] {
			delete(s.pinned, n.id)
		} else {
			s.pinned[n.id] = true
		}
		// The last pin going would leave My tasks alone in the pane, so the toggle goes with it.
		if len(s.pinned) == 0 && s.pinnedOnly {
			s.pinnedOnly = false
			cmds = append(cmds, intent(pinnedOnlyMsg{on: false}))
		}
		s.rebuildKeeping()
		return s.afterMove(before, tea.Batch(cmds...))
	default:
		return s, nil
	}
	s.scroll()
	return s.afterMove(before, nil)
}

// afterMove previews the node now under the cursor in the list, the same way lazygit follows the cursor.
func (s sidebarModel) afterMove(before treeNode, cmd tea.Cmd) (sidebarModel, tea.Cmd) {
	if cur, ok := s.current(); ok && cur.id != before.id {
		return s, tea.Batch(cmd, intent(nodeSelectedMsg{node: cur}))
	}
	return s, cmd
}

// cursorToFirstMatch parks the cursor on the first row that matches the query itself, not through a descendant.
// With the query gone the row selected before stays, if it is still drawn.
func (s *sidebarModel) cursorToFirstMatch(prev string) {
	q := s.query()
	if q == "" {
		if !s.selectByID(prev) {
			s.cursor = 0
		}
		return
	}
	for vi, ni := range s.visible {
		if s.matches(s.nodes[ni], q) {
			s.cursor = vi
			return
		}
	}
	s.cursor = 0
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
	rows := s.rows(height)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	lines := make([]string, 0, height)
	for row := 0; row < rows; row++ {
		vi := offset + row
		if vi >= len(s.visible) {
			lines = append(lines, "")
			continue
		}
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
			lead := strings.Repeat("  ", n.depth) + marker + " "
			mark := ""
			if s.pinned[n.id] {
				mark = " " + muted.Render("*")
			}
			// The cursor prefix takes two cells in front of the label.
			label = lead + fitName(displayTitle(n.title, th.HidePrefixes), width-2-lipgloss.Width(lead)-lipgloss.Width(mark)) + mark
		}
		if n.kind == nodeMe {
			count := fmt.Sprintf("%d", n.count)
			pad := width - lipgloss.Width(label) - lipgloss.Width(count) - 3
			if pad > 0 {
				label += strings.Repeat(" ", pad) + muted.Render(count)
			}
		}
		lines = append(lines, rowLine(th, label, width, vi == s.cursor, focused))
	}
	if rows < height {
		lines = append(lines, s.filter.View())
	}
	return strings.Join(lines, "\n")
}

// rowLine renders one list row and is shared by every list in the package.
// The row is cut to the width here, because Width() word wraps a longer label onto a second line,
// and every list scrolls by counting one line per row.
// A selected row drops the styling its parts brought along: a glyph rendered on its own ends in a reset,
// which would end the selection background right after it.
func rowLine(th Theme, label string, width int, selected, focused bool) string {
	prefix := "  "
	if selected {
		prefix = th.Glyphs.Cursor + " "
		label = ansi.Strip(label)
	}
	line := ansi.Truncate(prefix+label, width, "...")
	style := lipgloss.NewStyle().MaxWidth(width).Width(width)
	if selected && focused {
		style = style.Background(th.Accent).Foreground(th.Text)
	} else if selected {
		style = style.Foreground(th.Accent)
	}
	return style.Render(line)
}

// fitName cuts a name that does not fit to its start, three dots and its end.
// The end gets a third of the room and the start the rest, so the dots sit in one column for every cut row of a depth,
// and siblings that differ only at the end still tell apart.
// Both pieces are cut by cell, a wide character is dropped rather than split.
func fitName(name string, width int) string {
	total := ansi.StringWidth(name)
	if total <= width {
		return name
	}
	if width < 7 {
		return ansi.Truncate(name, width, "...")
	}
	room := width - 3
	tail := room / 3
	return ansi.Truncate(name, room-tail, "") + "..." + ansi.Cut(name, total-tail, total)
}
