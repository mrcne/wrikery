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
	Dates            *TaskDates // nil when the task has no dates block
	CreatedDate      string
	UpdatedDate      string
	LastOpenedAt     string // empty when never opened, local bookeeping
}

type TaskDates struct {
	Type     string
	Duration int
	Start    string
	Due      string
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

// MetaKeySidebarPinned is "1" while the sidebar shows only the pinned nodes, so the choice survives a restart.
const MetaKeySidebarPinned = "sidebar_pinned_only"

// MetaKeyTimelogFrom is the first day of the TimelogWindow the last timelog pull covered, an ISO date.
// The timesheet marks weeks before it as not synced, an empty value means no pull has run yet.
const MetaKeyTimelogFrom = "timelog_window_from"
