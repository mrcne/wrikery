// Package ui holds the bubbletea application. It reads from the store, writes to the store and the outbox,
// and never talks to the network. main forwards engine events as messages and injects the hooks.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// Hooks are the few things the UI needs from outside the store.
// Any of them may be nil, the UI then reports it instead of failing.
type Hooks struct {
	Refresh     func()
	WakeOutbox  func()
	VerifyToken func(ctx context.Context, token string) (string, error)
	OpenURL     func(url string) error
	Copy        func(text string) error
}

type Options struct {
	Version  string
	Store    *store.Store
	Hooks    Hooks
	Config   config.UIConfig
	Demo     bool
	FirstRun bool
	Now      func() time.Time
}

type screen int

const (
	screenMain screen = iota
	screenTimesheet
	screenIssues
	screenFirstRun
)

type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlaySearch
	overlayDialog
)

type Model struct {
	opts   Options
	width  int
	height int
	theme  Theme
	keys   KeyMap
	help   help.Model

	screen  screen
	overlay overlay
	focus   pane
	shape   shape

	status    statusModel
	firstRun  firstRunModel
	ref       refData
	sidebar   sidebarModel
	list      taskListModel
	board     boardModel
	detail    taskDetailModel
	search    searchModel
	issues    issuesModel
	timesheet timesheetModel
	dialog    dialog

	selectedNode   treeNode
	selectedTaskID string
	pendingSelect  string // task id to reselect once the next tasksLoadedMsg lands, set by reload after an outbox or task change
	pendingDate    string // date a new timesheet entry is for, set while search stands in as its task picker
}

func New(o Options) Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	m := Model{opts: o, theme: NewTheme(o.Config), keys: defaultKeyMap(), help: help.New(), focus: paneList}
	m.sidebar.keys = m.keys
	m.list = newTaskList(m.keys)
	m.detail.keys = m.keys
	m.search = newSearch(m.keys)
	m.issues.keys = m.keys
	m.timesheet.keys = m.keys
	m.board.keys = m.keys
	if m.theme.ASCII {
		// bubbles joins help entries with a bullet and truncates with a real ellipsis, both non ASCII.
		m.help.ShortSeparator, m.help.FullSeparator = "  ", "    "
		m.help.Ellipsis = "..."
	}
	m.status.demo = o.Demo
	if o.Demo {
		m.status.state = "idle"
		m.status.lastSynced = o.Now()
	}
	if o.FirstRun {
		m.screen = screenFirstRun
		m.firstRun = newFirstRun(stepToken, "", m.keys)
	}
	return m
}

// panes is the pane order of the current shape, what tab cycles through and the layout slides over.
func (m Model) panes() []pane {
	if m.shape == shapeBoard {
		return []pane{paneSidebar, paneBoard, paneDetail}
	}
	return []pane{paneSidebar, paneList, paneDetail}
}

