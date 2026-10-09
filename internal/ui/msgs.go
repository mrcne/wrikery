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
type treeLoadedMsg struct {
	nodes []treeNode
	pins  *pinState // nil keeps the pins the sidebar has
}
type nodeSelectedMsg struct{ node treeNode } // intent: show this node's tasks
type focusMsg struct{ pane pane }            // intent: move focus
type tasksLoadedMsg struct {
	nodeID, crumb string
	tasks         []store.Task
	states        map[string]store.OutboxState
	selectID      string // the row to land on: a create just queued, a jump, or the task on screen after a swap
}
type taskSelectedMsg struct{ id string } // intent: show this task in the detail pane
type taskGoneMsg struct{ id string }     // the task is no longer in the cache and no swap explains it
type taskLoadedMsg struct {
	asked    string // the id the read was asked for, the task's own unless a swap stepped in
	task     store.Task
	comments []store.Comment
	logs     []store.Timelog
	states   map[string]store.OutboxState
	crumb    string
}
type pickerLoadedMsg struct {
	spaces   []store.Space
	projects map[string][]store.Folder // per space id, projects directly under the root
	followed []store.Scope             // read in the same command, so the Follow box ticks the set as it stands at the read
}
type hostLoadedMsg struct{ host string } // the Wrike host the token probe found, shown on the settings screen
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

// scopesSavedMsg lands once the followed set chosen on the settings screen or the first run is written and read back,
// so the tree is built again from the new set only after the write is through, and a failed write never reports a success.
type scopesSavedMsg struct {
	scopes []store.Scope
	toast  string
}

func intent(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }
