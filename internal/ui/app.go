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
	Version string
	Store   *store.Store
	Hooks   Hooks
	Config  config.Config
	// ConfigFile is shown on the settings screen, the app reads it and never writes it.
	ConfigFile   string
	ThemeSetting string // ui.theme as the config file has it, Config.UI.Theme holds what auto resolved to
	Demo         bool
	FirstRun     bool
	Now          func() time.Time
}

type screen int

const (
	screenMain screen = iota
	screenTimesheet
	screenIssues
	screenSettings
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
	settings  settingsModel
	dialog    dialog

	selectedNode   treeNode
	selectedTaskID string
	pendingDate    string // date a new timesheet entry is for, set while search stands in as its task picker
}

func New(o Options) Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	m := Model{opts: o, theme: NewTheme(o.Config.UI), keys: defaultKeyMap(), help: help.New(), focus: paneList}
	m.sidebar = newSidebar(m.keys, m.theme.HidePrefixes)
	m.list = newTaskList(m.keys)
	m.detail.keys = m.keys
	m.search = newSearch(m.keys)
	m.issues.keys = m.keys
	m.timesheet.keys = m.keys
	m.settings = newSettings(o.Config, o.ThemeSetting, o.ConfigFile, m.keys)
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
		return m, tea.Batch(m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode), m.swapWatch(), false), m.reloadTask())
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
		cmds := []tea.Cmd{m.loadTree()}
		if m.screen == screenTimesheet {
			// The row glyphs were resolved against the reference data of their load, the week is read again with this one.
			cmds = append(cmds, m.loadWeek(m.timesheet.weekStart))
		}
		return m, tea.Batch(cmds...)
	case treeLoadedMsg:
		m.sidebar.setTree(msg.nodes, msg.pins)
		m.list.folders = newFolderIndex(msg.nodes)
		m.list.regroup()
		m.syncPaneSizes()
		if n, ok := m.sidebar.current(); ok {
			return m, intent(nodeSelectedMsg{node: n})
		}
		return m, nil
	case focusMsg:
		if m.screen == screenTimesheet {
			// The detail pane names the list on h, beside the grid that means the grid.
			m.timesheet.detailFocus = false
			return m, nil
		}
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
		return m, m.loadTasks(msg.node, m.sidebar.crumb(msg.node), "", false)
	case tasksLoadedMsg:
		// Loads run in the background, so an answer for a node the sidebar has left since is dropped,
		// or two quick moves could leave the list showing the folder passed on the way.
		if msg.nodeID != m.selectedNode.id {
			return m, nil
		}
		if m.list.nodeID != msg.nodeID && msg.selectID == "" {
			m.list.cursor = 0
		}
		found := m.list.setRows(msg.nodeID, msg.crumb, msg.tasks, msg.states, msg.selectID, msg.lift)
		if !found && m.shape == shapeBoard {
			// The selected card left the board, a status move onto a hidden status does that, so the cursor stays in its cell.
			m.list.cursor = m.board.fallback(&m.list)
		}
		if m.shape == shapeBoard {
			// In the list shape the board has no width yet, and a window placed at width zero would stick to the cursor column.
			m.board.fit(&m.list, m.theme.HidePrefixes)
		}
		// The list reloads behind the timesheet after every drain and every sync, its row must not replace the pane's task.
		if m.paneBesideGrid() {
			return m, nil
		}
		if cur, ok := m.list.current(); ok && cur.task.ID != m.selectedTaskID {
			return m, intent(taskSelectedMsg{id: cur.task.ID})
		}
		m.clearIfListEmpty()
		return m, nil
	case taskSelectedMsg:
		// The intent travels through the queue, and a list load that lands before it can leave the list without that row.
		// Such a late selection is dropped, or an empty folder would show a task from the folder before.
		if !m.list.has(msg.id) || m.paneBesideGrid() {
			return m, nil
		}
		m.selectedTaskID = msg.id
		return m, m.loadTask(msg.id)
	case taskLoadedMsg:
		// The cursor is free to move while the read runs, so an answer for a task nobody is on any more is dropped.
		if msg.asked != m.selectedTaskID {
			return m, nil
		}
		// The read may have followed a swap, the selection follows it too.
		m.selectedTaskID = msg.task.ID
		m.detail.set(msg)
		m.syncPaneSizes()
		return m, nil
	case taskGoneMsg:
		if msg.id != m.selectedTaskID {
			return m, nil
		}
		// Without this the detail and the action keys would stay on the task from before.
		m.selectedTaskID, m.detail = "", taskDetailModel{keys: m.keys}
		m.syncPaneSizes()
		return m, m.status.show("that task is no longer in the cache", true)
	case firstRunSubmitTokenMsg:
		return m, m.verifyToken(msg.token)
	case firstRunConfirmScopesMsg:
		return m, tea.Batch(m.saveScopes(msg.scopes, ""), m.firstRun.spinner.Tick)
	case scopesLoadedMsg:
		m.settings.setScopes(msg.scopes)
		if m.screen == screenFirstRun {
			var cmd tea.Cmd
			m.firstRun, cmd = m.firstRun.Update(msg)
			return m, cmd
		}
		return m, nil
	case openScopesMsg:
		return m, m.loadPicker()
	case pickerLoadedMsg:
		if m.screen == screenSettings {
			if d, ok := m.dialog.(scopesDialog); ok && m.overlay == overlayDialog {
				// The spaces landed while the box was open, see reload.
				next, cmd := d.reload(msg)
				m.dialog = next
				return m, cmd
			}
			d, cmd := newScopesDialog(pickerItems(msg, followedSet(msg.followed)), msg.followed)
			m.openDialog(d)
			return m, cmd
		}
		if m.screen == screenFirstRun {
			var cmd tea.Cmd
			m.firstRun, cmd = m.firstRun.Update(msg)
			return m, cmd
		}
		return m, nil
	case submitScopesMsg:
		return m, m.saveScopes(msg.scopes, msg.toast)
	case scopesSavedMsg:
		m.settings.setScopes(msg.scopes)
		if m.screen == screenFirstRun {
			var cmd tea.Cmd
			m.firstRun, cmd = m.firstRun.Update(scopesLoadedMsg{scopes: msg.scopes})
			return m, cmd
		}
		return m, tea.Batch(m.status.show(msg.toast, false), m.loadTree())
	case hostLoadedMsg:
		m.settings.host = msg.host
		return m, nil
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
	case openTaskMsg:
		return m.jumpToTask(msg.id, msg.parentID)
	case openBesideMsg:
		// The intent travels through the queue, a key typed right after enter can have left the timesheet already.
		if m.screen != screenTimesheet {
			return m, nil
		}
		if m.timesheet.detailOpen && msg.id == m.selectedTaskID && m.detail.loaded {
			// The pane followed the row here already, enter only hands it the keys.
			m.timesheet.detailFocus = true
			return m, m.markOpened(msg.id)
		}
		// The pane starts empty rather than show the main screen's task until the read lands.
		m.timesheet.detailOpen, m.timesheet.detailFocus = true, true
		m.selectedTaskID, m.detail = msg.id, taskDetailModel{keys: m.keys}
		m.syncPaneSizes()
		// A deliberate open, like enter on the main screen, so the thread of the task gets refreshed.
		return m, tea.Batch(m.loadTask(msg.id), m.markOpened(msg.id))
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
		if m.timesheet.detailFocus {
			// The keys are in the pane, it keeps its task even when the reload dropped its row and the cursor moved.
			return m, nil
		}
		follow := m.followTimesheetRow()
		return m, follow
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
		prompt := fmt.Sprintf("Delete %s on %s?", hoursText(msg.log.Hours), msg.log.TrackedDate)
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
	case writeQueuedMsg:
		m.status.pending, m.status.failed = msg.pending, msg.failed
		cmds := []tea.Cmd{m.status.show(msg.toast, false), m.reloadCurrent(msg.selectID)}
		if m.screen == screenIssues {
			cmds = append(cmds, m.loadIssues())
		}
		if m.screen == screenTimesheet {
			cmds = append(cmds, m.loadWeek(m.timesheet.weekStart))
		}
		return m, tea.Batch(cmds...)
	case editorDoneMsg:
		// The temp file is a small OS side effect local to this handler,
		// the text itself still goes through a submit message so it reaches the outbox by the same path as a dialog.
		// A file that cannot be read is treated as empty, there is nothing better to send.
		text, _ := os.ReadFile(msg.path)
		_ = os.Remove(msg.path)
		if msg.err != nil {
			return m, m.status.show("editor failed: "+msg.err.Error(), true)
		}
		if msg.edit != nil {
			return m, m.finishDescriptionEdit(msg.taskID, msg.edit, string(text))
		}
		trimmed := strings.TrimSpace(string(text))
		if trimmed == "" {
			return m, m.status.show("empty comment, nothing sent", false)
		}
		return m, intent(submitCommentMsg{taskID: msg.taskID, text: trimmed})
	case descriptionReadyMsg:
		cmd, err := openEditor(msg.task.ID, &descriptionEdit{html: msg.task.Description, text: editorText(msg.task.Description)})
		if err != nil {
			return m, m.status.show(err.Error(), true)
		}
		return m, cmd
	case submitDescriptionMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{Description: msg.html})
			return err
		}, "Description updated")
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
	case submitCreateMsg:
		st, meID := m.opts.Store, m.ref.meID
		p := store.TaskCreatePayload{Title: msg.title}
		// The reference data may not have loaded yet, then there is nobody to assign.
		if msg.assignMe && meID != "" {
			p.Responsibles = []string{meID}
		}
		toast := "Task added"
		if msg.where != "" {
			toast += " to " + msg.where
		}
		return m, m.enqueueSelecting(func(ctx context.Context) (string, error) {
			id, err := st.Outbox().EnqueueTaskCreate(ctx, msg.folderID, p)
			if err != nil {
				return "", err
			}
			return store.LocalID(id), nil
		}, toast)
	case submitTitleMsg:
		st := m.opts.Store
		return m, m.enqueue(func(ctx context.Context) error {
			_, err := st.Outbox().EnqueueTaskUpdate(ctx, msg.taskID, store.TaskUpdatePayload{Title: msg.title})
			return err
		}, "Title updated")
	case submitFilterMsg:
		return m, m.setNarrow(msg.filter)
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
			}, "Logged "+hoursText(msg.hours))
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
	var cmd tea.Cmd
	switch state {
	case "idle":
		m.status.lastSynced = m.opts.Now()
	case "offline", "failed":
		if m.status.since.IsZero() {
			m.status.since = m.opts.Now()
		}
	case "auth_required":
		if m.screen != screenFirstRun {
			// The way back lands on the main screen, which must not find the pane beside the grid still open.
			cmd = m.closeTimesheetDetail()
			m.screen = screenFirstRun
			m.firstRun = newFirstRun(stepToken, "Wrike did not accept the stored token. Paste it again to continue.", m.keys)
			m.firstRun.reauth = true
		}
	}
	if state != "offline" && state != "failed" {
		m.status.since = time.Time{}
	}
	return m, cmd
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
		cmds = append(cmds, m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode), m.swapWatch(), false))
	}
	if slices.Contains(entities, "tasks") || slices.Contains(entities, "comments") || slices.Contains(entities, "timelogs") || slices.Contains(entities, "dependencies") {
		cmds = append(cmds, m.reloadTask())
	}
	// A task change can be a status, and the row glyph is resolved when the week loads.
	if (slices.Contains(entities, "timelogs") || slices.Contains(entities, "tasks")) && m.screen == screenTimesheet {
		cmds = append(cmds, m.loadWeek(m.timesheet.weekStart))
	}
	if _, open := m.dialog.(scopesDialog); open && m.overlay == overlayDialog && (slices.Contains(entities, "spaces") || slices.Contains(entities, "folders")) {
		cmds = append(cmds, m.loadPicker())
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

// updateSidebar hands the key to the sidebar and writes the pin changes it made before the update returns.
// An intent message could land after a quit key, so the writes do not travel as commands.
func (m *Model) updateSidebar(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	m.sidebar, cmd = m.sidebar.Update(msg)
	m.syncPaneSizes()
	return tea.Batch(cmd, m.flushPins())
}

// flushPins writes what the sidebar changed about the pins, in the order it happened.
// The writes are small and local, a sync transaction can hold the writer for a moment and no longer.
func (m *Model) flushPins() tea.Cmd {
	ctx := context.Background()
	for _, c := range m.sidebar.takeChanges() {
		var err error
		if c.id == "" {
			value := "0"
			if c.on {
				value = "1"
			}
			err = m.opts.Store.SetMeta(ctx, store.MetaKeySidebarPinned, value)
		} else {
			err = m.opts.Store.Pins().Set(ctx, c.id, c.on)
		}
		if err != nil {
			slog.Error("ui", "error", err)
			return m.status.show("could not save the pin: "+err.Error(), true)
		}
	}
	return nil
}

// openedOnFocus marks the selected task opened when focus has just arrived at the detail pane, and does nothing otherwise.
func (m Model) openedOnFocus(prev pane) tea.Cmd {
	if m.focus != paneDetail || prev == paneDetail || m.selectedTaskID == "" {
		return nil
	}
	return m.markOpened(m.selectedTaskID)
}

// jumpToTask leaves the search overlay, the sync issues screen or the timesheet and shows the task on the main screen.
// selectedTaskID is set here rather than waiting for the sidebar/list round trip:
// taskLoadedMsg drops an answer for a task nobody is on yet, and openedOnFocus below has to mark the right task.
// A task whose folder is outside the tree reaches the detail pane too, but only until the next list reload,
// which selects the cursor row of the folder left behind.
func (m Model) jumpToTask(id, parentID string) (tea.Model, tea.Cmd) {
	prevFocus := m.focus
	m.screen, m.overlay, m.focus, m.selectedTaskID = screenMain, overlayNone, paneDetail, id
	m.search.blur()
	// A pick from the search overlay leaves the timesheet too, the grid has no pane beside it when T comes back.
	m.timesheet.detailOpen, m.timesheet.detailFocus = false, false
	var cmds []tea.Cmd
	if parentID != "" && m.sidebar.reveal(parentID) {
		n, _ := m.sidebar.current()
		// selectedNode has to follow the jump, or a later reload keyed off it (an outbox write, a store change)
		// reloads the node the jump left behind instead of the one now on screen.
		m.selectedNode = n
		cmds = append(cmds, m.loadTasks(n, m.sidebar.crumb(n), id, true), m.flushPins())
	}
	cmds = append(cmds, m.loadTask(id), m.openedOnFocus(prevFocus))
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

// timesheetKey routes a key on the timesheet screen: to the grid, or to the detail pane open beside it while that has the keys.
// With the grid focused and the pane open, the pane follows the row under the cursor, the way it follows the list.
func (m Model) timesheetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.timesheet.detailOpen && key.Matches(msg, m.keys.NextPane, m.keys.PrevPane) {
		m.timesheet.detailFocus = !m.timesheet.detailFocus
		if m.timesheet.detailFocus {
			return m, nil
		}
		// A reload may have moved the cursor while the pane had the keys, back on the grid the pane is on its row again.
		follow := m.followTimesheetRow()
		return m, follow
	}
	if m.timesheet.detailOpen && m.timesheet.detailFocus {
		if key.Matches(msg, m.keys.Enter) {
			// On to the main screen with the folder selected, the way a pick from the search goes.
			if !m.detail.loaded {
				return m, nil
			}
			return m.jumpToTask(m.detail.task.ID, firstParent(m.detail.task))
		}
		if cmd, ok := m.taskAction(msg); ok {
			return m, cmd
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		m.syncPaneSizes()
		return m, cmd
	}
	var cmd tea.Cmd
	m.timesheet, cmd = m.timesheet.Update(msg)
	follow := m.followTimesheetRow()
	return m, tea.Batch(cmd, follow)
}

// paneBesideGrid tells whether the detail shows the timesheet's task rather than the list's row.
func (m Model) paneBesideGrid() bool {
	return m.screen == screenTimesheet && m.timesheet.detailOpen
}

// followTimesheetRow keeps the pane beside the grid on the row under the cursor.
// The new task row and a row whose task the cache does not hold empty it.
func (m *Model) followTimesheetRow() tea.Cmd {
	if !m.timesheet.detailOpen {
		return nil
	}
	id := m.timesheet.currentTask()
	if id == m.selectedTaskID {
		return nil
	}
	m.selectedTaskID = id
	if id == "" {
		m.detail = taskDetailModel{keys: m.keys}
		m.syncPaneSizes()
		return nil
	}
	return m.loadTask(id)
}

// closeTimesheetDetail closes the pane beside the grid and puts the detail back on the list's own row,
// so coming back to the main screen does not show a task the list is not on.
func (m *Model) closeTimesheetDetail() tea.Cmd {
	if !m.timesheet.detailOpen {
		return nil
	}
	m.timesheet.detailOpen, m.timesheet.detailFocus = false, false
	m.selectedTaskID, m.detail = "", taskDetailModel{keys: m.keys}
	m.syncPaneSizes()
	if row, ok := m.list.current(); ok {
		m.selectedTaskID = row.task.ID
		return m.loadTask(row.task.ID)
	}
	return nil
}

// clearIfListEmpty drops the selected task when the list shows none: an empty folder, a filter nothing matches,
// or the done toggle hiding the last row. Without it the detail pane and the action keys stay on a task from before.
// It runs wherever the rows are rebuilt: after a load, and after a key the list or the board handled.
func (m *Model) clearIfListEmpty() {
	if _, ok := m.list.current(); !ok && m.selectedTaskID != "" {
		m.selectedTaskID, m.detail = "", taskDetailModel{keys: m.keys}
		m.syncPaneSizes()
	}
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
		m.clearIfListEmpty()
		return m, cmd
	}
	if m.sidebar.filtering {
		return m, m.updateSidebar(msg)
	}
	if m.screen == screenIssues || m.screen == screenTimesheet || m.screen == screenSettings {
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
			closing := m.closeTimesheetDetail()
			m.screen = screenIssues
			return m, tea.Batch(closing, m.loadIssues())
		case key.Matches(msg, m.keys.Timesheet) && m.screen != screenTimesheet:
			m.screen = screenTimesheet
			return m, m.loadWeek(time.Time{})
		case key.Matches(msg, m.keys.Settings) && m.screen != screenSettings:
			closing := m.closeTimesheetDetail()
			m.screen = screenSettings
			return m, tea.Batch(closing, m.loadScopes(), m.loadHost())
		case key.Matches(msg, m.keys.Back):
			if m.paneBesideGrid() {
				// One level at a time: the pane beside the grid goes first, the next esc leaves the timesheet.
				closing := m.closeTimesheetDetail()
				return m, closing
			}
			m.screen = screenMain
			return m, nil
		}
		var cmd tea.Cmd
		switch m.screen {
		case screenIssues:
			m.issues, cmd = m.issues.Update(msg)
		case screenTimesheet:
			return m.timesheetKey(msg)
		default:
			m.settings, cmd = m.settings.Update(msg)
		}
		return m, cmd
	}
	if cmd, ok := m.taskAction(msg); ok {
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
	case key.Matches(msg, m.keys.Settings):
		m.screen = screenSettings
		return m, tea.Batch(m.loadScopes(), m.loadHost())
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
			// The board is the home pane of its shape: a side pane hands the width back to it, and on the board itself esc leaves it like b.
			// The one pane rule below counts panes down and would land on the detail, which sits before the board in the order.
			if m.focus == paneBoard {
				// The toggle sizes the panes itself, and the key must not reach the list the toggle just focused.
				m.toggleShape()
				return m, nil
			}
			m.focus = paneBoard
		} else if visibleCount(m.width) == 1 && m.focus > paneSidebar {
			m.focus--
		}
	case key.Matches(msg, m.keys.Board):
		m.toggleShape()
	case key.Matches(msg, m.keys.GroupBy):
		m.list.cycleGroup(m.shape == shapeBoard)
	case key.Matches(msg, m.keys.FilterBox):
		d, cmd := newFilterDialog(&m.list)
		m.openDialog(d)
		return m, cmd
	case key.Matches(msg, m.keys.ClearFilter):
		return m, m.setNarrow(rowFilter{})
	case key.Matches(msg, m.keys.New):
		d, cmd := newCreateDialog(m.sidebar.nodes, m.folderCrumbs(), m.createPreset(),
			m.selectedNode.kind == nodeMe, min(m.width-4, 80))
		m.openDialog(d)
		return m, cmd
	}
	opened := m.openedOnFocus(prevFocus)
	// The sizes are computed after the child handled the key, not before,
	// the scroll clamp in syncPaneSizes wants the rows as the key left them.
	switch m.focus {
	case paneSidebar:
		return m, tea.Batch(opened, m.updateSidebar(msg))
	case paneList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		m.clearIfListEmpty()
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
		m.clearIfListEmpty()
		m.syncPaneSizes()
		return m, tea.Batch(opened, cmd)
	}
	m.syncPaneSizes()
	return m, opened
}

// taskAction runs the keys that act on the selected task: the dialogs, the editor hand offs, the browser and the clipboard.
// The main screen and the detail pane beside the timesheet grid share them. The second result is false when the key is none of them.
func (m *Model) taskAction(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Open):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			open := m.opts.Hooks.OpenURL
			if open == nil {
				return m.status.show("browser not available", true)
			}
			if unconfirmed(t) {
				return m.status.show(notOnWrike, true)
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
		return cmd, true
	case key.Matches(msg, m.keys.CopyLink):
		cmd := m.copy(func(t store.Task) (string, string) { return t.Permalink, "Copied permalink" })
		return cmd, true
	case key.Matches(msg, m.keys.CopyBranch):
		cmd := m.copy(func(t store.Task) (string, string) {
			name := branchName(m.opts.Config.UI.BranchTemplate, t, m.opts.Config.UI.HidePrefixes)
			return name, "Copied " + name
		})
		return cmd, true
	case key.Matches(msg, m.keys.CopyID):
		cmd := m.copy(func(t store.Task) (string, string) { return t.ID, "Copied task id" })
		return cmd, true
	case key.Matches(msg, m.keys.Comment):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newCommentDialog(t.ID, t.Title, min(m.width-4, 80))
			m.openDialog(d)
			return cmd
		})
		return cmd, true
	case key.Matches(msg, m.keys.CommentEditor):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			cmd, err := openEditor(t.ID, nil)
			if err != nil {
				return m.status.show(err.Error(), true)
			}
			return cmd
		})
		return cmd, true
	case key.Matches(msg, m.keys.Status):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			m.openDialog(newStatusDialog(t, m.ref, m.keys))
			return nil
		})
		return cmd, true
	case key.Matches(msg, m.keys.StatusPrev), key.Matches(msg, m.keys.StatusNext):
		delta := 1
		if key.Matches(msg, m.keys.StatusPrev) {
			delta = -1
		}
		cmd := m.withTask(func(t store.Task) tea.Cmd { return m.moveStatus(t, delta) })
		return cmd, true
	case key.Matches(msg, m.keys.LogTime):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			// Beside the grid the entry goes on the day under the cursor, the day n on the cell takes.
			date := ""
			if m.screen == screenTimesheet && m.timesheet.loaded {
				date = m.timesheet.cellDate()
			}
			d, cmd := newTimelogDialog(t.ID, t.Title, nil, date, m.opts.Now())
			m.openDialog(d)
			return cmd
		})
		return cmd, true
	case key.Matches(msg, m.keys.Assignee):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newAssigneeDialog(t, m.ref)
			m.openDialog(d)
			return cmd
		})
		return cmd, true
	case key.Matches(msg, m.keys.Dates):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newDatesDialog(t, m.opts.Now())
			m.openDialog(d)
			return cmd
		})
		return cmd, true
	case key.Matches(msg, m.keys.EditTitle):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			d, cmd := newTitleDialog(t, min(m.width-4, 80))
			m.openDialog(d)
			return cmd
		})
		return cmd, true
	case key.Matches(msg, m.keys.EditDescription):
		// A list row carries no description, so the task is read again before the editor opens.
		cmd := m.withTask(func(t store.Task) tea.Cmd { return m.loadDescription(t.ID) })
		return cmd, true
	case key.Matches(msg, m.keys.Importance):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			m.openDialog(newImportanceDialog(t, m.keys))
			return nil
		})
		return cmd, true
	case key.Matches(msg, m.keys.Folders):
		cmd := m.withTask(func(t store.Task) tea.Cmd {
			// A move leaves the folders under the node in view. The timesheet shows no folder, so a move from there leaves none.
			node := m.list.nodeID
			if m.screen == screenTimesheet {
				node = ""
			}
			d, cmd := newFoldersDialog(t, m.sidebar.nodes, m.list.folders, node)
			m.openDialog(d)
			return cmd
		})
		return cmd, true
	}
	return nil, false
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