func (m Model) layout() layout {
	if m.shape == shapeBoard {
		return computeBoardLayout(m.width, m.height-1, m.focus, m.sidebar.width())
	}
	return computeLayout(m.width, m.height-1, m.focus, m.sidebar.width())
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadRef(), m.loadScopes(), m.loadCounts())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncPaneSizes()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case SyncStateMsg:
		return m.onSyncState(msg.State)
	case StoreChangedMsg:
		return m, m.reload(msg.Entities)
	case OutboxChangedMsg:
		m.status.pending, m.status.failed = msg.Pending, msg.Failed
		if m.screen == screenIssues {
			return m, m.loadIssues()
		}
		if m.selectedNode.kind == nodeNone {
			// The queue can change before the tree lands, and there is no list to reread until a node is picked.
			return m, m.reloadTask()
		}
		// The pending and failed markers live on the task row and on the detail header, so a queue change rereads both.
		return m, tea.Batch(m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode)), m.reloadTask())
	case toastMsg:
		return m, m.status.show(msg.text, msg.isErr)
	case toastExpiredMsg:
		if msg.seq == m.status.toastSeq {
			m.status.toast = ""
		}
		return m, nil
	case errMsg:
		slog.Error("ui", "error", msg.err)
		return m, m.status.show(msg.err.Error(), true)
	case refLoadedMsg:
		m.ref = msg.ref
		// The by assignee grouping and the columns read contacts and workflows off the list's own copy.
		m.list.ref = msg.ref
		m.list.regroup()
		// Contact names and status names are drawn from ref, so the detail has to be built again once it lands.
		m.syncPaneSizes()
		// The tree needs meID for the open task count and statuses for the project glyphs, both live on ref.
		return m, m.loadTree()
	case treeLoadedMsg:
		m.sidebar.setNodes(msg.nodes)
		m.list.folders = newFolderIndex(msg.nodes)
		m.list.regroup()
		m.syncPaneSizes()
		if n, ok := m.sidebar.current(); ok {
			return m, intent(nodeSelectedMsg{node: n})
		}
		return m, nil
	case focusMsg:
		prev := m.focus
		m.focus = msg.pane
		// The children name the list, the shape decides whether that means the list or the board.
		if m.shape == shapeBoard && m.focus == paneList {
			m.focus = paneBoard
		}
		if m.shape == shapeList && m.focus == paneBoard {
			m.focus = paneList
		}
		m.syncPaneSizes()
		return m, m.openedOnFocus(prev)
	case nodeSelectedMsg:
		m.selectedNode = msg.node
		return m, m.loadTasks(msg.node, m.sidebar.crumb(msg.node))
	case tasksLoadedMsg:
		if m.list.nodeID != msg.nodeID && m.pendingSelect == "" {
			m.list.cursor = 0
		}
		found := m.list.setRows(msg.nodeID, msg.crumb, msg.tasks, msg.states, m.pendingSelect)
		m.pendingSelect = ""
		if !found && m.shape == shapeBoard {
			// The selected card left the board, a status move onto a hidden status does that, so the cursor stays in its cell.
			m.list.cursor = m.board.fallback(&m.list)
		}
		m.board.fit(&m.list, m.theme.HidePrefixes)
		if cur, ok := m.list.current(); ok && cur.task.ID != m.selectedTaskID {
			return m, intent(taskSelectedMsg{id: cur.task.ID})
		}
		return m, nil
	case taskSelectedMsg:
		m.selectedTaskID = msg.id
		return m, m.loadTask(msg.id)
	case taskLoadedMsg:
		// The cursor is free to move while the read runs, so an answer for a task nobody is on any more is dropped.
		if msg.task.ID != m.selectedTaskID {
			return m, nil
		}
		m.detail.set(msg)
		m.syncPaneSizes()
		return m, nil
	case firstRunSubmitTokenMsg:
		return m, m.verifyToken(msg.token)
	case firstRunConfirmScopesMsg:
		return m, tea.Batch(m.saveScopes(msg.scopes), m.firstRun.spinner.Tick)
	case firstRunFinishedMsg:
		if m.firstRun.reauth {
			// verifyToken restarted the engine, so a cycle is already running and the bar would otherwise still read as rejected.
			m.status.state = "syncing"
		}
		m.screen = screenMain
		return m, tea.Batch(m.loadRef(), m.loadScopes())
	case tokenVerifiedMsg:
		var cmd tea.Cmd
		m.firstRun, cmd = m.firstRun.Update(msg)
		if msg.err == nil && !m.firstRun.reauth {
			cmd = tea.Batch(cmd, m.loadPicker(), m.firstRun.spinner.Tick)
		}
		return m, cmd
	case runSearchMsg:
		return m, m.runSearch(msg.seq, msg.query)
	case searchResultsMsg:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	case searchOpenMsg:
		return m.openFromSearch(msg.task)
	case searchPickMsg:
		m.overlay, m.search.pickMode = overlayNone, false
		m.search.blur()
		d, cmd := newTimelogDialog(msg.task.ID, msg.task.Title, nil, m.pendingDate, m.opts.Now())
		m.openDialog(d)
		return m, cmd
	case closeDialogMsg:
		m.overlay, m.dialog = overlayNone, nil
		return m, nil
	case openConfirmMsg:
		m.openDialog(confirmDialog(msg))
		return m, nil
	case issuesLoadedMsg:
		m.issues.set(msg.rows)
		return m, nil
	case weekLoadedMsg:
		m.timesheet.set(msg)
		return m, nil
	case loadWeekMsg:
		return m, m.loadWeek(msg.start)
	case newEntryMsg:
		if msg.taskID == "" {
			m.pendingDate = msg.date
			m.overlay = overlaySearch
			cmd := m.search.reset()
			m.search.pickMode = true
			return m, cmd
		}
		d, cmd := newTimelogDialog(msg.taskID, m.timesheet.titleFor(msg.taskID), nil, msg.date, m.opts.Now())
		m.openDialog(d)
		return m, cmd
	case editEntryMsg:
		if timelogLocked(msg.log) {
			return m, m.status.show("this entry is locked or approved in Wrike and cannot be changed", true)
		}
		d, cmd := newTimelogDialog(msg.log.TaskID, m.timesheet.titleFor(msg.log.TaskID), &msg.log, "", m.opts.Now())
		m.openDialog(d)
		return m, cmd
	case deleteEntryMsg:
		if timelogLocked(msg.log) {
			return m, m.status.show("this entry is locked or approved in Wrike and cannot be changed", true)
		}
		prompt := fmt.Sprintf("Delete %.1f h on %s?", msg.log.Hours, msg.log.TrackedDate)
		m.openDialog(confirmDialog{prompt: prompt, onYes: deleteTimelogMsg{id: msg.log.ID}})
		return m, nil
	case pickEntryMsg:
		m.openDialog(newEntryPicker(msg.logs, msg.forDelete))
		return m, nil
	case retryIssueMsg:
		st := m.opts.Store
		return m, m.enqueueIssueOp(func(ctx context.Context) error { return st.Outbox().Retry(ctx, msg.id) }, "Retrying")
	case discardIssueMsg:
		st := m.opts.Store
		return m, m.enqueueIssueOp(func(ctx context.Context) error { return st.Outbox().Discard(ctx, msg.id) }, "Discarded")
	case openTaskMsg:
		// selectedTaskID is set here, not left for the pendingSelect round trip through tasksLoadedMsg:
		// taskLoadedMsg drops any answer for a task nobody is on yet, and nothing else is on this one.
		// The rest mirrors openFromSearch: the parent folder becomes the selected node so a later
		// list reload finds this task again, and pendingSelect is only set where a load is issued.
		m.screen, m.focus, m.selectedTaskID = screenMain, paneDetail, msg.id
		cmds := []tea.Cmd{m.loadTask(msg.id)}
		if msg.parentID != "" && m.sidebar.selectByID(msg.parentID) {
			n, _ := m.sidebar.current()
			m.selectedNode = n
			m.pendingSelect = msg.id
			cmds = append(cmds, m.loadTasks(n, m.sidebar.crumb(n)))
		}
		return m, tea.Batch(cmds...)
	case writeQueuedMsg:
		m.status.pending, m.status.failed = msg.pending, msg.failed
		cmds := []tea.Cmd{m.status.show(msg.toast, false), m.reloadCurrent()}
		if m.screen == screenIssues {
			cmds = append(cmds, m.loadIssues())
		}
		if m.screen == screenTimesheet {
			cmds = append(cmds, m.loadWeek(m.timesheet.weekStart))
		}
		return m, tea.Batch(cmds...)
	case editorDoneMsg:
		// The temp file is a small OS side effect local to this handler,
		// the comment itself still goes through submitCommentMsg so it reaches the outbox by the same path as the dialog.
		// A file that cannot be read is treated as an empty comment, there is nothing better to send.
		text, _ := os.ReadFile(msg.path)
		_ = os.Remove(msg.path)
		if msg.err != nil {
			return m, m.status.show("editor failed: "+msg.err.Error(), true)
		}
		trimmed := strings.TrimSpace(string(text))
		if trimmed == "" {
			return m, m.status.show("empty comment, nothing sent", false)
		}
		return m, intent(submitCommentMsg{taskID: msg.taskID, text: trimmed})
	case submitCommentMsg:
		st, meID := m.opts.Store, m.ref.meID
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueComment(ctx, msg.taskID, meID, msg.text)
			return err
		}, "Comment queued")
	case submitStatusMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{CustomStatusID: msg.statusID, Status: msg.group})
			return err
		}, "Status set to "+msg.name)
	case submitAssigneesMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{AddResponsibles: msg.add, RemoveResponsibles: msg.remove})
			return err
		}, "Assignees updated")
	case submitDatesMsg:
		st := m.opts.Store
		dates := msg.dates
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{Dates: &dates})
			return err
		}, "Dates updated")
	case submitTitleMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{Title: msg.title})
			return err
		}, "Title updated")
	case submitImportanceMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{Importance: msg.importance})
			return err
		}, "Importance set to "+msg.importance)
	case submitFoldersMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{AddParents: msg.add, RemoveParents: msg.remove})
			return err
		}, foldersToast(msg.add, msg.remove, m.list.folders.title))
	case submitTimelogMsg:
		st, meID := m.opts.Store, m.ref.meID
		if msg.timelogID == "" {
			if m.screen == screenTimesheet {
				// A new task's row appears with the reload that follows, the cursor goes with it.
				m.timesheet.focusTask = msg.taskID
			}
			return m, m.enqueue(func(ctx context.Context) error {
				_, err := st.Outbox().EnqueueTimelogCreate(ctx, msg.taskID, meID, store.TimelogCreatePayload{Hours: msg.hours, TrackedDate: msg.date, Comment: msg.comment})
				return err
			}, fmt.Sprintf("Logged %.1f h", msg.hours))
		}
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTimelogUpdate(ctx, msg.timelogID, store.TimelogUpdatePayload{Hours: msg.hours, TrackedDate: msg.date, Comment: msg.comment})
			return err
		}, "Time entry updated")
	case deleteTimelogMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTimelogDelete(ctx, msg.id)
			return err
		}, "Time entry deleted")
	}
	if m.screen == screenFirstRun {
		var cmd tea.Cmd
		m.firstRun, cmd = m.firstRun.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) onSyncState(state string) (tea.Model, tea.Cmd) {
	m.status.state = state
	switch state {
	case "idle":
		m.status.lastSynced = m.opts.Now()
	case "offline", "failed":
		if m.status.since.IsZero() {
			m.status.since = m.opts.Now()
		}
	case "auth_required":
		if m.screen != screenFirstRun {
			m.screen = screenFirstRun
			m.firstRun = newFirstRun(stepToken, "Wrike did not accept the stored token. Paste it again to continue.", m.keys)
			m.firstRun.reauth = true
		}
	}
	if state != "offline" && state != "failed" {
		m.status.since = time.Time{}
	}
	return m, nil
}

