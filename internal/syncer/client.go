package syncer

import (
	"context"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// Client is the slice of the Wrike API the engine calls.
// pkg/wrike implements it, the tests use a fake.
type Client interface {
	Me(ctx context.Context) (wrike.Contact, error)
	Contacts(ctx context.Context) ([]wrike.Contact, error)
	Spaces(ctx context.Context) ([]wrike.Space, error)
	Workflows(ctx context.Context) ([]wrike.Workflow, error)
	FolderTree(ctx context.Context) ([]wrike.Folder, error)
	Tasks(ctx context.Context, p wrike.TaskParams) (wrike.TasksPage, error)
	TaskComments(ctx context.Context, taskID string) ([]wrike.Comment, error)
	TaskTimelogs(ctx context.Context, taskID string) ([]wrike.Timelog, error)
	UpdateTask(ctx context.Context, taskID string, u wrike.TaskUpdate) (wrike.Task, error)
	CreateComment(ctx context.Context, taskID, text string) (wrike.Comment, error)
	CreateTimelog(ctx context.Context, taskID string, hours float64, trackedDate, comment string) (wrike.Timelog, error)
	UpdateTimelog(ctx context.Context, timelogID string, u wrike.TimelogUpdate) (wrike.Timelog, error)
	DeleteTimelog(ctx context.Context, timelogID string) error
}

var _ Client = (*wrike.Client)(nil)
