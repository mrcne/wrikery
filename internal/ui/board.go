package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/store"
)

const (
	cardWidth    = 21 // the least a column with cards gets, enough for a readable title and the meta line
	cardWidthMax = 40 // spare width past this only pads titles
	columnGap    = 2
	cardLines    = 2
)

type boardCell struct{ lane, col int }

// boardGrid is the list's groups and columns laid out as lanes and cells.
// The cells hold row positions, the indices the list cursor uses, so a task in two lanes is two cards.
type boardGrid struct {
	cells [][][]int   // [lane][col] row positions, in row order
	pos   []boardCell // per row position, its cell
	idx   []int       // per row position, its index within the cell
}

func buildGrid(l *taskListModel) boardGrid {
	g := boardGrid{cells: make([][][]int, len(l.groups)), pos: make([]boardCell, len(l.rows)), idx: make([]int, len(l.rows))}
	for lane := range l.groups {
		g.cells[lane] = make([][]int, len(l.columns))
	}
	lane := -1
	for p := range l.rows {
		for lane+1 < len(l.groupStart) && l.groupStart[lane+1] <= p {
			lane++
		}
		c := l.rowCol[p]
		g.pos[p] = boardCell{lane: lane, col: c}
		g.idx[p] = len(g.cells[lane][c])
		g.cells[lane][c] = append(g.cells[lane][c], p)
	}
	return g
}

// cardRows is the body lines the cards of a lane take, its tallest column times the lines of a card.
func (g boardGrid) cardRows(lane int) int {
	h := 0
	for _, cell := range g.cells[lane] {
		h = max(h, len(cell)*cardLines)
	}
	return h
}

// step walks the column across lanes: the next card in the cell, else the nearest card in the same column of a lane further on.
func (g boardGrid) step(cell boardCell, idx, dir int) int {
	col := cell.col
	if next := idx + dir; next >= 0 && next < len(g.cells[cell.lane][col]) {
		return g.cells[cell.lane][col][next]
	}
	for lane := cell.lane + dir; lane >= 0 && lane < len(g.cells); lane += dir {
		if c := g.cells[lane][col]; len(c) > 0 {
			if dir > 0 {
				return c[0]
			}
			return c[len(c)-1]
		}
	}
	return -1
}

// sideways goes to the next column with a card in this lane, at the nearest index.
// When the lane has none further on, it goes to the nearest lane with a card in the columns further on, the closest column first.
func (g boardGrid) sideways(cell boardCell, idx, dir int) int {
	ncols := len(g.cells[cell.lane])
	for col := cell.col + dir; col >= 0 && col < ncols; col += dir {
		if c := g.cells[cell.lane][col]; len(c) > 0 {
			return c[min(idx, len(c)-1)]
		}
	}
	for col := cell.col + dir; col >= 0 && col < ncols; col += dir {
		for d := 1; d < len(g.cells); d++ {
			for _, lane := range []int{cell.lane - d, cell.lane + d} {
				if lane >= 0 && lane < len(g.cells) && len(g.cells[lane][col]) > 0 {
					return g.cells[lane][col][0]
				}
			}
		}
	}
	return -1
}

// lane goes to the first card of the next lane, in the same column when it has one, else in the first column that has one.
func (g boardGrid) lane(cell boardCell, dir int) int {
	lane := cell.lane + dir
	if lane < 0 || lane >= len(g.cells) {
		return -1
	}
	if c := g.cells[lane][cell.col]; len(c) > 0 {
		return c[0]
	}
	for _, c := range g.cells[lane] {
		if len(c) > 0 {
			return c[0]
		}
	}
	return -1
}

// end is the first or the last card of the column over all lanes.
func (g boardGrid) end(col int, last bool) int {
	if last {
		for lane := len(g.cells) - 1; lane >= 0; lane-- {
			if c := g.cells[lane][col]; len(c) > 0 {
				return c[len(c)-1]
			}
		}
		return -1
	}
	for lane := range g.cells {
		if c := g.cells[lane][col]; len(c) > 0 {
			return c[0]
		}
	}
	return -1
}

