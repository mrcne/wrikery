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
	open       []bool // per node, whether its children are drawn, which the chevron follows
	cursor     int
	offset     int
	height     int // set by the root from the computed layout, View cannot remember it on its own
	keys       KeyMap
	hide       []string // title prefixes the rows leave out, the filter matches what is shown
	filter     textinput.Model
	filtering  bool
	anchor     string // the node selected when / opened the input, esc goes back to it when nothing matches
	pinned     map[string]bool
	pinnedOnly bool
	pinsLoaded bool        // the pins came from the store once, later tree loads keep the ones in memory
	changes    []pinChange // pin and toggle changes the root has not written yet
}

// pinState is what the store holds about pins, the ids and the pinned-only toggle.
type pinState struct {
	ids  []string
	only bool
}

// pinChange is one write the root owes the store: a pin for id, or the toggle when id is empty.
type pinChange struct {
	id string
	on bool
}

func newSidebar(keys KeyMap, hide []string) sidebarModel {
	in := textinput.New()
	in.Prompt = "/"
	in.Placeholder = "filter"
	in.CharLimit = 60
	return sidebarModel{keys: keys, hide: hide, filter: in, pinned: map[string]bool{}}
}

// setTree replaces the nodes, and the pins when the load carried them, with one rebuild for both.
// Expansion state and the selection are kept by id across the reload.
// The toggle counts only with a pin the tree can show, otherwise the pane would hold My tasks alone.
func (s *sidebarModel) setTree(nodes []treeNode, pins *pinState) {
	prev := ""
	if n, ok := s.current(); ok {
		prev = n.id
	}
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
	if pins != nil {
		s.pinned = map[string]bool{}
		for _, id := range pins.ids {
			s.pinned[id] = true
		}
		s.pinnedOnly, s.pinsLoaded = pins.only, true
	}
	if s.pinCount() == 0 {
		s.pinnedOnly = false
	}
	s.rebuild()
	s.reselect(prev)
	s.scroll()
}

// setSize hands the pane size down. The input gets the width so it scrolls a long query instead of running out of the pane.
func (s *sidebarModel) setSize(width, height int) {
	s.height = height
	s.filter.Width = max(1, width-lipgloss.Width(s.filter.Prompt)-1)
}

// takeChanges hands the pending pin writes to the root, once.
func (s *sidebarModel) takeChanges() []pinChange {
	out := s.changes
	s.changes = nil
	return out
}

// pinCount is the number of pins that are nodes of the tree. A pin whose folder left the followed scopes counts for nothing.
func (s sidebarModel) pinCount() int {
	seen := map[string]bool{}
	for _, n := range s.nodes {
		if s.pinned[n.id] && !seen[n.id] {
			seen[n.id] = true
		}
	}
	return len(seen)
}

func (s sidebarModel) indexOf(id string) int {
	for i, n := range s.nodes {
		if n.id == id {
			return i
		}
	}
	return -1
}

// reveal selects a node whatever hides it: collapsed ancestors are expanded, the filter is cleared when the node
// is not among its matches, and pinned-only is left when the node is outside it. False when the id is not in the tree,
// and then nothing changes.
func (s *sidebarModel) reveal(id string) bool {
	idx := s.indexOf(id)
	if idx < 0 {
		return false
	}
	for p := s.parentOf(idx); p >= 0; p = s.parentOf(p) {
		s.nodes[p].expanded = true
	}
	s.rebuild()
	if !s.selectByID(id) && s.query() != "" {
		s.clearFilter()
	}
	if !s.selectByID(id) && s.pinnedOnly {
		s.pinnedOnly = false
		s.changes = append(s.changes, pinChange{})
		s.rebuild()
		s.selectByID(id)
	}
	s.scroll()
	return true
}

func (s *sidebarModel) clearFilter() {
	s.filter.SetValue("")
	s.filtering = false
	s.filter.Blur()
	s.rebuild()
}

// rebuildKeeping rebuilds the rows and keeps the selection, see reselect.
func (s *sidebarModel) rebuildKeeping() {
	prev := ""
	if n, ok := s.current(); ok {
		prev = n.id
	}
	s.rebuild()
	s.reselect(prev)
	s.scroll()
}

