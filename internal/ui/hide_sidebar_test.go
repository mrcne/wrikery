package ui

import (
	"context"
	"slices"
	"testing"

	"github.com/charmbracelet/bubbles/key"
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

func pressS(m Model) Model {
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	return next.(Model)
}

// offersWayBack says whether the hint row names the sidebar key as the way to show the pane again.
func offersWayBack(m Model) bool {
	return slices.ContainsFunc(m.hintBindings(), func(b key.Binding) bool { return b.Help().Desc == "show sidebar" })
}

func TestSHidesTheSidebarAndBringsItBackOnItsNode(t *testing.T) {
	m := rootWithTree(t, 160)
	m.focus = paneSidebar
	m = pressS(m)
	if !m.sidebarHidden || m.focus != paneList {
		t.Fatalf("after S hidden=%v focus=%v, want hidden with the list focused", m.sidebarHidden, m.focus)
	}
	lay := m.layout()
	if len(lay.visible) != 2 || lay.visible[0] != paneList || lay.rects[paneList].w != 88 || lay.rects[paneDetail].w != 72 {
		t.Errorf("layout with the sidebar hidden = %v %v, want the list 88 and the detail 72", lay.visible, lay.rects)
	}
	if v, _ := m.opts.Store.GetMeta(context.Background(), store.MetaKeySidebarHidden); v != "1" {
		t.Errorf("meta %s = %q, want 1", store.MetaKeySidebarHidden, v)
	}
	if d := m.keys.HideSidebar.Help().Desc; d != "show sidebar" || !offersWayBack(m) {
		t.Errorf("while hidden the key reads %q and the hint row offers it: %v, want show sidebar in both", d, offersWayBack(m))
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
	m = pressS(m)
	if m.sidebarHidden || m.focus != paneSidebar {
		t.Fatalf("after the second S hidden=%v focus=%v, want shown with the sidebar focused", m.sidebarHidden, m.focus)
	}
	if n, ok := m.sidebar.current(); !ok || n.id != "PLT" {
		t.Errorf("the sidebar came back on %+v, want PLT", n)
	}
	if v, _ := m.opts.Store.GetMeta(context.Background(), store.MetaKeySidebarHidden); v != "0" {
		t.Errorf("meta after showing = %q, want 0", v)
	}
	if d := m.keys.HideSidebar.Help().Desc; d != "hide sidebar" || offersWayBack(m) {
		t.Errorf("while shown the key reads %q and the hint row offers the way back: %v, want hide sidebar and no hint", d, offersWayBack(m))
	}
}

// ctrl+b is what editors use for the same thing, it stays as a second key for terminals that pass it on.
func TestCtrlBHidesTheSidebarToo(t *testing.T) {
	m := rootWithTree(t, 160)
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	if m = next.(Model); !m.sidebarHidden {
		t.Error("ctrl+b left the sidebar shown")
	}
}

// h on the list names the sidebar and esc on a one pane screen steps back a pane, neither may land on a hidden sidebar.
func TestAHiddenSidebarIsNotFocusedByTheListOrByEsc(t *testing.T) {
	m := rootWithTree(t, 70)
	m.focus = paneSidebar
	m = pressS(m)
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

func TestTheBoardCyclesWithoutAHiddenSidebar(t *testing.T) {
	m := rootWithTree(t, 160)
	m.shape, m.focus = shapeBoard, paneBoard
	m = pressS(m)
	if !m.sidebarHidden || m.focus != paneBoard {
		t.Fatalf("S on the board: hidden=%v focus=%v, want hidden with the board focused", m.sidebarHidden, m.focus)
	}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if m.focus != paneDetail {
		t.Errorf("shift+tab from the board focused %v, want the detail", m.focus)
	}
	m = pressS(m)
	if m.sidebarHidden || m.focus != paneSidebar {
		t.Errorf("second S on the board: hidden=%v focus=%v, want shown with the sidebar focused", m.sidebarHidden, m.focus)
	}
}

// The other screens route their own keys, the sidebar key must not fall through to the main screen's switch.
func TestTheSidebarKeyOffTheMainScreenDoesNothing(t *testing.T) {
	m := rootWithTree(t, 160)
	m.screen = screenTimesheet
	if m = pressS(m); m.sidebarHidden {
		t.Error("S on the timesheet hid the sidebar")
	}
}

// The flag is read by its own start command, so a saved hidden sidebar is away before the tree lands and the settings row knows it.
func TestTheSidebarStaysHiddenAcrossRestarts(t *testing.T) {
	opts := rootTestOptions(t)
	if err := opts.Store.SetMeta(context.Background(), store.MetaKeySidebarHidden, "1"); err != nil {
		t.Fatal(err)
	}
	m := New(opts)
	m.width, m.height = 160, 40
	var flag tea.Msg
	for _, c := range m.Init()().(tea.BatchMsg) {
		if msg, ok := c().(sidebarHiddenMsg); ok {
			flag = msg
		}
	}
	if flag != (sidebarHiddenMsg{hidden: true}) {
		t.Fatalf("Init gave %#v for the sidebar, want the hidden flag", flag)
	}
	next, _ := m.Update(flag)
	m = next.(Model)
	if !m.sidebarHidden {
		t.Fatal("a saved hidden sidebar is shown after its flag landed")
	}
	if lay := m.layout(); len(lay.visible) != 2 || lay.visible[0] != paneList {
		t.Errorf("layout = %v, want the list and the detail", lay.visible)
	}
	rows := m.settings.rows()
	i := slices.IndexFunc(rows, func(r settingRow) bool { return r.label == "Sidebar" })
	if i < 0 || rows[i].value != "hidden" {
		t.Errorf("rows = %+v, want the Sidebar row reading hidden", rows)
	}
}