// setNarrow applies the filter box's choice, or clears it on F, and keeps the board, the detail and the sizes in step with the rows it leaves.
func (m *Model) setNarrow(f rowFilter) tea.Cmd {
	before, _ := m.list.current()
	m.list.setNarrow(f)
	if m.shape == shapeBoard {
		m.board.fit(&m.list, m.theme.HidePrefixes)
	}
	m.clearIfListEmpty()
	m.syncPaneSizes()
	_, cmd := m.list.afterMove(before, nil)
	return cmd
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
	// The bucket rule is about the board, which only the main screen shows.
	if m.screen == screenMain && m.shape == shapeBoard && m.list.inBucket(t.ID) {
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
	if m.screen == screenTimesheet {
		// The grid has no task of its own, only the pane open beside it.
		if m.timesheet.detailOpen && m.detail.loaded {
			return m.detail.task, true
		}
		return store.Task{}, false
	}
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

const notOnWrike = "not on Wrike yet"

// unconfirmed tells a task whose create is still in the outbox: no permalink and an id Wrike does not know.
func unconfirmed(t store.Task) bool { return store.IsLocalID(t.ID) }

func (m *Model) copy(pick func(store.Task) (text, toast string)) tea.Cmd {
	return m.withTask(func(t store.Task) tea.Cmd {
		if unconfirmed(t) {
			return m.status.show(notOnWrike, true)
		}
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
	case screenSettings:
		body = m.theme.box("Settings", m.settings.View(m.theme, m.width-2, bodyHeight-2), m.width, bodyHeight, true)
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
		m.sidebar.setSize(r.w-2, r.h-2)
	}
	if r, ok := lay.rects[paneList]; ok {
		m.list.height = r.h - 2
		// A taller pane can leave the window past the end of the list, scroll pulls it back.
		m.list.scroll()
	}
	if r, ok := lay.rects[paneBoard]; ok {
		m.board.width, m.board.height = r.w-2, r.h-2
		m.board.fit(&m.list, m.theme.HidePrefixes)
	}
	if r, ok := m.detailRect(lay); ok {
		m.detail.layout(m.theme, m.ref, m.opts.Now(), r.w-2, r.h-2, m.opts.Config.UI.Theme)
	}
	// The issues screen replaces the whole body with one box, its inner height mirrors what viewIssues gives its View.
	m.issues.height = max(0, m.height-3)
	// Same box, same formula: the timesheet screen also replaces the whole body with one box.
	m.timesheet.height = max(0, m.height-3)
}

// detailRect is where the detail pane draws: beside the timesheet grid, or over it, while a task is open there,
// else its place in the main layout.
func (m Model) detailRect(lay layout) (rect, bool) {
	if m.screen == screenTimesheet && m.timesheet.detailOpen {
		w := timesheetDetailWidth(m.width)
		if w == 0 {
			w = m.width
		}
		return rect{w: w, h: m.height - 1}, true
	}
	r, ok := lay.rects[paneDetail]
	return r, ok
}

// viewIssues fills the whole body with one box, there is no sidebar or detail pane to share it with.
func (m Model) viewIssues(height int) string {
	title := fmt.Sprintf("Sync issues (%d)", len(m.issues.rows))
	body := m.issues.View(m.theme, m.opts.Now(), m.width-2, height-2)
	return m.theme.box(title, body, m.width, height, true)
}

// viewTimesheet fills the whole body with the grid's box, the same way viewIssues does,
// or shares it with the detail pane while a task is open beside the grid.
// Before the first weekLoadedMsg lands the box is titled plainly, with nothing in it yet.
func (m Model) viewTimesheet(height int) string {
	dw := 0
	if m.timesheet.detailOpen {
		dw = timesheetDetailWidth(m.width)
		if dw == 0 && m.timesheet.detailFocus {
			// Too narrow for both, the focused one takes the whole body.
			return m.theme.box(m.detail.title(), m.detail.View(), m.width, height, true)
		}
	}
	title := "Timesheet"
	if m.timesheet.loaded {
		title = m.timesheet.title()
	}
	body := ""
	if m.timesheet.loaded {
		body = m.timesheet.View(m.theme, m.width-dw-2, height-2)
	}
	grid := m.theme.box(title, body, m.width-dw, height, !m.timesheet.detailFocus)
	if dw == 0 {
		return grid
	}
	detail := m.theme.box(m.detail.title(), m.detail.View(), dw, height, m.timesheet.detailFocus)
	return lipgloss.JoinHorizontal(lipgloss.Top, grid, detail)
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
		if m.sidebar.pinnedOnly {
			return "Pinned"
		}
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
		if m.timesheet.detailFocus {
			return []key.Binding{m.keys.Up, m.keys.Down, m.keys.Status, m.keys.Comment, m.keys.LogTime, m.keys.Enter, m.keys.NextPane, m.keys.Back}
		}
		keys := []key.Binding{
			m.keys.DayLeft, m.keys.DayRight, m.keys.Down, m.keys.Up, m.keys.WeekPrev, m.keys.WeekNext,
			m.keys.ThisWeek, m.keys.Add, m.keys.Edit, m.keys.Delete, m.keys.Enter,
		}
		if m.timesheet.detailOpen {
			keys = append(keys, m.keys.NextPane)
		}
		return append(keys, m.keys.Back)
	}
	if m.screen == screenSettings {
		return []key.Binding{m.keys.Down, m.keys.Up, m.keys.Enter, m.keys.Back}
	}
	base := []key.Binding{m.keys.NextPane, m.keys.Search, m.keys.Help, m.keys.Quit}
	// The filter keys come last, after the global ones, so a narrower bar drops them before ? and q,
	// and the clear key is offered only while there is something to clear.
	filter := []key.Binding{m.keys.FilterBox}
	if m.list.narrow.active() {
		filter = append(filter, m.keys.ClearFilter)
	}
	if m.focus == paneBoard {
		return slices.Concat([]key.Binding{m.keys.ColPrev, m.keys.ColNext, m.keys.StatusNext, m.keys.Enter, m.keys.Board, m.keys.GroupBy}, base, filter)
	}
	if m.focus == paneList {
		return slices.Concat([]key.Binding{m.keys.Enter, m.keys.Filter, m.keys.GroupBy, m.keys.Board, m.keys.ToggleDone}, base, filter)
	}
	if m.focus == paneDetail {
		return append([]key.Binding{m.keys.Up, m.keys.Down, m.keys.Left}, base...)
	}
	return append([]key.Binding{m.keys.Enter, m.keys.Filter, m.keys.Pin, m.keys.Pinned}, base...)
}

// createPreset is the folder the new task goes into before the user changes it:
// the folder of the group under the cursor when the rows are grouped by folder, else the node in view.
// My tasks is no folder, there the box starts empty.
func (m Model) createPreset() string {
	if m.list.groupBy == groupFolder {
		if g := m.list.groupOf(m.list.cursor); g >= 0 && m.list.groups[g].id != "" {
			return m.list.groups[g].id
		}
	}
	if m.selectedNode.kind == nodeMe || m.selectedNode.kind == nodeNone {
		return ""
	}
	return m.selectedNode.id
}

// folderCrumbs is the path of every sidebar node by id, for the folder line of the create box.
// The nodes come in tree order with their depth, so one walk with the path so far gives every crumb.
// A folder under two parents is listed where it first appears, as the box lists it.
func (m Model) folderCrumbs() map[string]string {
	out := map[string]string{}
	var path []string
	for _, n := range m.sidebar.nodes {
		if n.kind == nodeMe {
			continue
		}
		path = append(path[:n.depth], n.title)
		if _, ok := out[n.id]; !ok {
			out[n.id] = strings.Join(path, " / ")
		}
	}
	return out
}

func (m Model) helpGroups() [][]key.Binding {
	if m.screen == screenIssues {
		return [][]key.Binding{m.keys.global(), m.keys.issues()}
	}
	if m.screen == screenTimesheet {
		if m.timesheet.detailFocus {
			return [][]key.Binding{m.keys.global(), m.keys.task()}
		}
		return [][]key.Binding{m.keys.global(), m.keys.timesheet()}
	}
	if m.screen == screenSettings {
		return [][]key.Binding{m.keys.global(), m.keys.settings()}
	}
	if m.shape == shapeBoard {
		return [][]key.Binding{m.keys.global(), m.keys.board(), m.keys.task()}
	}
	return [][]key.Binding{m.keys.global(), m.keys.list(), m.keys.task()}
}
