package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

// Messages main sends from engine events. Strings mirror the syncer constants so this package does not import it.
type SyncStateMsg struct{ State string }
type StoreChangedMsg struct{ Entities []string }
type OutboxChangedMsg struct{ Pending, Failed int }

type errMsg struct{ err error }

// toastMsg reports the result of work the update loop handed to a command, such as a clipboard write.
// The root turns it into a status bar toast.
type toastMsg struct {
	text  string
	isErr bool
}

type refData struct {
	meID      string
	contacts  map[string]store.Contact
	statuses  map[string]store.CustomStatus
	workflows []store.Workflow
}

type refLoadedMsg struct{ ref refData }
type scopesLoadedMsg struct{ scopes []store.Scope }
type treeLoadedMsg struct{ nodes []treeNode }
type nodeSelectedMsg struct{ node treeNode } // intent: show this node's tasks
type focusMsg struct{ pane pane }            // intent: move focus
type tasksLoadedMsg struct {
	nodeID, crumb string
	tasks         []store.Task
	states        map[string]store.OutboxState
}
type taskSelectedMsg struct{ id string } // intent: show this task in the detail pane
type taskLoadedMsg struct {
	task     store.Task
	comments []store.Comment
	logs     []store.Timelog
	states   map[string]store.OutboxState
	crumb    string
}
type pickerLoadedMsg struct {
	spaces   []store.Space
	projects map[string][]store.Folder // per space id, projects directly under the root
}
type tokenVerifiedMsg struct {
	name string
	err  error
}
type searchResultsMsg struct {
	seq    int
	tasks  []store.Task
	crumbs map[string]string
}
type openTaskMsg struct{ id, parentID string } // intent: leave the search overlay, the issues screen or the timesheet for this task
type searchPickMsg struct{ task store.Task }   // intent: pick mode, hand the task back to whoever asked for it
type runSearchMsg struct {
	seq   int
	query string
}

// openConfirmMsg is an intent from a child screen: the root opens a confirmDialog from it.
type openConfirmMsg struct {
	prompt string
	onYes  tea.Msg
}

// Intents from the first run child. The root turns them into commands.
type firstRunSubmitTokenMsg struct{ token string }
type firstRunConfirmScopesMsg struct{ scopes []store.Scope }
type firstRunFinishedMsg struct{}

func intent(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }
