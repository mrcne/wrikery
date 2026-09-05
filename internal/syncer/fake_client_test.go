package syncer

import (
	"context"
	"sync"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// fakeClient is the Client used by every test in this package that needs to drive the engine against scripted responses.
type fakeClient struct {
	mu    sync.Mutex
	calls []string

	me            func() (wrike.Contact, error)
	contacts      func() ([]wrike.Contact, error)
	spaces        func() ([]wrike.Space, error)
	workflows     func() ([]wrike.Workflow, error)
	folderTree    func() ([]wrike.Folder, error)
	tasks         func(p wrike.TaskParams) (wrike.TasksPage, error)
	taskComments  func(taskID string) ([]wrike.Comment, error)
	taskTimelogs  func(taskID string) ([]wrike.Timelog, error)
	updateTask    func(taskID string, u wrike.TaskUpdate) (wrike.Task, error)
	createComment func(taskID, text string) (wrike.Comment, error)
	createTimelog func(taskID string, hours float64, trackedDate, comment string) (wrike.Timelog, error)
	updateTimelog func(timelogID string, u wrike.TimelogUpdate) (wrike.Timelog, error)
	deleteTimelog func(timelogID string) error
}

func (f *fakeClient) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakeClient) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeClient) Me(ctx context.Context) (wrike.Contact, error) {
	f.record("Me")
	if f.me == nil {
		return wrike.Contact{ID: "U1", Me: true}, nil
	}
	return f.me()
}

func (f *fakeClient) Contacts(ctx context.Context) ([]wrike.Contact, error) {
	f.record("Contacts")
	if f.contacts == nil {
		return nil, nil
	}
	return f.contacts()
}

func (f *fakeClient) Spaces(ctx context.Context) ([]wrike.Space, error) {
	f.record("Spaces")
	if f.spaces == nil {
		return nil, nil
	}
	return f.spaces()
}

func (f *fakeClient) Workflows(ctx context.Context) ([]wrike.Workflow, error) {
	f.record("Workflows")
	if f.workflows == nil {
		return nil, nil
	}
	return f.workflows()
}

func (f *fakeClient) FolderTree(ctx context.Context) ([]wrike.Folder, error) {
	f.record("FolderTree")
	if f.folderTree == nil {
		return nil, nil
	}
	return f.folderTree()
}

func (f *fakeClient) Tasks(ctx context.Context, p wrike.TaskParams) (wrike.TasksPage, error) {
	f.record("Tasks " + p.FolderID + p.SpaceID)
	if f.tasks == nil {
		return wrike.TasksPage{}, nil
	}
	return f.tasks(p)
}

func (f *fakeClient) TaskComments(ctx context.Context, taskID string) ([]wrike.Comment, error) {
	f.record("TaskComments " + taskID)
	if f.taskComments == nil {
		return nil, nil
	}
	return f.taskComments(taskID)
}

func (f *fakeClient) TaskTimelogs(ctx context.Context, taskID string) ([]wrike.Timelog, error) {
	f.record("TaskTimelogs " + taskID)
	if f.taskTimelogs == nil {
		return nil, nil
	}
	return f.taskTimelogs(taskID)
}

func (f *fakeClient) UpdateTask(ctx context.Context, taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
	f.record("UpdateTask " + taskID)
	if f.updateTask == nil {
		return wrike.Task{ID: taskID}, nil
	}
	return f.updateTask(taskID, u)
}

func (f *fakeClient) CreateComment(ctx context.Context, taskID, text string) (wrike.Comment, error) {
	f.record("CreateComment " + taskID)
	if f.createComment == nil {
		return wrike.Comment{ID: "C1", TaskID: taskID, Text: text}, nil
	}
	return f.createComment(taskID, text)
}

func (f *fakeClient) CreateTimelog(ctx context.Context, taskID string, hours float64, trackedDate, comment string) (wrike.Timelog, error) {
	f.record("CreateTimelog " + taskID)
	if f.createTimelog == nil {
		return wrike.Timelog{ID: "L1", TaskID: taskID, TrackedDate: trackedDate, Hours: hours, Comment: comment}, nil
	}
	return f.createTimelog(taskID, hours, trackedDate, comment)
}

func (f *fakeClient) UpdateTimelog(ctx context.Context, timelogID string, u wrike.TimelogUpdate) (wrike.Timelog, error) {
	f.record("UpdateTimelog " + timelogID)
	if f.updateTimelog == nil {
		return wrike.Timelog{ID: timelogID}, nil
	}
	return f.updateTimelog(timelogID, u)
}

func (f *fakeClient) DeleteTimelog(ctx context.Context, timelogID string) error {
	f.record("DeleteTimelog " + timelogID)
	if f.deleteTimelog == nil {
		return nil
	}
	return f.deleteTimelog(timelogID)
}
