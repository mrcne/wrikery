package ui

type pane int

const (
	paneSidebar pane = iota
	paneList
	paneDetail
	paneBoard
)

// shape is how the main screen shows the tasks: the list between the sidebar and the detail, or the board.
type shape int

const (
	shapeList shape = iota
	shapeBoard
)

type rect struct{ x, y, w, h int }

type layout struct {
	visible []pane
	rects   map[pane]rect
}

// visibleCount holds the breakpoints: three panes from 120 columns, two from 80, one below.
func visibleCount(width int) int {
	switch {
	case width >= 120:
		return 3
	case width >= 80:
		return 2
	default:
		return 1
	}
}

// computeLayout slides a window of n panes over sidebar, list, detail so the focused pane is always inside it.
func computeLayout(width, height int, focus pane, sidebarWidth int) layout {
	n := visibleCount(width)
	first := paneSidebar
	if int(focus) > n-1 {
		first = pane(int(focus) - (n - 1))
	}
	lay := layout{rects: map[pane]rect{}}
	for i := 0; i < n; i++ {
		lay.visible = append(lay.visible, first+pane(i))
	}
	x := 0
	remaining := width
	for i, p := range lay.visible {
		var w int
		switch {
		case i == len(lay.visible)-1:
			w = remaining
		case p == paneSidebar:
			w = sidebarWidth
		case p == paneList:
			// The detail pane takes 45 percent of what is left after the sidebar, the list the rest.
			w = remaining - remaining*45/100
		}
		lay.rects[p] = rect{x: x, y: 0, w: w, h: height}
		x += w
		remaining -= w
	}
	return lay
}

// computeBoardLayout draws the board across the width and a side pane only while it has focus.
// The folder rarely changes while a board is up and the width goes to the columns, so the sidebar is not kept in view.
// Below 80 columns the focused pane alone is drawn, as in the list shape.
func computeBoardLayout(width, height int, focus pane, sidebarWidth int) layout {
	lay := layout{rects: map[pane]rect{}}
	alone := func(p pane) layout {
		lay.visible = []pane{p}
		lay.rects[p] = rect{x: 0, y: 0, w: width, h: height}
		return lay
	}
	if width < 80 {
		return alone(focus)
	}
	switch focus {
	case paneSidebar:
		lay.visible = []pane{paneSidebar, paneBoard}
		lay.rects[paneSidebar] = rect{x: 0, y: 0, w: sidebarWidth, h: height}
		lay.rects[paneBoard] = rect{x: sidebarWidth, y: 0, w: width - sidebarWidth, h: height}
	case paneDetail:
		// The detail keeps the share it has in the list shape, 45 percent, the board takes the rest.
		dw := width * 45 / 100
		lay.visible = []pane{paneBoard, paneDetail}
		lay.rects[paneBoard] = rect{x: 0, y: 0, w: width - dw, h: height}
		lay.rects[paneDetail] = rect{x: width - dw, y: 0, w: dw, h: height}
	default:
		return alone(paneBoard)
	}
	return lay
}