// reload re-reads what is on screen for the caches that changed.
func (m Model) reload(entities []string) tea.Cmd {
	var cmds []tea.Cmd
	if slices.Contains(entities, "contacts") || slices.Contains(entities, "workflows") {
		cmds = append(cmds, m.loadRef())
	}
	if slices.Contains(entities, "folders") || slices.Contains(entities, "spaces") || slices.Contains(entities, "tasks") {
		cmds = append(cmds, m.loadTree())
	}
	// The first sync writes tasks before the tree is on screen, and the zero node has no folder to list.
	if slices.Contains(entities, "tasks") && m.selectedNode.kind != nodeNone {
		cmds = append(cmds, m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode)))
	}
	if slices.Contains(entities, "tasks") || slices.Contains(entities, "comments") || slices.Contains(entities, "timelogs") {
		cmds = append(cmds, m.reloadTask())
	}
	if slices.Contains(entities, "timelogs") && m.screen == screenTimesheet {
		cmds = append(cmds, m.loadWeek(m.timesheet.weekStart))
	}
	if m.screen == screenFirstRun {
		if m.firstRun.step == stepScopes && (slices.Contains(entities, "spaces") || slices.Contains(entities, "folders")) {
			cmds = append(cmds, m.loadPicker())
		}
		if m.firstRun.step == stepSyncing {
			cmds = append(cmds, m.loadScopes())
		}
	}
	return tea.Batch(cmds...)
}

