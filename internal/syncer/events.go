package syncer

type SyncState string

const (
	StateIdle         SyncState = "idle"
	StateSyncing      SyncState = "syncing"
	StateOffline      SyncState = "offline"
	StateAuthRequired SyncState = "auth_required"
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
)

type EventKind string

const (
	EventStoreChanged  EventKind = "store_changed"
	EventStateChanged  EventKind = "state_changed"
	EventOutboxChanged EventKind = "outbox_changed"
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
