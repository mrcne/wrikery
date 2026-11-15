package ui

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The operations a control request names, the same words internal/control puts on the socket.
// The two sets are kept equal by a test in cmd/wrikery, the one package that sees both.
const (
	OpFilter  = "filter"
	OpSelect  = "select"
	OpFolder  = "folder"
	OpShape   = "shape"
	OpScreen  = "screen"
	OpRefresh = "refresh"
	OpShow    = "show"
)

// screenWords are the words a screen request opens a screen by and a show request names it by.
var screenWords = map[string]screen{
	"tasks": screenMain, "timesheet": screenTimesheet, "changes": screenChanges, "inbox": screenInbox, "settings": screenSettings,
}

// control answers a request from the command line, the operations a script can ask for.
// Each one goes through the code its key uses, so a command and a key leave the interface in the same state.
// A box, the search, the help, a filter field being typed in or the first run screen refuse the request: the person is in the middle of something.
// A show request changes nothing, so it is answered all the same and says that the interface is busy.
func (m Model) control(msg ControlMsg) (tea.Model, tea.Cmd) {
	if !msg.Deadline.IsZero() && time.Now().After(msg.Deadline) {
		return m, nil
	}
	if msg.Op == OpShow {
		state := m.shown()
		msg.reply(ControlReply{State: &state})
		return m, nil
	}
	if m.busy() {
		return m.answer(msg, "", errors.New("a box or a filter field is open in the interface"))
	}
	switch msg.Op {
	case OpFilter:
		f := rowFilter{}
		if len(msg.Importance) > 0 {
			f.importance = map[string]bool{}
			for _, level := range msg.Importance {
				f.importance[level] = true
			}
		}
		switch msg.Person {
		case "":
		case "me":
			if m.ref.meID == "" {
				return m.answer(msg, "", errors.New("the interface does not know who you are yet"))
			}
			f.person = m.ref.meID
		default:
			f.person = msg.Person
		}
		if len(msg.Statuses) > 0 {
			f.statuses = map[string]bool{}
			for _, name := range msg.Statuses {
				f.statuses[statusKey(name)] = true
			}
		}
		cmd := m.setNarrow(f)
		text := "filter cleared"
		if f.active() {
			text = "showing " + f.summary(m.ref)
		}
		next, _ := m.answer(msg, text, nil)
		return next, cmd
	case OpSelect:
		var leaving tea.Cmd
		if m.screen == screenInbox {
			leaving = m.leaveInbox()
		}
		next, cmd := m.jumpToTask(msg.TaskID, msg.ParentIDs, m.mine(msg.ResponsibleIDs))
		msg.send("selected", nil)
		return next, tea.Batch(cmd, leaving)
	case OpFolder:
		if !m.sidebar.reveal(msg.FolderID) {
			return m.answer(msg, "", errors.New("not in the sidebar, follow its space"))
		}
		// Enter on the node: the main screen, the node selected, its tasks loaded, the keys on the list.
		// The pin changes reveal may have queued are written before the reply, on the model that is returned.
		n, _ := m.sidebar.current()
		closing := m.leaveScreen()
		m.screen, m.selectedNode, m.unasked = screenMain, n, false
		m.focus = paneList
		if m.shape == shapeBoard {
			m.focus = paneBoard
		}
		m.syncPaneSizes()
		cmd := tea.Batch(closing, m.loadTasks(n, m.sidebar.crumb(n), "", false), m.flushPins())
		next, _ := m.answer(msg, "opened "+n.title, nil)
		return next, cmd
	case OpShape:
		var want shape
		switch msg.Shape {
		case "list":
			want = shapeList
		case "board":
			want = shapeBoard
		default:
			return m.answer(msg, "", fmt.Errorf("no shape %q, list or board", msg.Shape))
		}
		if m.shape != want {
			m.toggleShape()
		}
		return m.answer(msg, msg.Shape, nil)
	case OpScreen:
		want, ok := screenWords[msg.Screen]
		if !ok {
			return m.answer(msg, "", fmt.Errorf("no screen %q, tasks, timesheet, changes, inbox or settings", msg.Screen))
		}
		cmd := m.openScreen(want)
		next, _ := m.answer(msg, msg.Screen, nil)
		return next, cmd
	case OpRefresh:
		if m.opts.Offline {
			// A command must not end the mode the person chose, going online is the person's R.
			return m.answer(msg, "", errors.New("offline mode, R in the interface goes online"))
		}
		if m.opts.Hooks.Refresh == nil {
			return m.answer(msg, "", errors.New("refresh is not available"))
		}
		next, cmd := m.refresh()
		msg.send("refreshing", nil)
		return next, cmd
	}
	return m.answer(msg, "", fmt.Errorf("the running interface does not know the operation %q, restart it", msg.Op))
}