// openedOnFocus marks the selected task opened when focus has just arrived at the detail pane, and does nothing otherwise.
func (m Model) openedOnFocus(prev pane) tea.Cmd {
	if m.focus != paneDetail || prev == paneDetail || m.selectedTaskID == "" {
		return nil
	}
	return m.markOpened(m.selectedTaskID)
}

// openFromSearch closes the search overlay and jumps straight to the chosen task.
// selectedTaskID is set here rather than waiting for the sidebar/list round trip, so openedOnFocus
// below marks the right task and a task whose folder is outside the tree still reaches the detail pane.
func (m Model) openFromSearch(t store.Task) (tea.Model, tea.Cmd) {
	prevFocus := m.focus
	m.screen = screenMain
	m.overlay = overlayNone
	m.search.blur()
	m.focus = paneDetail
	m.selectedTaskID = t.ID
	var cmds []tea.Cmd
	if len(t.ParentIDs) > 0 && m.sidebar.selectByID(t.ParentIDs[0]) {
		n, _ := m.sidebar.current()
		// selectedNode has to follow the jump, or a later reload keyed off it (an outbox write, a store change)
		// reloads the node the search left behind instead of the one now on screen.
		m.selectedNode = n
		// pendingSelect is read by the next tasksLoadedMsg, so it is set only where a load is actually issued.
		m.pendingSelect = t.ID
		cmds = append(cmds, m.loadTasks(n, m.sidebar.crumb(n)))
	}
	cmds = append(cmds, m.loadTask(t.ID), m.openedOnFocus(prevFocus))
	return m, tea.Batch(cmds...)
}

