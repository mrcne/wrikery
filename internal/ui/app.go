// Package ui holds the bubbletea application. It reads from the store, writes to the store and the outbox,
// and never talks to the network. main forwards engine events as messages and injects the hooks.
package ui

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// Hooks are the few things the UI needs from outside the store. Any of them may be nil, the UI then reports it instead of failing.
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

	status   statusModel
	firstRun firstRunModel
	ref      refData
	sidebar  sidebarModel
	list     taskListModel
	detail   taskDetailModel
	search   searchModel

	selectedNode   treeNode
	selectedTaskID string
	pendingSelect  string // task id to reselect once the next tasksLoadedMsg lands, set by reload after an outbox or task change
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

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadRef(), m.loadScopes())
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
		// The pending and failed markers live on the task row and on the detail header, so a queue change rereads both.
		return m, tea.Batch(m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode)), m.reloadTask())
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
		// Contact names and status names are drawn from ref, so the detail has to be built again once it lands.
		m.syncPaneSizes()
		// The tree needs meID for the open task count and statuses for the project glyphs, both live on ref.
		return m, m.loadTree()
	case treeLoadedMsg:
		m.sidebar.setNodes(msg.nodes)
		m.syncPaneSizes()
		if n, ok := m.sidebar.current(); ok {
			return m, intent(nodeSelectedMsg{node: n})
		}
		return m, nil
	case focusMsg:
		prev := m.focus
		m.focus = msg.pane
		m.syncPaneSizes()
		return m, m.openedOnFocus(prev)
	case nodeSelectedMsg:
		m.selectedNode = msg.node
		return m, m.loadTasks(msg.node, m.sidebar.crumb(msg.node))
	case tasksLoadedMsg:
		if m.list.nodeID != msg.nodeID && m.pendingSelect == "" {
			m.list.cursor = 0
		}
		m.list.setRows(msg.nodeID, msg.crumb, msg.tasks, msg.states, m.pendingSelect)
		m.pendingSelect = ""
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
	case "offline":
		if m.status.offlineSince.IsZero() {
			m.status.offlineSince = m.opts.Now()
		}
	case "auth_required":
		if m.screen != screenFirstRun {
			m.screen = screenFirstRun
			m.firstRun = newFirstRun(stepToken, "Wrike rejected the token. Paste a new one to continue.", m.keys)
			m.firstRun.reauth = true
		}
	}
	if state != "offline" {
		m.status.offlineSince = time.Time{}
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
	if slices.Contains(entities, "tasks") {
		cmds = append(cmds, m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode)))
	}
	if slices.Contains(entities, "tasks") || slices.Contains(entities, "comments") || slices.Contains(entities, "timelogs") {
		cmds = append(cmds, m.reloadTask())
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
	m.overlay = overlayNone
	m.search.blur()
	m.focus = paneDetail
	m.selectedTaskID = t.ID
	m.pendingSelect = t.ID
	var cmds []tea.Cmd
	if len(t.ParentIDs) > 0 && m.sidebar.selectByID(t.ParentIDs[0]) {
		n, _ := m.sidebar.current()
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

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
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
		if m.opts.Hooks.Refresh == nil {
			return m, m.status.show("refresh is not available", true)
		}
		m.opts.Hooks.Refresh()
		return m, m.status.show("refreshing", false)
	case key.Matches(msg, m.keys.NextPane):
		m.focus = (m.focus + 1) % 3
	case key.Matches(msg, m.keys.PrevPane):
		m.focus = (m.focus + 2) % 3
	case key.Matches(msg, m.keys.Back):
		if m.screen != screenMain {
			m.screen = screenMain
		} else if visibleCount(m.width) == 1 && m.focus > paneSidebar {
			m.focus--
		}
	}
	m.syncPaneSizes()
	opened := m.openedOnFocus(prevFocus)
	switch m.focus {
	case paneSidebar:
		var cmd tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(msg)
		return m, tea.Batch(opened, cmd)
	case paneList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, tea.Batch(opened, cmd)
	case paneDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, tea.Batch(opened, cmd)
	}
	return m, opened
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
	default:
		body = m.viewMain(bodyHeight)
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
	return out
}

// syncPaneSizes pushes the computed pane heights down to the child models.
// A View method takes its size as a plain argument each frame and has no way to remember it between calls on its own.
func (m *Model) syncPaneSizes() {
	lay := computeLayout(m.width, m.height-1, m.focus, m.sidebar.width())
	if r, ok := lay.rects[paneSidebar]; ok {
		m.sidebar.height = r.h - 2
	}
	if r, ok := lay.rects[paneList]; ok {
		m.list.height = r.h - 2
	}
	if r, ok := lay.rects[paneDetail]; ok {
		m.detail.layout(m.theme, m.ref, m.opts.Now(), r.w-2, r.h-2, m.opts.Config.Theme)
	}
}

func (m Model) viewMain(height int) string {
	lay := computeLayout(m.width, height, m.focus, m.sidebar.width())
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
	}
	return m.detail.title()
}

func (m Model) paneBody(p pane, r rect) string {
	switch p {
	case paneSidebar:
		return m.sidebar.View(m.theme, r.w-2, r.h-2, m.focus == paneSidebar)
	case paneList:
		return m.list.View(m.theme, m.ref, m.opts.Now(), r.w-2, r.h-2, m.focus == paneList)
	}
	// The detail pane laid itself out in Update, View only reads the viewport.
	return m.detail.View()
}

// Only the keys that already do something, the overlay lists the whole map.
func (m Model) hintBindings() []key.Binding {
	base := []key.Binding{m.keys.NextPane, m.keys.Search, m.keys.Help, m.keys.Quit}
	if m.focus == paneList {
		return append([]key.Binding{m.keys.Enter, m.keys.Filter, m.keys.ToggleDone}, base...)
	}
	if m.focus == paneDetail {
		return append([]key.Binding{m.keys.Up, m.keys.Down, m.keys.Left}, base...)
	}
	return base
}

func (m Model) helpGroups() [][]key.Binding {
	return [][]key.Binding{m.keys.global(), m.keys.list(), m.keys.task()}
}
