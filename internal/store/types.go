package store

type Task struct {
	ID               string
	Title            string
	Description      string // raw API HTML
	DescriptionPlain string // filled by the store on write, callers never set it
	Status           string
	CustomStatusID   string
	Importance       string
	Permalink        string
	ResponsibleIDs   []string
	ParentIDs        []string
	SuperTaskIDs     []string // the tasks this one is a subtask of
	DependencyIDs    []string // the edges this task is an end of, see Dependency
	AttachmentCount  int
	FollowedByMe     bool               // the pulls ask for it, every read by id and every write's answer carry it
	Dates            *TaskDates         // nil when the task has no dates block
	CustomFields     []CustomFieldValue // the task's custom field values, nil when it has none
	Effort           *TaskEffort        // the Effort field, nil until a read asked for it, see the upsert
	CreatedDate      string
	UpdatedDate      string
	LastOpenedAt     string // empty when never opened, local bookeeping
	ThreadReadAt     string // the last per task read of the comments and time entries, empty when never read, local bookkeeping
}

type TaskDates struct {
	Type     string
	Duration int
	Start    string
	Due      string
}

// CustomField is a custom field the account defines, the type is the API's, Duration, Numeric, Text and the rest.
type CustomField struct {
	ID, Title, Type string
	SpaceID         string // empty for an account level field, the space that owns the field otherwise
}

// CustomFieldValue is one value a task carries, a string whatever the field's type.
type CustomFieldValue struct {
	ID, Value string
}

// TaskEffort is the Effort field of a task: the minutes the API calls totalEffort and the mode, Basic, Flexible, FullTime or None.
type TaskEffort struct {
	Mode         string
	TotalMinutes int
}

// Dependency is one scheduling edge between two tasks. Either end can be a task outside the cache.
type Dependency struct {
	ID            string
	PredecessorID string
	SuccessorID   string
	RelationType  string // FinishToStart, StartToStart, FinishToFinish or StartToFinish
	LagMinutes    int
}

// RelatedIDs names the tasks a task's relations point at, the super tasks and the other end of every edge, each once.
func RelatedIDs(t Task, deps []Dependency) []string {
	seen := map[string]bool{t.ID: true}
	var out []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range t.SuperTaskIDs {
		add(id)
	}
	for _, dep := range deps {
		add(dep.PredecessorID)
		add(dep.SuccessorID)
	}
	return out
}

type Folder struct {
	ID       string
	Title    string
	Scope    string
	Space    bool // true for a space root folder, the API's "space" flag
	ChildIDs []string
	Project  *Project // nil for plain folders
}

type Project struct {
	Status         string
	CustomStatusID string
	StartDate      string
	EndDate        string
}

type Space struct {
	ID                    string
	Title                 string
	AccessType            string
	Archived              bool
	DefaultTaskWorkflowID string // the workflow a task created in the space starts in, empty when unknown
}

type Contact struct {
	ID           string
	FirstName    string
	LastName     string
	Type         string
	PrimaryEmail string
	Deleted      bool
	Me           bool
}

type Workflow struct {
	ID             string
	Name           string
	Standard       bool
	Hidden         bool
	CustomStatuses []CustomStatus
}

type CustomStatus struct {
	ID       string
	Name     string
	Color    string
	Group    string
	Standard bool
	Hidden   bool
}

type Comment struct {
	ID          string
	TaskID      string
	AuthorID    string
	Text        string
	CreatedDate string
	Mentions    []string // who the comment mentions: user ids and the group forms @assignees and @followers, nil for nobody
	ArrivedAt   string   // when the row first entered this cache, local bookkeeping, the inbox marks a row against it and not against its creation date
}

type Timelog struct {
	ID             string
	TaskID         string
	UserID         string
	CategoryID     string
	TrackedDate    string
	Comment        string
	Hours          float64
	LockStatus     string
	ApprovalStatus string
	CreatedDate    string
	UpdatedDate    string
}

const (
	ScopeKindSpace   = "space"
	ScopeKindProject = "project"
	ScopeKindMe      = "me"
)

type Scope struct {
	ID           string
	Kind         string // one of the ScopeKind constants
	Title        string
	Followed     bool
	Cursor       string // empty means initial sync has not completed
	LastSyncedAt string
}

// MetaKeyMe is the meta row holding the current user's contact id. The syncer writes it, the UI reads it.
const MetaKeyMe = "me_contact_id"

// MetaKeyHost is the host the token probe found, written by cmd/wrikery, empty means not probed yet.
const MetaKeyHost = "api_host"

// MetaKeyAccountID and MetaKeyAccountName name the Wrike account the cache was built from, see Home.
const (
	MetaKeyAccountID   = "account_id"
	MetaKeyAccountName = "account_name"
)

// MetaKeyHomeHost is the host the cache's credential was verified on, whatever the config says, see Home.
const MetaKeyHomeHost = "home_host"

// MetaKeySwitchPending is "1" from a move to another account or data center until a drain pass checked the queue against it.
const MetaKeySwitchPending = "switch_pending"

// MetaKeyRebuildDue is "1" from a move until a full cycle swept the cache with the scopes the new account answered.
const MetaKeyRebuildDue = "rebuild_due"

// MetaKeySidebarPinned is "1" while the sidebar shows only the pinned nodes, so the choice survives a restart.
const MetaKeySidebarPinned = "sidebar_pinned_only"

// MetaKeySidebarHidden is "1" while the sidebar pane is hidden, so the choice survives a restart.
const MetaKeySidebarHidden = "sidebar_hidden"

// MetaKeyStatusMoveConfirmed is "1" once the user said yes to the first one key status move, the keys move at once from then on.
const MetaKeyStatusMoveConfirmed = "status_move_confirmed"

// MetaKeyEffortUnavailable is "1" after a read of the Effort field answered 403.
// The engine leaves the field out of its reads from then on, and a manual cycle clears the row and asks again.
const MetaKeyEffortUnavailable = "effort_unavailable"

// MetaKeyPullFields is the set of optional task fields the last run's pulls asked for, comma separated.
// A run whose set differs clears the scope cursors first, so every cached task is read once with the new field.
const MetaKeyPullFields = "pull_fields"

// MetaKeyTimelogFrom is the first day of the TimelogWindow the last timelog pull covered, an ISO date.
// The timesheet marks weeks before it as not synced, an empty value means no pull has run yet.
const MetaKeyTimelogFrom = "timelog_window_from"

// MetaKeyCommentsPulledTo and MetaKeyTimelogChangesPulledTo are the end of the last account wide window
// the comments pull and the time entry changes pull asked for, RFC 3339 on this machine's clock, empty before the first pull.
const (
	MetaKeyCommentsPulledTo       = "comments_pulled_to"
	MetaKeyTimelogChangesPulledTo = "timelog_changes_pulled_to"
)

// MetaKeyInboxSeenTo is the arrival stamp of the newest inbox row the user had on screen, empty for none.
// Arrival and not creation, because comments do not enter the cache in creation order: a task cached late,
// or a thread read on an open, brings comments older than what was seen already, and those must still show as unread.
// The stamp is this machine's clock on both sides, so a skew against Wrike's clock plays no part.
const MetaKeyInboxSeenTo = "inbox_seen_to"
