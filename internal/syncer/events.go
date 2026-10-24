package syncer

type SyncState string

const (
	StateIdle         SyncState = "idle"
	StateSyncing      SyncState = "syncing"
	StateOffline      SyncState = "offline"
	StateFailed       SyncState = "failed"
	StateAuthRequired SyncState = "auth_required"
	// StateAnotherAccount stops the engine until the user decides about a credential of another account, see AnotherAccountError.
	StateAnotherAccount SyncState = "another_account"
)

type EntityKind string

const (
	KindTasks     EntityKind = "tasks"
	KindFolders   EntityKind = "folders"
	KindSpaces    EntityKind = "spaces"
	KindContacts  EntityKind = "contacts"
	KindWorkflows EntityKind = "workflows"
	KindComments  EntityKind = "comments"
	KindTimelogs  EntityKind = "timelogs"
	// KindDependencies changes only what the task detail draws, the tree and the lists need no reload for it.
	KindDependencies EntityKind = "dependencies"
)

// AllEntities is every kind, what a full reload names. A new kind goes here next to its constant.
var AllEntities = []EntityKind{KindTasks, KindFolders, KindSpaces, KindContacts, KindWorkflows, KindComments, KindTimelogs, KindDependencies}

type EventKind string

const (
	EventStoreChanged  EventKind = "store_changed"
	EventStateChanged  EventKind = "state_changed"
	EventOutboxChanged EventKind = "outbox_changed"
	// EventScopesLost says the first full cycle after a move found followed scopes the new account does not have.
	EventScopesLost EventKind = "scopes_lost"
)

// Event is a refresh hint for the UI, never a delta. Receivers re-read what they show from the store.
// The engine drops events when the buffer is full, so nothing may depend on seeing every one.
// That includes the outbox counts below: they are the numbers at emit time, the store has the current ones.
type Event struct {
	Kind     EventKind
	Entities []EntityKind // for store_changed, which caches were touched
	State    SyncState    // for state_changed
	Pending  int          // for outbox_changed
	Failed   int
}