// reselect puts the cursor back on the node selected before a rebuild, or on its nearest drawn ancestor,
// or leaves it at the same height clamped to the rows that are left, so a vanished row does not throw the selection to the top.
func (s *sidebarModel) reselect(prev string) {
	if s.selectByID(prev) {
		return
	}
	if idx := s.indexOf(prev); idx >= 0 {
		for p := s.parentOf(idx); p >= 0; p = s.parentOf(p) {
			if s.selectByID(s.nodes[p].id) {
				return
			}
		}
	}
	s.cursor = min(s.cursor, max(0, len(s.visible)-1))
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

// scroll keeps the cursor row inside the pane and the window inside the rows.
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
	// A query can shorten the tree under a window that was scrolled down, which would hide the ancestors above it.
	if top := max(0, len(s.visible)-rows); s.offset > top {
		s.offset = top
	}
}

// rebuild flattens the tree into draw order.
// Without a query it skips the children of collapsed nodes, and in pinned-only mode everything but My tasks,
// the pinned nodes, their ancestors and what hangs under a pinned node.
// A query opens the path to every match and keeps the matches with their ancestors, nothing else.
// The pinned view opens the path to every pin the same way, since expansion lives in memory and a restart collapses it.
func (s *sidebarModel) rebuild() {
	s.visible = s.visible[:0]
	q := s.query()
	n := len(s.nodes)
	s.open = make([]bool, n)
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
		all := s.nodes[i].expanded || q != ""
		for _, c := range s.nodes[i].children {
			if all || (s.pinnedOnly && hasPin[c]) {
				s.open[i] = true
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
	titles := []string{n.title}
	for idx := s.indexOf(n.id); idx >= 0; {
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
			// With no match the cursor goes back to where / was pressed.
			id := s.anchor
			if cur, ok := s.current(); ok {
				id = cur.id
			}
			s.clearFilter()
			s.reveal(id)
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
		s.anchor = before.id
		s.scroll()
		return s, s.filter.Focus()
	case key.Matches(msg, s.keys.Pinned):
		if s.pinCount() == 0 {
			return s, intent(toastMsg{text: "Nothing pinned, space pins the row under the cursor"})
		}
		s.pinnedOnly = !s.pinnedOnly
		s.changes = append(s.changes, pinChange{on: s.pinnedOnly})
		s.rebuildKeeping()
		return s.afterMove(before, nil)
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
		on := !s.pinned[n.id]
		if on {
			s.pinned[n.id] = true
		} else {
			delete(s.pinned, n.id)
		}
		s.changes = append(s.changes, pinChange{id: n.id, on: on})
		// The last pin going would leave My tasks alone in the pane, so the toggle goes with it.
		if s.pinnedOnly && s.pinCount() == 0 {
			s.pinnedOnly = false
			s.changes = append(s.changes, pinChange{})
		}
		s.rebuildKeeping()
		return s.afterMove(before, nil)
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

// width sizes the pane from the whole tree, not from the rows on screen, so a filter or an expand does not lay the panes out again.
func (s sidebarModel) width() int {
	longest := 0
	for _, n := range s.nodes {
		if w := lipgloss.Width(displayTitle(n.title, s.hide)) + 2*n.depth + 8; w > longest {
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
		ni := s.visible[vi]
		n := s.nodes[ni]
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
				if s.open[ni] {
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
			label = lead + fitName(displayTitle(n.title, s.hide), width-2-lipgloss.Width(lead)-lipgloss.Width(mark)) + mark
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
// A part the caller styled on its own ends in a reset, which would end the selection style with it,
// so the row style is opened again behind every reset: the part keeps its color and the highlight runs to the end.
func rowLine(th Theme, label string, width int, selected, focused bool) string {
	prefix := "  "
	if selected {
		prefix = th.Glyphs.Cursor + " "
	}
	line := ansi.Truncate(prefix+label, width, "...")
	style := lipgloss.NewStyle()
	if selected && focused {
		style = style.Background(th.Accent).Foreground(th.Text)
	} else if selected {
		style = style.Foreground(th.Accent)
	}
	var b strings.Builder
	for _, piece := range strings.SplitAfter(line, "\x1b[0m") {
		if piece != "" {
			b.WriteString(style.Render(piece))
		}
	}
	if pad := width - ansi.StringWidth(line); pad > 0 {
		b.WriteString(style.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
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
