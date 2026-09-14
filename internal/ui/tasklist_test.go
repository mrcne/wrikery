package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestDueLabel(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) // Thursday
	cases := []struct {
		due, status string
		want        string
		overdue     bool
	}{
		{"", "Active", "--", false},
		{"2026-09-03", "Active", "today", false},
		{"2026-09-04", "Active", "Fri", false},
		{"2026-09-09", "Active", "Wed", false},
		{"2026-09-12", "Active", "12 Sep", false},
		{"2026-09-01", "Active", "1 Sep", true},
		{"2026-09-01", "Completed", "done", false},
		{"2026-09", "Active", "2026-09", false},
	}
	for _, c := range cases {
		task := store.Task{Status: c.status}
		if c.due != "" {
			task.Dates = &store.TaskDates{Due: c.due}
		}
		got, overdue := dueLabel(task, now)
		if got != c.want || overdue != c.overdue {
			t.Errorf("dueLabel(%s, %s) = %q, %v; want %q, %v", c.due, c.status, got, overdue, c.want, c.overdue)
		}
	}
}

func TestTaskListFilterAndDoneToggle(t *testing.T) {
	var l taskListModel
	l.keys = defaultKeyMap()
	l.filter = textinput.New()
	tasks := []store.Task{
		{ID: "1", Title: "Fix auth retry", Status: "Active"},
		{ID: "2", Title: "Rotate keys", Status: "Active"},
		{ID: "3", Title: "Old fix", Status: "Completed"},
	}
	l.setRows("F1", "Platform / API", tasks, map[string]store.OutboxState{"2": store.StatePending}, "")
	if len(l.rows) != 2 {
		t.Fatalf("done tasks shown by default: %d rows", len(l.rows))
	}
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	if len(l.rows) != 3 {
		t.Fatalf("z did not show done tasks: %d rows", len(l.rows))
	}
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "fix" {
		l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(l.rows) != 2 || !l.filtering {
		t.Fatalf("filter 'fix' -> %d rows, filtering=%v", len(l.rows), l.filtering)
	}
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(l.rows) != 3 || l.filtering {
		t.Errorf("esc should clear the filter: %d rows", len(l.rows))
	}
	if !strings.Contains(l.title(), "Platform / API") || !strings.Contains(l.title(), "3") {
		t.Errorf("title = %q", l.title())
	}
}

func TestInitials(t *testing.T) {
	// Written as \u escapes so the file stays plain ASCII.
	// U+0141 and U+017B are the capital L-stroke and Z-dot-above letters that open the Polish first and last name below.
	contacts := map[string]store.Contact{
		"C1": {FirstName: "Celina", LastName: "Wrona"},
		"ME": {FirstName: "Ada", LastName: "Nowak"},
		"X1": {FirstName: "\u0141ukasz", LastName: "\u017Buk"},
	}
	if want, got := "\u0141\u017B", initials([]string{"X1"}, contacts, ""); got != want {
		t.Errorf("initials(X1) = %q, want %q", got, want)
	}
	if got := initials([]string{"C1", "ME"}, contacts, "ME"); got != "AN+" {
		t.Errorf("initials(C1, ME with meID=ME) = %q, want %q", got, "AN+")
	}
}

func TestTasksLoadedMsgSkipsReselectWhenCursorDidNotMove(t *testing.T) {
	m := Model{keys: defaultKeyMap()}
	m.list = newTaskList(m.keys)
	tasks := []store.Task{{ID: "1", Title: "a", Status: "Active"}, {ID: "2", Title: "b", Status: "Active"}}
	m.selectedTaskID = "1"
	m.selectedNode = treeNode{id: "F1"}
	_, cmd := m.Update(tasksLoadedMsg{nodeID: "F1", tasks: tasks})
	if cmd != nil {
		if msg, ok := cmd().(taskSelectedMsg); ok {
			t.Errorf("reselected task %s though the cursor stayed on the already selected task", msg.id)
		}
	}
}

func TestTasksLoadedMsgReselectsWhenCursorMoved(t *testing.T) {
	m := Model{keys: defaultKeyMap()}
	m.list = newTaskList(m.keys)
	tasks := []store.Task{{ID: "1", Title: "a", Status: "Active"}, {ID: "2", Title: "b", Status: "Active"}}
	m.selectedTaskID = "9"
	m.selectedNode = treeNode{id: "F1"}
	_, cmd := m.Update(tasksLoadedMsg{nodeID: "F1", tasks: tasks})
	if cmd == nil {
		t.Fatal("expected a command reselecting the new task")
	}
	msg, ok := cmd().(taskSelectedMsg)
	if !ok || msg.id != "1" {
		t.Errorf("Update() cmd = %#v, want taskSelectedMsg{id: \"1\"}", cmd())
	}
}

func TestTaskListReselectsByID(t *testing.T) {
	var l taskListModel
	l.keys = defaultKeyMap()
	l.filter = textinput.New()
	tasks := []store.Task{{ID: "1", Title: "a", Status: "Active"}, {ID: "2", Title: "b", Status: "Active"}}
	l.setRows("F1", "", tasks, nil, "")
	l.cursor = 1
	l.setRows("F1", "", []store.Task{{ID: "0", Title: "new", Status: "Active"}, tasks[0], tasks[1]}, nil, "")
	if row, _ := l.current(); row.task.ID != "2" {
		t.Errorf("selection lost, now on %s", row.task.ID)
	}
}

func pressKey(l taskListModel, k string) taskListModel {
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	return l
}

func TestTaskListSectionsSkipTheCursorAndCountLines(t *testing.T) {
	l := newTaskList(defaultKeyMap())
	l.ref = refData{meID: "ME", contacts: map[string]store.Contact{
		"ME": {FirstName: "Ada", LastName: "Nowak"},
		"B1": {FirstName: "Bartek", LastName: "Lis"},
	}, statuses: map[string]store.CustomStatus{"S2": {ID: "S2", Name: "In Progress", Group: "Active"}}}
	l.height = 3
	tasks := []store.Task{
		{ID: "1", Title: "Mine", Status: "Active", CustomStatusID: "S2", ResponsibleIDs: []string{"ME"}},
		{ID: "2", Title: "Theirs", Status: "Active", CustomStatusID: "S2", ResponsibleIDs: []string{"B1"}},
		{ID: "3", Title: "Also theirs", Status: "Active", CustomStatusID: "S2", ResponsibleIDs: []string{"B1"}},
		{ID: "4", Title: "Shared", Status: "Active", CustomStatusID: "S2", ResponsibleIDs: []string{"B1", "ME"}},
	}
	l.setRows("F1", "API", tasks, nil, "")
	l.setGroup(groupAssignee)
	// The shared task is a row under both people, the title still counts it once.
	if got := l.title(); !strings.Contains(got, "Tasks: API, by assignee (4)") || len(l.rows) != 5 {
		t.Errorf("title = %q over %d rows", got, len(l.rows))
	}
	if cur, _ := l.current(); cur.task.ID != "1" {
		t.Fatalf("selection lost on regroup, now on %s", cur.task.ID)
	}
	if l.visual(2) != 4 {
		t.Errorf("visual(2) = %d, want 4: two section lines sit above the third row", l.visual(2))
	}
	l = pressKey(l, "}")
	if cur, _ := l.current(); cur.task.ID != "2" {
		t.Errorf("} should land on the first row of the next section, got %s", cur.task.ID)
	}
	if l.offset != 2 {
		t.Errorf("offset = %d, want 2: at height 3 the section line above the cursor row stays in view", l.offset)
	}
	l = pressKey(l, "{")
	if cur, _ := l.current(); cur.task.ID != "1" {
		t.Errorf("{ should go back to the first row of the previous section, got %s", cur.task.ID)
	}
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	view := l.View(th, l.ref, time.Time{}, 60, 8, true)
	for _, want := range []string{"-- Ada Nowak (me) (2) ", "-- Bartek Lis (3) ", "In Progress"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	l.setGroup(groupNone)
	if l.visual(2) != 2 || strings.Contains(l.title(), "by ") {
		t.Errorf("without a grouping there are no section lines: visual(2) = %d, title %q", l.visual(2), l.title())
	}
}

func TestCycleGroupSkipsStatusOnTheBoard(t *testing.T) {
	l := newTaskList(defaultKeyMap())
	l.setGroup(groupAssignee)
	l.cycleGroup(true)
	if l.groupBy != groupNone {
		t.Errorf("after assignee the board goes back to none, got %v", l.groupBy)
	}
	l.setGroup(groupAssignee)
	l.cycleGroup(false)
	if l.groupBy != groupStatus {
		t.Errorf("the list goes on to status, got %v", l.groupBy)
	}
}

func TestFilterPullsTheWindowBackWhenTheListShrinks(t *testing.T) {
	var l taskListModel
	l.keys = defaultKeyMap()
	l.filter = textinput.New()
	var tasks []store.Task
	for i := range 30 {
		title := fmt.Sprintf("Task %d", i)
		if i%10 == 3 {
			title += " auth"
		}
		tasks = append(tasks, store.Task{ID: fmt.Sprint(i), Title: title, Status: "Active"})
	}
	l.setRows("F1", "crumb", tasks, nil, "")
	l.height = 10
	l.cursor = 25
	l.scroll()
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "auth" {
		l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyEnter})
	out := l.View(NewTheme(config.UIConfig{}), refData{}, time.Now(), 80, 10, true)
	for _, want := range []string{"Task 3 auth", "Task 13 auth", "Task 23 auth"} {
		if !strings.Contains(out, want) {
			t.Errorf("filtered list lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Task 23 auth") && !strings.HasPrefix(line, "> ") {
			t.Errorf("the cursor is not on the last match: %q", line)
		}
	}
}

func TestResizePullsTheListWindowBack(t *testing.T) {
	m := New(Options{})
	var tasks []store.Task
	for i := range 30 {
		tasks = append(tasks, store.Task{ID: fmt.Sprint(i), Title: fmt.Sprintf("Task %d", i), Status: "Active"})
	}
	m.list.setRows("F1", "crumb", tasks, nil, "")
	m.list.height = 10
	m.list.cursor = 25
	m.list.scroll()
	if m.list.offset != 16 {
		t.Fatalf("offset = %d before the resize, want 16", m.list.offset)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if got := next.(Model).list; got.offset != 0 || got.height < 30 {
		t.Errorf("after growing to 40 rows offset = %d, height = %d, want the window pulled back to 0", got.offset, got.height)
	}
}

// A jump names the task to select, and a completed one has to be on screen for that,
// so the done toggle turns on for it and stays as it was on a plain reload.
func TestJumpToACompletedTaskTurnsTheDoneToggleOn(t *testing.T) {
	l := newTaskList(defaultKeyMap())
	tasks := []store.Task{{ID: "1", Title: "open", Status: "Active"}, {ID: "2", Title: "done", Status: "Completed"}}
	if found := l.setRows("F1", "API", tasks, nil, "2"); !found || !l.showDone || l.cursor != 1 {
		t.Errorf("jump to the completed task: found %v, showDone %v, cursor %d", found, l.showDone, l.cursor)
	}
	l = newTaskList(defaultKeyMap())
	l.setRows("F1", "API", tasks, nil, "")
	tasks[0].Status = "Completed"
	if found := l.setRows("F1", "API", tasks, nil, ""); found || l.showDone {
		t.Errorf("reload with the selected task completed: found %v, showDone %v, want it gone from the list", found, l.showDone)
	}
}

// Loads and intents travel through the queue, so an answer can land after the sidebar moved on:
// a list for a node no longer selected, or a selection for a row the list no longer has.
// Both are dropped, otherwise an empty folder shows the tasks or the detail of the folder passed on the way.
func TestStaleListLoadAndTaskSelectionAreDropped(t *testing.T) {
	m := New(rootTestOptions(t))
	m.selectedNode = treeNode{id: "F1"}
	tasks := []store.Task{{ID: "T1", Title: "one", Status: "Active"}}
	next, _ := m.Update(tasksLoadedMsg{nodeID: "F1", crumb: "API", tasks: tasks})
	next, _ = next.(Model).Update(taskSelectedMsg{id: "T1"})
	m = next.(Model)
	if m.selectedTaskID != "T1" {
		t.Fatalf("a selection for a row of the list should stand, selected %q", m.selectedTaskID)
	}
	m.selectedNode = treeNode{id: "F2"}
	next, _ = m.Update(tasksLoadedMsg{nodeID: "F2", crumb: "Wishlist"})
	next, cmd := next.(Model).Update(taskSelectedMsg{id: "T1"})
	if got := next.(Model).selectedTaskID; got != "" || cmd != nil {
		t.Errorf("a selection for a row the list no longer has should be dropped, selected %q", got)
	}
	next, cmd = next.(Model).Update(tasksLoadedMsg{nodeID: "F1", crumb: "API", tasks: tasks})
	if m = next.(Model); m.list.nodeID != "F2" || m.list.count != 0 || cmd != nil {
		t.Errorf("a late load for the node left behind should be dropped, list shows %s with %d rows", m.list.nodeID, m.list.count)
	}
}