// typing says whether a filter field has the keys, the list's or the sidebar's: not an overlay, but every key goes to it.
func (m Model) typing() bool {
	return m.list.filtering || m.sidebar.filtering
}

// busy says whether something has the keys that a request must not change the interface under:
// the first run screen, an overlay, or a filter field being typed in.
func (m Model) busy() bool {
	return m.screen == screenFirstRun || m.overlay != overlayNone || m.typing()
}

// shown is the state a show request answers: the screen, and of the tasks screen the shape, the node the list is on and the filter box's choice.
// The task is the one the action keys act on, which only the tasks screen and the pane beside the timesheet grid have,
// so a script that reads it knows what a key or a write on "this task" would reach, and nothing while a pane is one read behind.
func (m Model) shown() ControlState {
	s := ControlState{Shape: "list", Busy: m.busy(), Importance: []string{}, Statuses: []string{}}
	if m.screen == screenFirstRun {
		// No request opens it, so it has no word in the table.
		s.Screen = "sign-in"
	}
	for word, sc := range screenWords {
		if sc == m.screen {
			s.Screen = word
		}
	}
	if m.shape == shapeBoard {
		s.Shape = "board"
	}
	if n := m.selectedNode; n.kind != nodeNone {
		s.FolderID, s.FolderTitle = n.id, n.title
	}
	if m.screen == screenMain || m.screen == screenTimesheet {
		if t, ok := m.selectedTask(); ok {
			s.TaskID, s.TaskTitle = t.ID, t.Title
		}
	}
	f := m.list.narrow
	for _, level := range importanceLevels {
		if f.importance[level] {
			s.Importance = append(s.Importance, level)
		}
	}
	switch f.person {
	case "":
	case unassigned:
		s.PersonID, s.PersonName = unassigned, "Unassigned"
	default:
		s.PersonID, s.PersonName = f.person, contactName(f.person, m.ref)
	}
	if len(f.statuses) > 0 {
		s.Statuses = statusNames(f.statuses, m.ref)
	}
	return s
}

// answer sends the reply and returns the model as it is, for the operations that change nothing more.
func (m Model) answer(msg ControlMsg, text string, err error) (tea.Model, tea.Cmd) {
	msg.send(text, err)
	return m, nil
}

func (msg ControlMsg) send(text string, err error) {
	msg.reply(ControlReply{Text: text, Err: err})
}

// reply puts the answer in the channel without ever waiting: the sender buffers one, and a full channel means nobody listens any more.
func (msg ControlMsg) reply(r ControlReply) {
	if msg.Reply == nil {
		return
	}
	select {
	case msg.Reply <- r:
	default:
	}
}

// openScreen switches to a screen the way its key does, so a key and a control request run the same loads.
// Leaving the timesheet closes the pane beside its grid first. The screen already in view is left alone.
func (m *Model) openScreen(s screen) tea.Cmd {
	if m.screen == s {
		return nil
	}
	closing := m.leaveScreen()
	m.screen = s
	switch s {
	case screenChanges:
		return tea.Batch(closing, m.loadChanges())
	case screenInbox:
		return tea.Batch(closing, m.loadInbox())
	case screenTimesheet:
		return tea.Batch(closing, m.loadWeek(time.Time{}))
	case screenSettings:
		return tea.Batch(closing, m.loadScopes(), m.loadHost())
	}
	return closing
}
