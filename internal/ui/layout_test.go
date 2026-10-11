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
	three := []pane{paneSidebar, paneList, paneDetail}
	two := computeLayout(100, 40, paneDetail, 28, three)
	if len(two.visible) != 2 || two.visible[0] != paneList || two.visible[1] != paneDetail {
		t.Errorf("focus detail at 100 cols shows %v, want list+detail", two.visible)
	}
	two = computeLayout(100, 40, paneSidebar, 28, three)
	if two.visible[0] != paneSidebar || two.visible[1] != paneList {
		t.Errorf("focus sidebar at 100 cols shows %v", two.visible)
	}
	one := computeLayout(70, 40, paneList, 28, three)
	if len(one.visible) != 1 || one.visible[0] != paneList || one.rects[paneList].w != 70 {
		t.Errorf("one pane = %+v", one)
	}
	wide := computeLayout(160, 40, paneList, 28, three)
	total := 0
	for _, p := range wide.visible {
		total += wide.rects[p].w
	}
	if total != 160 || wide.rects[paneSidebar].w != 28 {
		t.Errorf("three pane widths do not fill 160: %+v", wide.rects)
	}
	for _, r := range wide.rects {
		if r.h != 40 {
			t.Errorf("pane height %d, want 40", r.h)
		}
	}
}

// With the sidebar hidden the window slides over the list and the detail alone.
func TestLayoutWindowOverTheListAndTheDetail(t *testing.T) {
	two := []pane{paneList, paneDetail}
	wide := computeLayout(160, 40, paneList, 28, two)
	if len(wide.visible) != 2 || wide.visible[0] != paneList || wide.rects[paneList].w != 88 || wide.rects[paneDetail].w != 72 || wide.rects[paneDetail].x != 88 {
		t.Errorf("list and detail at 160: %v %+v, want the list 88 then the detail 72", wide.visible, wide.rects)
	}
	mid := computeLayout(100, 40, paneDetail, 28, two)
	if len(mid.visible) != 2 || mid.visible[0] != paneList || mid.rects[paneDetail].w != 45 {
		t.Errorf("list and detail at 100: %v %+v", mid.visible, mid.rects)
	}
	one := computeLayout(70, 40, paneList, 28, two)
	if len(one.visible) != 1 || one.visible[0] != paneList || one.rects[paneList].w != 70 {
		t.Errorf("one pane = %+v", one)
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