// reloadTask re-reads the task the detail pane is showing.
// Nothing is open before the first selection, hence the guard.
func (m Model) reloadTask() tea.Cmd {
	if m.selectedTaskID == "" {
		return nil
	}
	return m.loadTask(m.selectedTaskID)
}

// openDialog opens a dialog and switches the overlay to it.
// The pointer receiver mutates the caller's m in place, and the caller returns that same m afterward,
// so a caller must not also read m in the same statement, the order between the two is unspecified.
func (m *Model) openDialog(d dialog) {
	m.dialog = d
	m.overlay = overlayDialog
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.overlay == overlayDialog && m.dialog != nil {
		if msg.Type == tea.KeyEsc {
			m.overlay, m.dialog = overlayNone, nil
			return m, nil
		}
		var cmd tea.Cmd
		m.dialog, cmd = m.dialog.Update(msg)
		return m, cmd
	}
	if m.overlay == overlayHelp {
		if key.Matches(msg, m.keys.Help, m.keys.Back, m.keys.Quit) {
			m.overlay = overlayNone
		}
		return m, nil
	}
	if m.overlay == overlaySearch {
		if key.Matches(msg, m.keys.Back) {
			m.overlay = overlayNone
			m.search.blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}
	if m.screen == screenFirstRun {
		// A token may contain a q, so only the step with the input swallows it.
		if m.firstRun.step != stepToken && key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.firstRun, cmd = m.firstRun.Update(msg)
		return m, cmd
	}
	if m.list.filtering {
		// A filter query may contain any letter, including the ones bound to quit or the pane switches.
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		if m.shape == shapeBoard {
			// Every keystroke rebuilds the rows, and the board's window and offset describe the rows it saw last.
			m.board.fit(&m.list, m.theme.HidePrefixes)
		}
		return m, cmd
	}
	if m.screen == screenIssues || m.screen == screenTimesheet {
		// Only the keys that make sense with no task on screen fall through:
		// everything else would otherwise reach whatever task the main screen had last selected,
		// underneath the box this screen is showing instead, the task action keys below included.
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.overlay = overlayHelp
			return m, nil
		case key.Matches(msg, m.keys.Search):
			m.overlay = overlaySearch
			return m, m.search.reset()
		case key.Matches(msg, m.keys.Refresh):
			return m.refresh()
		case key.Matches(msg, m.keys.Issues) && m.screen != screenIssues:
			m.screen = screenIssues
			return m, m.loadIssues()
		case key.Matches(msg, m.keys.Timesheet) && m.screen != screenTimesheet:
			m.screen = screenTimesheet
			return m, m.loadWeek(time.Time{})
		case key.Matches(msg, m.keys.Back):
			m.screen = screenMain
			return m, nil
		}
		var cmd tea.Cmd
		if m.screen == screenIssues {
			m.issues, cmd = m.issues.Update(msg)
		} else {
			m.timesheet, cmd = m.timesheet.Update(msg)
		}
		return m, cmd
	}
	prevFocus := m.focus
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.overlay = overlayHelp
	case key.Matches(msg, m.keys.Search):
		m.overlay = overlaySearch
		return m, m.search.reset()
	case key.Matches(msg, m.keys.Refresh):
		return m.refresh()
	case key.Matches(msg, m.keys.Issues):
		m.screen = screenIssues
		return m, m.loadIssues()
	case key.Matches(msg, m.keys.Timesheet):
		m.screen = screenTimesheet
		return m, m.loadWeek(time.Time{})
	case key.Matches(msg, m.keys.NextPane):
		m.focus = m.cyclePane(1)
	case key.Matches(msg, m.keys.PrevPane):
		m.focus = m.cyclePane(-1)
	case key.Matches(msg, m.keys.Back):
		if m.screen != screenMain {
			m.screen = screenMain
		} else if m.shape == shapeBoard {
			// The board is the home pane of its shape: a side pane hands the width back to it, on the board itself esc rests.
			// The one pane rule below counts panes down and would land on the detail, which sits before the board in the order.
			m.focus = paneBoard
		} else if visibleCount(m.width) == 1 && m.focus > paneSidebar {
			m.focus--
		}
	case key.Matches(msg, m.keys.Open):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			open := m.opts.Hooks.OpenURL
			if open == nil {
				return m.status.show("browser not available", true)
			}
			if t.Permalink == "" {
				return m.status.show(noPermalink, true)
			}
			// Starting a browser can block, so it runs as a command and reports back instead of stalling the key handler.
			return func() tea.Msg {
				if err := open(t.Permalink); err != nil {
					return toastMsg{text: "could not open browser: " + err.Error(), isErr: true}
				}
				return toastMsg{text: "Opened in browser"}
			}
		})
		return m, cmd
	case key.Matches(msg, m.keys.CopyLink):
		cmd := m.copy(func(t store.Task) (string, string) { return t.Permalink, "Copied permalink" })
		return m, cmd
	case key.Matches(msg, m.keys.CopyBranch):
		cmd := m.copy(func(t store.Task) (string, string) {
			name := branchName(m.opts.Config.BranchTemplate, t, m.opts.Config.HidePrefixes)
			return name, "Copied " + name
		})
		return m, cmd
	case key.Matches(msg, m.keys.CopyID):
		cmd := m.copy(func(t store.Task) (string, string) { return t.ID, "Copied task id" })
		return m, cmd
	case key.Matches(msg, m.keys.Comment):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newCommentDialog(t.ID, t.Title, min(m.width-4, 80))
			m.openDialog(d)
			return cmd
		})
		return m, cmd
	case key.Matches(msg, m.keys.CommentEditor):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			cmd, err := openEditor(t.ID)
			if err != nil {
				return m.status.show(err.Error(), true)
			}
			return cmd
		})
		return m, cmd
	case key.Matches(msg, m.keys.Status):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			m.openDialog(newStatusDialog(t, m.ref, m.keys))
			return nil
		})
		return m, cmd
	case key.Matches(msg, m.keys.Board):
		m.toggleShape()
	case key.Matches(msg, m.keys.GroupBy):
		m.list.cycleGroup(m.shape == shapeBoard)
	case key.Matches(msg, m.keys.StatusPrev), key.Matches(msg, m.keys.StatusNext):
		delta := 1
		if key.Matches(msg, m.keys.StatusPrev) {
			delta = -1
		}
		cmd := m.withTask(func(t store.Task) tea.Cmd { return m.moveStatus(t, delta) })
		return m, cmd
	case key.Matches(msg, m.keys.LogTime):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newTimelogDialog(t.ID, t.Title, nil, "", m.opts.Now())
			m.openDialog(d)
			return cmd
		})
		return m, cmd
	case key.Matches(msg, m.keys.Assignee):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newAssigneeDialog(t, m.ref)
			m.openDialog(d)
			return cmd
		})
		return m, cmd
	case key.Matches(msg, m.keys.Dates):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newDatesDialog(t, m.opts.Now())
			m.openDialog(d)
			return cmd
		})
		return m, cmd
	case key.Matches(msg, m.keys.EditTitle):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newTitleDialog(t, min(m.width-4, 80))
			m.openDialog(d)
			return cmd
		})
		return m, cmd
	case key.Matches(msg, m.keys.Importance):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			m.openDialog(newImportanceDialog(t, m.keys))
			return nil
		})
		return m, cmd
	case key.Matches(msg, m.keys.Folders):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newFoldersDialog(t, m.sidebar.nodes, m.list.folders, m.list.nodeID)
			m.openDialog(d)
			return cmd
		})
		return m, cmd
	}
	opened := m.openedOnFocus(prevFocus)
	// The sizes are computed after the child handled the key, not before:
	// expanding a sidebar node widens the sidebar, and the detail would otherwise be laid out for the previous width.
	switch m.focus {
	case paneSidebar:
		var cmd tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(msg)
		m.syncPaneSizes()
		return m, tea.Batch(opened, cmd)
	case paneList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		m.syncPaneSizes()
		return m, tea.Batch(opened, cmd)
	case paneDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		m.syncPaneSizes()
		return m, tea.Batch(opened, cmd)
	case paneBoard:
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(msg, &m.list, m.theme.HidePrefixes)
		m.syncPaneSizes()
		return m, tea.Batch(opened, cmd)
	}
	m.syncPaneSizes()
	return m, opened
}

