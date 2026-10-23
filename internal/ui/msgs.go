package ui

import (
	"time"

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

// statusOf is the custom status of a task as far as the reference data knows it.
// A status id the cache has no row for keeps the group the task itself carries, so the glyph and the colour still have something to go on.
func (r refData) statusOf(t store.Task) store.CustomStatus {
	cs := r.statuses[t.CustomStatusID]
	if cs.Group == "" {
		cs.Group = t.Status
	}
	return cs
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
	lift          bool   // show the row even where the done toggle or a filter would hide it, a jump and a create ask for it, a swap does not
}
type taskSelectedMsg struct{ id string } // intent: show this task in the detail pane
type taskGoneMsg struct{ id string }     // the task is no longer in the cache and no swap explains it

// notesLoadedMsg carries the notes of a task's earlier entries to the log time box open on it.
type notesLoadedMsg struct {
	taskID string
	notes  []string
}

type taskLoadedMsg struct {
	asked    string // the id the read was asked for, the task's own unless a swap stepped in
	task     store.Task
	comments []store.Comment
	logs     []store.Timelog
	subtasks []store.Task
	deps     []store.Dependency
	related  map[string]store.Task // the cached super tasks and dependency ends by id, a missing one is outside the followed spaces
	states   map[string]store.OutboxState
	crumb    string
}
type pickerLoadedMsg struct {
	spaces   []store.Space
	projects map[string][]store.Folder // per space id, projects directly under the root
	followed []store.Scope             // read in the same command, so the Follow box ticks the set as it stands at the read
}
type hostLoadedMsg struct{ host string }                       // the Wrike host the token probe found, shown on the settings screen
type savedFlagsMsg struct{ sidebarHidden, moveConfirmed bool } // the choices kept in the meta table, read once at start
type tokenVerifiedMsg struct {
	name    string
	err     error
	browser bool   // the sign in ran through the browser, not through a pasted token
	app     string // the client id of the app a browser sign in used
}
type searchResultsMsg struct {
	seq    int
	tasks  []store.Task
	crumbs map[string]string
	ref    string // the query as a task reference the cache lacks, drawn as the row that fetches it from Wrike, else empty
}

// reachOrigin says who asked for a task from outside the followed scopes, so the answer lands where the ask came from.
type reachOrigin int

const (
	reachFromSearch    reachOrigin = iota // the quick search, the task opens on the main screen
	reachFromPick                         // the search in pick mode, the task goes to the time entry box
	reachFromTimesheet                    // enter on a timesheet row, the task opens beside the grid
)

type reachMsg struct { // intent: fetch a task the cache lacks from Wrike
	ref    string
	origin reachOrigin
}
type reachedMsg struct { // the answer to a reachMsg, task or err
	ref    string
	origin reachOrigin
	task   store.Task
	err    error
}
type openTaskMsg struct{ task store.Task }   // intent: leave the search overlay or the changes screen for this task
type openBesideMsg struct{ id string }       // intent: show the task in the detail pane beside the timesheet grid
type searchPickMsg struct{ task store.Task } // intent: pick mode, hand the task back to whoever asked for it
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
type firstRunStartSignInMsg struct {
	attempt int
	secret  string
}
type firstRunCopyAddressMsg struct{ address string }

// signInStartedMsg lands when the hook started a browser sign in, or could not.
type signInStartedMsg struct {
	attempt int
	signIn  SignIn
	err     error
}

// signInResultMsg is the end of a browser sign in. The attempt tells the late answer of a cancelled one apart from the current one.
type signInResultMsg struct {
	attempt int
	name    string
	app     string
	err     error
}
type firstRunConfirmScopesMsg struct{ scopes []store.Scope }
type firstRunFinishedMsg struct{}

// scopesSavedMsg lands once the followed set chosen on the settings screen or the first run is written and read back,
// so the tree is built again from the new set only after the write is through, and a failed write never reports a success.
type scopesSavedMsg struct {
	scopes []store.Scope
	toast  string
}

func intent(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// ControlMsg is a request from a command on the control socket, handed in by cmd/wrikery the way the engine events are.
// The fields follow the operation, the rest stay empty. Reply is buffered by the sender, so Update never waits on it.
// Deadline is when the sender stopped waiting: a message held up behind an external editor is dropped past it,
// the command was told the interface is busy and a filter that lands minutes later would surprise. Zero means no deadline.
type ControlMsg struct {
	Op             string
	Importance     []string
	Person         string
	Statuses       []string
	TaskID         string
	ParentIDs      []string // the folders of the task to select, the first one the sidebar has is opened
	ResponsibleIDs []string // the people on that task, the user's own task lands in My tasks when no folder is in the sidebar
	FolderID       string
	Shape          string
	Screen         string
	Deadline       time.Time
	Reply          chan<- ControlReply
}

// ControlReply is the interface's answer: a line for the person, or the reason it did nothing.
type ControlReply struct {
	Text string
	Err  error
}
