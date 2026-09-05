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
