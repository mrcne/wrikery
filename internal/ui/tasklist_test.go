package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

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
