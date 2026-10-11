package ui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

// rootWithTree is a root model on the main screen at the given width, the sidebar on the Platform space.
func rootWithTree(t *testing.T, width int) Model {
	t.Helper()
	m := New(rootTestOptions(t))
	m.width, m.height = width, 40
	m.sidebar.setTree(testTree(), nil)
	m.sidebar.selectByID("PLT")
	m.syncPaneSizes()
	return m
}

func ctrlB(m Model) Model {
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	return next.(Model)
}

func TestCtrlBHidesTheSidebarAndBringsItBackOnItsNode(t *testing.T) {
	m := rootWithTree(t, 160)
	m.focus = paneSidebar
	m = ctrlB(m)
	if !m.sidebarHidden || m.focus != paneList {
		t.Fatalf("after ctrl+b hidden=%v focus=%v, want hidden with the list focused", m.sidebarHidden, m.focus)
	}
	lay := m.layout()
	if len(lay.visible) != 2 || lay.visible[0] != paneList || lay.rects[paneList].w != 88 || lay.rects[paneDetail].w != 72 {
		t.Errorf("layout with the sidebar hidden = %v %v, want the list 88 and the detail 72", lay.visible, lay.rects)
	}
	if v, _ := m.opts.Store.GetMeta(context.Background(), store.MetaKeySidebarHidden); v != "1" {
		t.Errorf("meta %s = %q, want 1", store.MetaKeySidebarHidden, v)
	}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != paneDetail {
		t.Errorf("tab from the list focused %v, want the detail", m.focus)
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != paneList {
		t.Errorf("tab from the detail focused %v, want the list, the sidebar is hidden", m.focus)
	}
	m = ctrlB(m)
	if m.sidebarHidden || m.focus != paneSidebar {
		t.Fatalf("after the second ctrl+b hidden=%v focus=%v, want shown with the sidebar focused", m.sidebarHidden, m.focus)
	}
	if n, ok := m.sidebar.current(); !ok || n.id != "PLT" {
		t.Errorf("the sidebar came back on %+v, want PLT", n)
	}
	if v, _ := m.opts.Store.GetMeta(context.Background(), store.MetaKeySidebarHidden); v != "0" {
		t.Errorf("meta after showing = %q, want 0", v)
	}
}

// h on the list names the sidebar and esc on a one pane screen steps back a pane, neither may land on a hidden sidebar.
func TestAHiddenSidebarIsNotFocusedByTheListOrByEsc(t *testing.T) {
	m := rootWithTree(t, 70)
	m.focus = paneSidebar
	m = ctrlB(m)
	next, _ := m.Update(focusMsg{pane: paneSidebar})
	m = next.(Model)
	if m.focus != paneList {
		t.Errorf("h on the list focused %v, want the list to keep the focus", m.focus)
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.focus != paneList {
		t.Errorf("esc on the one pane screen focused %v, want the list", m.focus)
	}
	if lay := m.layout(); len(lay.visible) != 1 || lay.visible[0] != paneList {
		t.Errorf("layout = %v, want the list alone", lay.visible)
	}
}

func TestTheSidebarStaysHiddenAcrossRestarts(t *testing.T) {
	m := New(rootTestOptions(t))
	m.width, m.height = 160, 40
	next, _ := m.Update(treeLoadedMsg{nodes: testTree(), saved: &sidebarState{hidden: true}})
	m = next.(Model)
	if !m.sidebarHidden {
		t.Fatal("a saved hidden sidebar is shown after the tree load")
	}
	if lay := m.layout(); len(lay.visible) != 2 || lay.visible[0] != paneList {
		t.Errorf("layout = %v, want the list and the detail", lay.visible)
	}
}

func TestTheBoardCyclesWithoutAHiddenSidebar(t *testing.T) {
	m := rootWithTree(t, 160)
	m.shape, m.focus = shapeBoard, paneBoard
	m = ctrlB(m)
	if !m.sidebarHidden || m.focus != paneBoard {
		t.Fatalf("ctrl+b on the board: hidden=%v focus=%v, want hidden with the board focused", m.sidebarHidden, m.focus)
	}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if m.focus != paneDetail {
		t.Errorf("shift+tab from the board focused %v, want the detail", m.focus)
	}
	m = ctrlB(m)
	if m.sidebarHidden || m.focus != paneSidebar {
		t.Errorf("second ctrl+b on the board: hidden=%v focus=%v, want shown with the sidebar focused", m.sidebarHidden, m.focus)
	}
}

func TestCtrlBOffTheMainScreenDoesNothing(t *testing.T) {
	m := rootWithTree(t, 160)
	m.screen = screenTimesheet
	m = ctrlB(m)
	if m.sidebarHidden {
		t.Error("ctrl+b on the timesheet hid the sidebar")
	}
}

// The tree load carries the saved flag with the pins, so the sidebar is hidden from the first frame that has a tree.
func TestTheTreeLoadReadsTheHiddenFlag(t *testing.T) {
	m := New(rootTestOptions(t))
	ctx := context.Background()
	if err := m.opts.Store.Scopes().Upsert(ctx, store.Scope{ID: "S1", Kind: store.ScopeKindSpace, Title: "Platform", Followed: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.opts.Store.SetMeta(ctx, store.MetaKeySidebarHidden, "1"); err != nil {
		t.Fatal(err)
	}
	msg, ok := m.loadTree()().(treeLoadedMsg)
	if !ok || msg.saved == nil || !msg.saved.hidden {
		t.Fatalf("loadTree gave %#v, want a treeLoadedMsg with saved.hidden", msg)
	}
}
