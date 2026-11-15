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
		{"list filter", func(m *Model) { m.list.filtering = true }},
		{"sidebar filter", func(m *Model) { m.sidebar.filtering = true }},
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
	m, r := control(t, m, ControlMsg{Op: "select", TaskID: "T7", ParentIDs: []string{"API"}})
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
	}{{"timesheet", screenTimesheet}, {"changes", screenChanges}, {"inbox", screenInbox}, {"settings", screenSettings}, {"tasks", screenMain}} {
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

func TestControlSelectPicksAParentTheSidebarHas(t *testing.T) {
	m := controlRoot(t)
	m, r := control(t, m, ControlMsg{Op: "select", TaskID: "T7", ParentIDs: []string{"NOPE", "API"}})
	if r.Err != nil {
		t.Fatalf("reply = %+v", r)
	}
	if n, _ := m.sidebar.current(); n.id != "API" {
		t.Errorf("sidebar on %q, want the first folder of the task that is in the sidebar", n.id)
	}
}

// The user's own task with no folder in the sidebar lands in My tasks, the way a pick from the search does.
func TestControlSelectLandsAnOwnTaskInMyTasks(t *testing.T) {
	m := controlRoot(t)
	m, r := control(t, m, ControlMsg{Op: "select", TaskID: "T7", ParentIDs: []string{"NOPE"}, ResponsibleIDs: []string{"U1"}})
	if r.Err != nil {
		t.Fatalf("reply = %+v", r)
	}
	if n, _ := m.sidebar.current(); n.kind != nodeMe || m.selectedNode.kind != nodeMe {
		t.Errorf("sidebar on %q, node %q, want My tasks", n.id, m.selectedNode.id)
	}
}

func TestControlFilterKeepsThePaneBesideTheGrid(t *testing.T) {
	m := controlRoot(t)
	m.screen, m.timesheet.detailOpen, m.selectedTaskID = screenTimesheet, true, "T9"
	m, r := control(t, m, ControlMsg{Op: "filter", Importance: []string{"High"}})
	if r.Err != nil || m.selectedTaskID != "T9" || m.screen != screenTimesheet {
		t.Errorf("reply %+v, task %q, screen %v, want the pane beside the grid left alone", r, m.selectedTaskID, m.screen)
	}
}

func TestControlFolderWritesThePinChangesBeforeAnswering(t *testing.T) {
	m := controlRoot(t)
	m.sidebar.pinnedOnly = true
	m.sidebar.rebuild()
	m, r := control(t, m, ControlMsg{Op: "folder", FolderID: "ONC"})
	if r.Err != nil {
		t.Fatalf("reply = %+v", r)
	}
	if len(m.sidebar.changes) != 0 {
		t.Errorf("the returned model still holds %d pin changes, want them written before the reply", len(m.sidebar.changes))
	}
}

func show(t *testing.T, m Model) ControlState {
	t.Helper()
	_, r := control(t, m, ControlMsg{Op: OpShow})
	if r.Err != nil || r.State == nil || r.Text != "" {
		t.Fatalf("show replied %+v, want the state alone", r)
	}
	return *r.State
}

func TestControlShowAnswersTheScreenTheFolderAndTheFilter(t *testing.T) {
	m := controlRoot(t)
	s := show(t, m)
	if s.Screen != "tasks" || s.Shape != "list" || s.Busy || s.FolderID != "" || s.TaskID != "" || s.PersonID != "" {
		t.Errorf("before a folder is open: %+v, want the tasks screen as a list and nothing else", s)
	}
	if s.Importance == nil || s.Statuses == nil || len(s.Importance)+len(s.Statuses) != 0 {
		t.Errorf("no filter: importance %#v, statuses %#v, want two empty lists, never nil", s.Importance, s.Statuses)
	}
	m, _ = control(t, m, ControlMsg{Op: OpFolder, FolderID: "API"})
	m, _ = control(t, m, ControlMsg{Op: OpShape, Shape: "board"})
	m, _ = control(t, m, ControlMsg{Op: OpFilter, Importance: []string{"Low", "High"}, Person: "me", Statuses: []string{"in progress"}})
	s = show(t, m)
	if s.Shape != "board" || s.FolderID != "API" || s.FolderTitle != "API" {
		t.Errorf("state = %+v, want the board on the API project", s)
	}
	if strings.Join(s.Importance, ",") != "High,Low" || s.PersonID != "U1" || s.PersonName != "Ada Nowak" || strings.Join(s.Statuses, ",") != "In Progress" {
		t.Errorf("filter = %v, %q %q, %v, want the levels in the box's order, the person by id and name, the status as its workflow spells it",
			s.Importance, s.PersonID, s.PersonName, s.Statuses)
	}
	m, _ = control(t, m, ControlMsg{Op: OpFilter, Person: "unassigned"})
	if s = show(t, m); s.PersonID != "unassigned" || s.PersonName != "Unassigned" || len(s.Importance) != 0 {
		t.Errorf("filter = %+v, want unassigned, the word a filter request takes", s)
	}
	// The shape, the folder and the filter are the tasks screen's, another screen in view does not hide them.
	m, _ = control(t, m, ControlMsg{Op: OpFolder, FolderID: "me"})
	for word := range screenWords {
		m, _ = control(t, m, ControlMsg{Op: OpScreen, Screen: word})
		if s = show(t, m); s.Screen != word || s.Shape != "board" || s.FolderID != "me" || s.FolderTitle != "My tasks" || s.PersonID != "unassigned" {
			t.Errorf("on %s: %+v, want the screen named and the tasks screen's shape, folder and filter", word, s)
		}
	}
}

// Reading changes nothing, so show answers where every other request is refused, and says that the keys are taken.
func TestControlShowAnswersWhileABoxIsOpen(t *testing.T) {
	for _, c := range []struct {
		name, screen string
		prep         func(m *Model)
	}{
		{"help", "tasks", func(m *Model) { m.overlay = overlayHelp }},
		{"search", "tasks", func(m *Model) { m.overlay = overlaySearch }},
		{"a box", "tasks", func(m *Model) { m.overlay = overlayDialog }},
		{"first run", "sign-in", func(m *Model) { m.screen = screenFirstRun }},
		{"list filter", "tasks", func(m *Model) { m.list.filtering = true }},
		{"sidebar filter", "tasks", func(m *Model) { m.sidebar.filtering = true }},
	} {
		m := controlRoot(t)
		c.prep(&m)
		if s := show(t, m); !s.Busy || s.Screen != c.screen {
			t.Errorf("%s: %+v, want busy on %s", c.name, s, c.screen)
		}
	}
}

// The task is the one a key would act on. A script that writes to "the task in view" must never be handed the task before.
func TestControlShowNamesTheTaskTheKeysActOn(t *testing.T) {
	m := pump(demoModel(t), focusMsg{pane: paneList})
	row, ok := m.list.current()
	if !ok {
		t.Fatal("the demo list has no row")
	}
	if s := show(t, m); s.TaskID != row.task.ID || s.TaskTitle != row.task.Title || s.FolderID != m.selectedNode.id {
		t.Errorf("state = %+v, want the row under the cursor %q in %q", s, row.task.ID, m.selectedNode.id)
	}
	// The selection moved to a task the list does not have and the pane with the keys still holds the row's task: nothing, for that one read.
	behind := m
	behind.focus = paneDetail
	behind.selectTask("IEAA-ELSEWHERE")
	if s := show(t, behind); s.TaskID != "" {
		t.Errorf("with the pane one read behind show named %q, want no task", s.TaskID)
	}
	// The other screens have no task under the keys, the timesheet has the one in the pane beside its grid.
	changes, _ := control(t, m, ControlMsg{Op: OpScreen, Screen: "changes"})
	if s := show(t, changes); s.TaskID != "" || s.FolderID != m.selectedNode.id {
		t.Errorf("on the changes screen: %+v, want no task and the folder kept", s)
	}
	sheet := m
	sheet.screen = screenTimesheet
	if s := show(t, sheet); s.TaskID != "" {
		t.Errorf("on the grid with no pane open show named %q", s.TaskID)
	}
	sheet.timesheet.detailOpen = true
	if s := show(t, sheet); s.TaskID != row.task.ID {
		t.Errorf("with the pane beside the grid on %q show named %q", row.task.ID, s.TaskID)
	}
}