// cyclePane moves focus by delta over the panes of the current shape.
func (m Model) cyclePane(delta int) pane {
	panes := m.panes()
	for i, p := range panes {
		if p == m.focus {
			return panes[(i+delta+len(panes))%len(panes)]
		}
	}
	return panes[0]
}

// toggleShape switches the list and the board. The selection is the list's cursor either way, so nothing is carried over.
// The board has no by status grouping, its columns already are the status, so that grouping drops to none.
func (m *Model) toggleShape() {
	if m.shape == shapeBoard {
		m.shape = shapeList
		if m.focus == paneBoard {
			m.focus = paneList
		}
	} else {
		m.shape = shapeBoard
		if m.focus == paneList {
			m.focus = paneBoard
		}
		if m.list.groupBy == groupStatus {
			m.list.setGroup(groupNone)
		}
	}
	m.syncPaneSizes()
}

// moveStatus steps the task to the neighbouring status of its workflow, through the message the status dialog sends,
// so the toast, the cache update and the outbox row are the ones that exist.
func (m *Model) moveStatus(t store.Task, delta int) tea.Cmd {
	if m.shape == shapeBoard && m.list.inBucket(t.ID) {
		return m.status.show("this task is on another workflow, s picks a status", true)
	}
	cs, known, ok := stepStatus(t, m.ref, delta)
	if !known {
		return m.status.show(noWorkflowKnown, true)
	}
	if !ok {
		if delta > 0 {
			return m.status.show("already at the last status", false)
		}
		return m.status.show("already at the first status", false)
	}
	return intent(submitStatusMsg{taskID: t.ID, statusID: cs.ID, name: cs.Name, group: cs.Group})
}

