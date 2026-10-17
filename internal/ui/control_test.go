package ui

import (
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

// controlRoot is a root model on the main screen with a tree, a known user and one workflow, what a control request lands on.
func controlRoot(t *testing.T) Model {
	t.Helper()
	m := rootWithTree(t, 120)
	m.ref.meID = "U1"
	m.ref.contacts = map[string]store.Contact{"U1": {ID: "U1", FirstName: "Ada", LastName: "Nowak"}}
	m.ref.workflows = []store.Workflow{{ID: "W1", Name: "Default", CustomStatuses: []store.CustomStatus{
		{ID: "S1", Name: "New", Group: "Active"}, {ID: "S2", Name: "In Progress", Group: "Active"}}}}
	return m
}

// control hands the message to Update and returns the reply the interface put in the channel.
func control(t *testing.T, m Model, msg ControlMsg) (Model, ControlReply) {
	t.Helper()
	ch := make(chan ControlReply, 1)
	msg.Reply = ch
	next, _ := m.Update(msg)
	select {
	case r := <-ch:
		return next.(Model), r
	default:
		t.Fatal("the interface sent no reply")
		return m, ControlReply{}
	}
}

func TestControlFilterNarrowsAndRepliesTheSummary(t *testing.T) {
	m := controlRoot(t)
	m, r := control(t, m, ControlMsg{Op: "filter", Importance: []string{"High"}, Person: "me", Statuses: []string{"in progress"}})
	if r.Err != nil || r.Text != "showing High | Ada Nowak (me) | In Progress" {
		t.Errorf("reply = %+v", r)
	}
	n := m.list.narrow
	if !n.importance["High"] || n.person != "U1" || !n.statuses["in progress"] || len(n.statuses) != 1 {
		t.Errorf("narrow = %+v, want the level, the user's own id and the status key", n)
	}
	m, r = control(t, m, ControlMsg{Op: "filter", Person: "unassigned"})
	if r.Err != nil || r.Text != "showing Unassigned" || m.list.narrow.person != unassigned || len(m.list.narrow.importance) != 0 {
		t.Errorf("a second filter replaces the first: %+v, %+v", r, m.list.narrow)
	}
	m, r = control(t, m, ControlMsg{Op: "filter"})
	if r.Err != nil || r.Text != "filter cleared" || m.list.narrow.active() {
		t.Errorf("clear: %+v, %+v", r, m.list.narrow)
	}
}

func TestControlIsBusyWithABoxOpen(t *testing.T) {
	for _, c := range []struct {
		name string
		prep func(m *Model)
	}{
		{"help", func(m *Model) { m.overlay = overlayHelp }},
		{"search", func(m *Model) { m.overlay = overlaySearch }},
		{"first run", func(m *Model) { m.screen = screenFirstRun }},
	} {
		m := controlRoot(t)
		c.prep(&m)
		_, r := control(t, m, ControlMsg{Op: "filter", Importance: []string{"High"}})
		if r.Err == nil || !strings.Contains(r.Err.Error(), "open in the interface") {
			t.Errorf("%s: reply = %+v, want busy", c.name, r)
		}
	}
}

func TestControlSelectShowsTheTask(t *testing.T) {
	m := controlRoot(t)
	m.screen = screenTimesheet
	m, r := control(t, m, ControlMsg{Op: "select", TaskID: "T7", ParentID: "API"})
	if r.Err != nil || r.Text != "selected" {
		t.Errorf("reply = %+v", r)
	}
	n, _ := m.sidebar.current()
	if m.screen != screenMain || m.selectedTaskID != "T7" || n.id != "API" || m.selectedNode.id != "API" {
		t.Errorf("screen %v, task %q, sidebar on %q, node %q, want the main screen on the task's folder", m.screen, m.selectedTaskID, n.id, m.selectedNode.id)
	}
}

func TestControlFolderOpensTheNode(t *testing.T) {
	m := controlRoot(t)
	m.screen = screenChanges
	m, r := control(t, m, ControlMsg{Op: "folder", FolderID: "ONC"})
	if r.Err != nil || r.Text != "opened On-call" {
		t.Errorf("reply = %+v", r)
	}
	n, _ := m.sidebar.current()
	if m.screen != screenMain || n.id != "ONC" || m.selectedNode.id != "ONC" || m.focus != paneList {
		t.Errorf("screen %v, sidebar on %q, node %q, focus %v, want the folder open on the main screen with the list focused", m.screen, n.id, m.selectedNode.id, m.focus)
	}
	m, r = control(t, m, ControlMsg{Op: "folder", FolderID: "me"})
	if r.Err != nil || r.Text != "opened My tasks" || m.selectedNode.kind != nodeMe {
		t.Errorf("me: reply = %+v, node %+v", r, m.selectedNode)
	}
	_, r = control(t, m, ControlMsg{Op: "folder", FolderID: "NOPE"})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "not in the sidebar") {
		t.Errorf("unknown: reply = %+v", r)
	}
}

func TestControlShape(t *testing.T) {
	m := controlRoot(t)
	m, r := control(t, m, ControlMsg{Op: "shape", Shape: "board"})
	if r.Err != nil || r.Text != "board" || m.shape != shapeBoard {
		t.Errorf("board: %+v, shape %v", r, m.shape)
	}
	m, r = control(t, m, ControlMsg{Op: "shape", Shape: "board"})
	if r.Err != nil || r.Text != "board" || m.shape != shapeBoard {
		t.Errorf("board again: %+v, shape %v", r, m.shape)
	}
	m, r = control(t, m, ControlMsg{Op: "shape", Shape: "list"})
	if r.Err != nil || r.Text != "list" || m.shape != shapeList {
		t.Errorf("list: %+v, shape %v", r, m.shape)
	}
	_, r = control(t, m, ControlMsg{Op: "shape", Shape: "circle"})
	if r.Err == nil {
		t.Errorf("a shape that does not exist must be refused")
	}
}

func TestControlOpenScreens(t *testing.T) {
	m := controlRoot(t)
	for _, c := range []struct {
		word string
		want screen
	}{{"timesheet", screenTimesheet}, {"changes", screenChanges}, {"settings", screenSettings}, {"tasks", screenMain}} {
		var r ControlReply
		m, r = control(t, m, ControlMsg{Op: "screen", Screen: c.word})
		if r.Err != nil || r.Text != c.word || m.screen != c.want {
			t.Errorf("%s: reply %+v, screen %v", c.word, r, m.screen)
		}
	}
	_, r := control(t, m, ControlMsg{Op: "screen", Screen: "garden"})
	if r.Err == nil {
		t.Errorf("a screen that does not exist must be refused")
	}
}

func TestControlRefresh(t *testing.T) {
	opts := rootTestOptions(t)
	opts.Hooks.Refresh = nil
	m := New(opts)
	m.width, m.height = 120, 40
	_, r := control(t, m, ControlMsg{Op: "refresh"})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "not available") {
		t.Errorf("without an engine: %+v", r)
	}
	called := 0
	opts.Hooks.Refresh = func() { called++ }
	m = New(opts)
	_, r = control(t, m, ControlMsg{Op: "refresh"})
	if r.Err != nil || r.Text != "refreshing" || called != 1 {
		t.Errorf("with an engine: %+v, called %d", r, called)
	}
}

func TestControlUnknownOperationIsRefused(t *testing.T) {
	_, r := control(t, controlRoot(t), ControlMsg{Op: "dance"})
	if r.Err == nil {
		t.Errorf("reply = %+v, want a refusal", r)
	}
}
