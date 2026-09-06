package ui

type pane int

const (
	paneSidebar pane = iota
	paneList
	paneDetail
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