// refresh asks the sync engine for a cycle right away, the demo and the tests run without an engine.
func (m Model) refresh() (Model, tea.Cmd) {
	if m.opts.Hooks.Refresh == nil {
		return m, m.status.show("refresh is not available", true)
	}
	m.opts.Hooks.Refresh()
	return m, m.status.show("refreshing", false)
}

// selectedTask is the task the open/copy bindings act on:
// the detail's task when the detail pane has focus and finished loading, otherwise the list's current row.
func (m Model) selectedTask() (store.Task, bool) {
	if m.focus == paneDetail && m.detail.loaded {
		return m.detail.task, true
	}
	if row, ok := m.list.current(); ok {
		return row.task, true
	}
	return store.Task{}, false
}

// withTask runs f on the selected task, or reports there is none.
// status.show mutates the status model, so this takes a pointer receiver.
// handleKey has a value receiver, so it calls this on its own local copy of m and returns that copy.
func (m *Model) withTask(f func(store.Task) tea.Cmd) tea.Cmd {
	t, ok := m.selectedTask()
	if !ok {
		return m.status.show("no task selected", true)
	}
	return f(t)
}

const noPermalink = "this task has no permalink yet"

func (m *Model) copy(pick func(store.Task) (text, toast string)) tea.Cmd {
	return m.withTask(func(t store.Task) tea.Cmd {
		cp := m.opts.Hooks.Copy
		if cp == nil {
			return m.status.show("clipboard not available", true)
		}
		text, toast := pick(t)
		if text == "" {
			// The permalink is the only one of the three that can be missing, the id and the branch name are always there.
			return m.status.show(noPermalink, true)
		}
		// The hook writes to the terminal and runs a clipboard tool, so it goes into a command rather than into the update loop.
		return func() tea.Msg {
			if err := cp(text); err != nil {
				// The error says what did and did not reach the clipboard, so it is shown as it is.
				return toastMsg{text: err.Error(), isErr: true}
			}
			return toastMsg{text: toast}
		}
	})
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	bodyHeight := m.height - 1
	var body string
	switch m.screen {
	case screenFirstRun:
		body = m.firstRun.View(m.theme, m.width, bodyHeight)
	case screenIssues:
		body = m.viewIssues(bodyHeight)
	case screenTimesheet:
		body = m.viewTimesheet(bodyHeight)
	default:
		body = m.viewMain()
	}
	hints := ""
	if m.height >= 20 {
		available := m.width - m.status.leftWidth(m.theme, m.opts.Now()) - 3
		hints = fitHints(m.help, m.hintBindings(), available)
	}
	out := body + "\n" + m.status.View(m.theme, m.width, hints, m.opts.Now())
	if m.overlay == overlayHelp {
		out = centered(out, helpView(m.theme, m.help, m.helpGroups(), m.width, "wrikery "+m.opts.Version), m.width, m.height)
	}
	if m.overlay == overlaySearch {
		out = centered(out, m.search.View(m.theme, m.ref, min(m.width-4, 80), searchMaxRows(m.height)), m.width, m.height)
	}
	if m.overlay == overlayDialog && m.dialog != nil {
		out = centered(out, m.dialog.View(m.theme, min(m.width-4, 80), m.height), m.width, m.height)
	}
	return out
}

