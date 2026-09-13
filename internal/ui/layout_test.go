package ui

import "testing"

func TestVisibleCountBreakpoints(t *testing.T) {
	for w, want := range map[int]int{79: 1, 80: 2, 119: 2, 120: 3, 200: 3} {
		if got := visibleCount(w); got != want {
			t.Errorf("visibleCount(%d) = %d, want %d", w, got, want)
		}
	}
}

func TestLayoutWindowFollowsFocus(t *testing.T) {
	two := computeLayout(100, 40, paneDetail, 28)
	if len(two.visible) != 2 || two.visible[0] != paneList || two.visible[1] != paneDetail {
		t.Errorf("focus detail at 100 cols shows %v, want list+detail", two.visible)
	}
	two = computeLayout(100, 40, paneSidebar, 28)
	if two.visible[0] != paneSidebar || two.visible[1] != paneList {
		t.Errorf("focus sidebar at 100 cols shows %v", two.visible)
	}
	one := computeLayout(70, 40, paneList, 28)
	if len(one.visible) != 1 || one.visible[0] != paneList || one.rects[paneList].w != 70 {
		t.Errorf("one pane = %+v", one)
	}
	three := computeLayout(160, 40, paneList, 28)
	total := 0
	for _, p := range three.visible {
		total += three.rects[p].w
	}
	if total != 160 || three.rects[paneSidebar].w != 28 {
		t.Errorf("three pane widths do not fill 160: %+v", three.rects)
	}
	for _, r := range three.rects {
		if r.h != 40 {
			t.Errorf("pane height %d, want 40", r.h)
		}
	}
}

func TestBoardLayoutShowsASidePaneOnlyWhileFocused(t *testing.T) {
	alone := computeBoardLayout(120, 40, paneBoard, 28)
	if len(alone.visible) != 1 || alone.visible[0] != paneBoard || alone.rects[paneBoard].w != 120 {
		t.Errorf("board focused: %+v, want the board alone across 120", alone)
	}
	side := computeBoardLayout(120, 40, paneSidebar, 28)
	if len(side.visible) != 2 || side.visible[0] != paneSidebar || side.rects[paneBoard].w != 92 || side.rects[paneBoard].x != 28 {
		t.Errorf("sidebar focused: %+v, want sidebar 28 then the board 92", side)
	}
	det := computeBoardLayout(120, 40, paneDetail, 28)
	if len(det.visible) != 2 || det.visible[1] != paneDetail || det.rects[paneDetail].w != 54 || det.rects[paneBoard].w != 66 {
		t.Errorf("detail focused: %+v, want the board 66 then the detail 54", det)
	}
	narrow := computeBoardLayout(70, 40, paneDetail, 28)
	if len(narrow.visible) != 1 || narrow.visible[0] != paneDetail || narrow.rects[paneDetail].w != 70 {
		t.Errorf("below 80 the focused pane alone: %+v", narrow)
	}
	for _, r := range side.rects {
		if r.h != 40 {
			t.Errorf("pane height %d, want 40", r.h)
		}
	}
}