type boardWindow struct {
	first, last int   // columns drawn, inclusive, last is -1 without columns
	widths      []int // per column, meaningful for the drawn ones
	left, right int   // columns off screen on each side
}

func columnHeader(c boardColumn) string { return fmt.Sprintf("%s (%d)", c.title, len(c.rows)) }

// fitColumns picks the columns drawn from firstCol on and their widths for the inner width.
// A column with cards is at least cardWidth wide and an empty one only as wide as its header, so an unused status costs little.
// When they do not all fit, a window of whole columns is drawn and moved so that cursorCol is inside it,
// with 4 cells on the left and 5 on the right kept for the "< n" and "+n >" markers.
func fitColumns(cols []boardColumn, width, firstCol, cursorCol int) boardWindow {
	n := len(cols)
	w := boardWindow{widths: make([]int, n), last: -1}
	if n == 0 {
		return w
	}
	total := columnGap * (n - 1)
	for i, c := range cols {
		w.widths[i] = ansi.StringWidth(columnHeader(c))
		if len(c.rows) > 0 {
			w.widths[i] = max(w.widths[i], cardWidth)
		}
		total += w.widths[i]
	}
	if total <= width {
		w.last = n - 1
		spread(cols, w.widths, 0, n-1, width-total)
		return w
	}
	avail := width - 4 - 5
	cursorCol = min(max(cursorCol, 0), n-1)
	first := min(max(firstCol, 0), cursorCol)
	last := lastFitting(w.widths, first, avail)
	for last < cursorCol {
		first++
		last = lastFitting(w.widths, first, avail)
	}
	w.first, w.last, w.left, w.right = first, last, first, n-1-last
	used := columnGap * (last - first)
	for i := first; i <= last; i++ {
		used += w.widths[i]
	}
	spread(cols, w.widths, first, last, avail-used)
	return w
}

// lastFitting is the last column that fits next to first in avail cells, first itself at the least.
func lastFitting(widths []int, first, avail int) int {
	used, last := widths[first], first
	for last+1 < len(widths) && used+columnGap+widths[last+1] <= avail {
		last++
		used += columnGap + widths[last]
	}
	return last
}

// spread hands spare cells to the card columns between first and last, equally, up to cardWidthMax each.
func spread(cols []boardColumn, widths []int, first, last, spare int) {
	var card []int
	for i := first; i <= last; i++ {
		if len(cols[i].rows) > 0 {
			card = append(card, i)
		}
	}
	if len(card) == 0 || spare <= 0 {
		return
	}
	each := spare / len(card)
	for _, i := range card {
		widths[i] = min(cardWidthMax, widths[i]+each)
	}
}

// boardModel draws the list's rows as cards in status columns and moves the list's own cursor.
// It keeps no rows of its own: Update and View read the list, so the two shapes can never disagree on what is selected.
type boardModel struct {
	firstCol int       // first column drawn, moved by fit so the cursor column stays in the window
	offset   int       // first body line drawn, the header row is pinned above it
	col      int       // column the cursor was in last, drawn as current when the list is empty
	last     boardCell // cell of the selected card as of the last fit, where the cursor goes when the card leaves the board
	lastIdx  int
	width    int
	height   int // inner box height, set by the root from the computed layout
	keys     KeyMap
}

// bodyHeight is what is left for the lanes under the pinned header row, and under the filter line when it shows.
func (b boardModel) bodyHeight(l *taskListModel) int {
	h := b.height - 1
	if l.filtering || l.filter.Value() != "" {
		h--
	}
	return max(1, h)
}

// cardTop is the first body line of the card at row position p.
func cardTop(l *taskListModel, g boardGrid, p int) int {
	c := g.pos[p]
	y := 0
	for lane := 0; lane < c.lane; lane++ {
		y += g.cardRows(lane)
		if l.sectioned() {
			y++
		}
	}
	if l.sectioned() {
		y++
	}
	return y + g.idx[p]*cardLines
}

