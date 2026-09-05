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

// Hooks are the few things the UI needs from outside the store. Any of them may be nil, the UI then shows a toast.
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
}

func New(o Options) Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	m := Model{opts: o, theme: NewTheme(o.Config), keys: defaultKeyMap(), help: help.New(), focus: paneList}
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
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case SyncStateMsg:
		return m.onSyncState(msg.State)
	case StoreChangedMsg:
		return m, m.reload(msg.Entities)
	case OutboxChangedMsg:
		m.status.pending, m.status.failed = msg.Pending, msg.Failed
		return m, nil
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
		return m, nil
	case firstRunSubmitTokenMsg:
		return m, m.verifyToken(msg.token)
	case firstRunConfirmScopesMsg:
		var cmd tea.Cmd
		m.firstRun, cmd = m.firstRun.Update(msg)
		return m, tea.Batch(cmd, m.saveScopes(msg.scopes), m.firstRun.spinner.Tick)
	case firstRunFinishedMsg:
		m.screen = screenMain
		return m, tea.Batch(m.loadRef(), m.loadScopes())
	case tokenVerifiedMsg:
		var cmd tea.Cmd
		m.firstRun, cmd = m.firstRun.Update(msg)
		if msg.err == nil {
			cmd = tea.Batch(cmd, m.loadPicker(), m.firstRun.spinner.Tick)
		}
		return m, cmd
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
		m.screen = screenFirstRun
		m.firstRun = newFirstRun(stepToken, "Wrike rejected the token. Paste a new one to continue.", m.keys)
	}
	if state != "offline" {
		m.status.offlineSince = time.Time{}
	}
	return m, nil
}

// reload re-reads what is on screen for the caches that changed. Later PRs add their panes here.
func (m Model) reload(entities []string) tea.Cmd {
	var cmds []tea.Cmd
	if slices.Contains(entities, "contacts") || slices.Contains(entities, "workflows") {
		cmds = append(cmds, m.loadRef())
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
	if m.screen == screenFirstRun {
		var cmd tea.Cmd
		m.firstRun, cmd = m.firstRun.Update(msg)
		return m, cmd
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.overlay = overlayHelp
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
	return m, nil
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
		hints = m.help.ShortHelpView(m.hintBindings())
	}
	out := body + "\n" + m.status.View(m.theme, m.width, hints, m.opts.Now())
	if m.overlay == overlayHelp {
		out = centered(out, helpView(m.theme, m.help, m.helpGroups(), m.width), m.width, m.height)
	}
	return out
}

func (m Model) viewMain(height int) string {
	lay := computeLayout(m.width, height, m.focus, 28)
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
		return "Tasks"
	}
	return "Task"
}

// paneBody is empty in this PR. The browse PR fills it from the child models.
func (m Model) paneBody(p pane, r rect) string { return "" }

func (m Model) hintBindings() []key.Binding {
	return []key.Binding{m.keys.NextPane, m.keys.Search, m.keys.Timesheet, m.keys.Help, m.keys.Quit}
}

func (m Model) helpGroups() [][]key.Binding {
	return [][]key.Binding{m.keys.global(), m.keys.list(), m.keys.task()}
}
