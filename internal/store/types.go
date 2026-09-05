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

type Folder struct {
	ID       string
	Title    string
	Scope    string
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
	ID         string
	Title      string
	AccessType string
	Archived   bool
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
