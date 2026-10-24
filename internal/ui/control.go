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
)

// control answers a request from the command line, the operations a script can ask for.
// Each one goes through the code its key uses, so a command and a key leave the interface in the same state.
// A box, the search, the help, a filter field being typed in or the first run screen refuse the request: the person is in the middle of something.
func (m Model) control(msg ControlMsg) (tea.Model, tea.Cmd) {
	if !msg.Deadline.IsZero() && time.Now().After(msg.Deadline) {
		return m, nil
	}
	if m.screen == screenFirstRun || m.overlay != overlayNone || m.typing() {
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
		next, cmd := m.jumpToTask(msg.TaskID, msg.ParentIDs, m.mine(msg.ResponsibleIDs))
		msg.send("selected", nil)
		return next, cmd
	case OpFolder:
		if !m.sidebar.reveal(msg.FolderID) {
			return m.answer(msg, "", errors.New("not in the sidebar, follow its space"))
		}
		// Enter on the node: the main screen, the node selected, its tasks loaded, the keys on the list.
		// The pin changes reveal may have queued are written before the reply, on the model that is returned.
		n, _ := m.sidebar.current()
		closing := m.closeTimesheetDetail()
		m.screen, m.selectedNode = screenMain, n
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
		var want screen
		switch msg.Screen {
		case "tasks":
			want = screenMain
		case "timesheet":
			want = screenTimesheet
		case "changes":
			want = screenChanges
		case "settings":
			want = screenSettings
		default:
			return m.answer(msg, "", fmt.Errorf("no screen %q, tasks, timesheet, changes or settings", msg.Screen))
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

// answer sends the reply and returns the model as it is, for the operations that change nothing more.
func (m Model) answer(msg ControlMsg, text string, err error) (tea.Model, tea.Cmd) {
	msg.send(text, err)
	return m, nil
}

// send puts the reply in the channel without ever waiting: the sender buffers one, and a full channel means nobody listens any more.
func (msg ControlMsg) send(text string, err error) {
	if msg.Reply == nil {
		return
	}
	select {
	case msg.Reply <- ControlReply{Text: text, Err: err}:
	default:
	}
}

// openScreen switches to a screen the way its key does, so a key and a control request run the same loads.
// Leaving the timesheet closes the pane beside its grid first. The screen already in view is left alone.
func (m *Model) openScreen(s screen) tea.Cmd {
	if m.screen == s {
		return nil
	}
	var closing tea.Cmd
	if m.screen == screenTimesheet {
		closing = m.closeTimesheetDetail()
	}
	m.screen = s
	switch s {
	case screenChanges:
		return tea.Batch(closing, m.loadChanges())
	case screenTimesheet:
		return tea.Batch(closing, m.loadWeek(time.Time{}))
	case screenSettings:
		return tea.Batch(closing, m.loadScopes(), m.loadHost())
	}
	return closing
}