// fit moves the column window and the vertical offset so the selected card is drawn, and remembers its cell.
// It runs after every move and every resize, View has a value receiver and cannot keep what it computes.
func (b *boardModel) fit(l *taskListModel) {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		b.firstCol, b.offset = 0, 0
		return
	}
	g := buildGrid(l)
	cell := g.pos[l.cursor]
	b.col, b.last, b.lastIdx = cell.col, cell, g.idx[l.cursor]
	b.firstCol = fitColumns(l.columns, b.width, b.firstCol, cell.col).first
	top := cardTop(l, g, l.cursor)
	if l.sectioned() && g.idx[l.cursor] == 0 {
		// The first card of a lane brings the lane's divider along.
		top--
	}
	if top < b.offset {
		b.offset = top
	}
	if bottom := cardTop(l, g, l.cursor) + cardLines; bottom > b.offset+b.bodyHeight(l) {
		b.offset = bottom - b.bodyHeight(l)
	}
	b.offset = max(0, b.offset)
}

// fallback is the row position to select once the selected card is gone, after a move onto a status the board hides:
// the card at the same index in the cell it had, or the last one there, or the nearest column of that lane with a card.
// The list's own fallback, the row at the old position, could land anywhere in the lane, the rows are ordered by group and then by the list order.
func (b boardModel) fallback(l *taskListModel) int {
	if len(l.rows) == 0 {
		return 0
	}
	g := buildGrid(l)
	lane := min(b.last.lane, len(g.cells)-1)
	col := min(b.last.col, len(l.columns)-1)
	if lane < 0 || col < 0 {
		return 0
	}
	if cell := g.cells[lane][col]; len(cell) > 0 {
		return cell[min(b.lastIdx, len(cell)-1)]
	}
	for d := 1; d < len(l.columns); d++ {
		for _, c := range []int{col - d, col + d} {
			if c >= 0 && c < len(l.columns) && len(g.cells[lane][c]) > 0 {
				return g.cells[lane][c][0]
			}
		}
	}
	return 0
}

// inBucket tells whether the selected card sits in a column that takes no move: another workflow than the main one, or an unknown status.
func (b boardModel) inBucket(l *taskListModel) bool {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return false
	}
	return l.columns[l.rowCol[l.cursor]].bucket
}

func (b boardModel) Update(msg tea.KeyMsg, l *taskListModel) (boardModel, tea.Cmd) {
	switch {
	case key.Matches(msg, b.keys.Filter), key.Matches(msg, b.keys.ToggleDone):
		// The filter input and the done toggle live on the list, the board only draws their result.
		var cmd tea.Cmd
		*l, cmd = l.Update(msg)
		b.fit(l)
		return b, cmd
	case key.Matches(msg, b.keys.Enter):
		return b, intent(focusMsg{pane: paneDetail})
	}
	if len(l.rows) == 0 {
		return b, nil
	}
	g := buildGrid(l)
	before, _ := l.current()
	cell, idx := g.pos[l.cursor], g.idx[l.cursor]
	target := -1
	switch {
	case key.Matches(msg, b.keys.Down):
		target = g.step(cell, idx, +1)
	case key.Matches(msg, b.keys.Up):
		target = g.step(cell, idx, -1)
	case key.Matches(msg, b.keys.ColNext):
		target = g.sideways(cell, idx, +1)
	case key.Matches(msg, b.keys.ColPrev):
		target = g.sideways(cell, idx, -1)
	case key.Matches(msg, b.keys.NextGroup):
		target = g.lane(cell, +1)
	case key.Matches(msg, b.keys.PrevGroup):
		target = g.lane(cell, -1)
	case key.Matches(msg, b.keys.Top):
		target = g.end(cell.col, false)
	case key.Matches(msg, b.keys.Bottom):
		target = g.end(cell.col, true)
	}
	if target < 0 {
		return b, nil
	}
	l.cursor = target
	b.fit(l)
	_, cmd := l.afterMove(before, nil)
	return b, cmd
}