// syncPaneSizes pushes the computed pane heights down to the child models.
// A View method takes its size as a plain argument each frame and has no way to remember it between calls on its own.
func (m *Model) syncPaneSizes() {
	lay := m.layout()
	if r, ok := lay.rects[paneSidebar]; ok {
		m.sidebar.height = r.h - 2
	}
	if r, ok := lay.rects[paneList]; ok {
		m.list.height = r.h - 2
	}
	if r, ok := lay.rects[paneBoard]; ok {
		m.board.width, m.board.height = r.w-2, r.h-2
		m.board.fit(&m.list, m.theme.HidePrefixes)
	}
	if r, ok := lay.rects[paneDetail]; ok {
		m.detail.layout(m.theme, m.ref, m.opts.Now(), r.w-2, r.h-2, m.opts.Config.Theme)
	}
	// The issues screen replaces the whole body with one box, its inner height mirrors what viewIssues gives its View.
	m.issues.height = max(0, m.height-3)
	// Same box, same formula: the timesheet screen also replaces the whole body with one box.
	m.timesheet.height = max(0, m.height-3)
}

// viewIssues fills the whole body with one box, there is no sidebar or detail pane to share it with.
func (m Model) viewIssues(height int) string {
	title := fmt.Sprintf("Sync issues (%d)", len(m.issues.rows))
	body := m.issues.View(m.theme, m.opts.Now(), m.width-2, height-2)
	return m.theme.box(title, body, m.width, height, true)
}

// viewTimesheet fills the whole body with one box, the same way viewIssues does.
// Before the first weekLoadedMsg lands the box is titled plainly, with nothing in it yet.
func (m Model) viewTimesheet(height int) string {
	title := "Timesheet"
	if m.timesheet.loaded {
		title = m.timesheet.title()
	}
	body := ""
	if m.timesheet.loaded {
		body = m.timesheet.View(m.theme, m.width-2, height-2)
	}
	return m.theme.box(title, body, m.width, height, true)
}

func (m Model) viewMain() string {
	lay := m.layout()
	parts := make([]string, 0, len(lay.visible))
	for _, p := range lay.visible {
		r := lay.rects[p]
		parts = append(parts, m.theme.box(m.paneTitle(p), m.paneBody(p, r), r.w, r.h, p == m.focus))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m Model) paneTitle(p pane) string {
	switch p {
	case paneSidebar:
		return "Spaces"
	case paneList:
		return m.list.title()
	case paneBoard:
		return m.list.titled("Board")
	}
	return m.detail.title()
}

func (m Model) paneBody(p pane, r rect) string {
	switch p {
	case paneSidebar:
		return m.sidebar.View(m.theme, r.w-2, r.h-2, m.focus == paneSidebar)
	case paneList:
		return m.list.View(m.theme, m.ref, m.opts.Now(), r.w-2, r.h-2, m.focus == paneList)
	case paneBoard:
		return m.board.View(m.theme, m.ref, m.opts.Now(), &m.list, m.focus == paneBoard)
	}
	// The detail pane laid itself out in Update, View only reads the viewport.
	return m.detail.View()
}

// Only the keys that already do something, the overlay lists the whole map.
func (m Model) hintBindings() []key.Binding {
	if m.screen == screenIssues {
		return []key.Binding{m.keys.Retry, m.keys.Discard, m.keys.Enter, m.keys.Back}
	}
	if m.screen == screenTimesheet {
		return []key.Binding{
			m.keys.DayLeft, m.keys.DayRight, m.keys.Down, m.keys.Up, m.keys.WeekPrev, m.keys.WeekNext,
			m.keys.ThisWeek, m.keys.Add, m.keys.Edit, m.keys.Delete, m.keys.Back,
		}
	}
	base := []key.Binding{m.keys.NextPane, m.keys.Search, m.keys.Help, m.keys.Quit}
	if m.focus == paneBoard {
		return append([]key.Binding{m.keys.ColPrev, m.keys.ColNext, m.keys.StatusNext, m.keys.Enter, m.keys.Board, m.keys.GroupBy}, base...)
	}
	if m.focus == paneList {
		return append([]key.Binding{m.keys.Enter, m.keys.Filter, m.keys.GroupBy, m.keys.Board, m.keys.ToggleDone}, base...)
	}
	if m.focus == paneDetail {
		return append([]key.Binding{m.keys.Up, m.keys.Down, m.keys.Left}, base...)
	}
	return base
}

func (m Model) helpGroups() [][]key.Binding {
	if m.screen == screenIssues {
		return [][]key.Binding{m.keys.global(), m.keys.issues()}
	}
	if m.screen == screenTimesheet {
		return [][]key.Binding{m.keys.global(), m.keys.timesheet()}
	}
	if m.shape == shapeBoard {
		return [][]key.Binding{m.keys.global(), m.keys.board(), m.keys.task()}
	}
	return [][]key.Binding{m.keys.global(), m.keys.list(), m.keys.task()}
}