// cardLine draws one of the two lines of a card: the title cut with two dots, then a muted meta line with the initials,
// the pending or failed mark and the due date right aligned, through the same helpers as a list row.
func cardLine(th Theme, ref refData, now time.Time, r taskRow, sub, width int, selected, focused bool) string {
	inner := width - 2 // rowLine puts the cursor prefix in front
	if sub == 0 {
		return rowLine(th, ansi.Truncate(r.task.Title, max(inner, 1), ".."), width, selected, focused)
	}
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	who := fmt.Sprintf("%-4s", initials(r.task.ResponsibleIDs, ref.contacts, ref.meID))
	mark := " "
	switch r.state {
	case store.StatePending:
		mark = lipgloss.NewStyle().Foreground(th.Warn).Render(th.Glyphs.Pending)
	case store.StateFailed:
		mark = lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed)
	}
	due, overdue := dueLabel(r.task, now)
	dueStyle := muted
	if overdue {
		dueStyle = lipgloss.NewStyle().Foreground(th.Error)
	}
	pad := max(0, inner-5-ansi.StringWidth(due))
	return rowLine(th, muted.Render(who)+mark+strings.Repeat(" ", pad)+dueStyle.Render(due), width, selected, focused)
}

func (b boardModel) View(th Theme, ref refData, now time.Time, l *taskListModel, focused bool) string {
	if b.width <= 0 || b.height <= 0 {
		return ""
	}
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	g := buildGrid(l)
	cursorCol := b.col
	if l.cursor >= 0 && l.cursor < len(l.rows) {
		cursorCol = g.pos[l.cursor].col
	}
	w := fitColumns(l.columns, b.width, b.firstCol, cursorCol)
	// The markers take their cells only when the window is in effect, so a board that fits uses the full width.
	prefix := ""
	if w.left > 0 || w.right > 0 {
		prefix = strings.Repeat(" ", 4)
	}
	gap := strings.Repeat(" ", columnGap)
	pad := func(s string, width int) string { return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s))) }

	var header []string
	for c := w.first; c <= w.last; c++ {
		col := l.columns[c]
		style := lipgloss.NewStyle().Foreground(th.StatusColor(col.status))
		if col.bucket {
			style = muted
		}
		if c == cursorCol && focused {
			style = lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
		}
		header = append(header, style.Render(pad(ansi.Truncate(columnHeader(col), w.widths[c], ".."), w.widths[c])))
	}
	head := prefix + strings.Join(header, gap)
	if w.left > 0 {
		// prefix is four plain spaces and nothing styled comes before them, so the cut splits no escape sequence.
		head = muted.Render(fmt.Sprintf("%-4s", fmt.Sprintf("< %d", w.left))) + head[4:]
	}
	if w.right > 0 {
		marker := fmt.Sprintf("%5s", fmt.Sprintf("+%d >", w.right))
		head = pad(head, b.width-ansi.StringWidth(marker)) + muted.Render(marker)
	}

	var body []string
	for lane := range g.cells {
		if l.sectioned() {
			grp := l.groups[lane]
			body = append(body, divider(th, fmt.Sprintf("%s (%d)", grp.title, len(grp.rows)), b.width))
		}
		for y := 0; y < g.cardRows(lane); y++ {
			var cells []string
			for c := w.first; c <= w.last; c++ {
				cell := g.cells[lane][c]
				i, sub := y/cardLines, y%cardLines
				if i >= len(cell) {
					cells = append(cells, strings.Repeat(" ", w.widths[c]))
					continue
				}
				p := cell[i]
				cells = append(cells, cardLine(th, ref, now, l.all[l.rows[p]], sub, w.widths[c], p == l.cursor, focused))
			}
			body = append(body, prefix+strings.Join(cells, gap))
		}
	}
	if len(l.rows) == 0 {
		text := "no tasks here"
		if !l.showDone && len(l.all) > 0 {
			text = "nothing open, z shows completed"
		}
		body = []string{"", muted.Render(text)}
	}

	bodyH := b.bodyHeight(l)
	offset := min(b.offset, max(0, len(body)-1))
	lines := make([]string, 0, b.height)
	lines = append(lines, head)
	for y := 0; y < bodyH; y++ {
		if offset+y < len(body) {
			lines = append(lines, body[offset+y])
		} else {
			lines = append(lines, "")
		}
	}
	if l.filtering || l.filter.Value() != "" {
		lines = append(lines, l.filter.View())
	}
	return strings.Join(lines, "\n")
}
