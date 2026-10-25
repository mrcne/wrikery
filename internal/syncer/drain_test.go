package syncer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestDrainSendsInOrderAndCompletes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "before")

	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "after", Description: "<p>d</p>", Importance: "Low", AddParents: []string{"F2"}}); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{
		createComment: func(taskID, text string) (wrike.Comment, error) {
			return wrike.Comment{ID: "C9", TaskID: taskID, AuthorID: "U1", Text: text,
				CreatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}, nil
		},
		updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
			if u.Importance != "Low" {
				t.Errorf("update sent importance %q, want the queued Low", u.Importance)
			}
			if u.Description != "<p>d</p>" {
				t.Errorf("update sent description %q, want the queued HTML", u.Description)
			}
			if len(u.AddParents) != 1 || u.AddParents[0] != "F2" {
				t.Errorf("update sent parents %v, want the queued F2", u.AddParents)
			}
			return wrike.Task{ID: taskID, Title: u.Title, Status: "Active",
				UpdatedDate: time.Date(2026, 9, 3, 10, 5, 0, 0, time.UTC)}, nil
		},
	}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v, want changed and no error", changed, err)
	}

	log := fc.callLog()
	// The edit reads its task first, the check against a change made on Wrike meanwhile.
	if len(log) != 3 || log[0] != "CreateComment T1" || log[1] != "TasksByIDs T1" || log[2] != "UpdateTask T1" {
		t.Fatalf("calls = %v, want comment first by outbox order", log)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Fatalf("counts = %d, %d, %v", pending, failed, err)
	}
	comments, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil || len(comments) != 1 || comments[0].ID != "C9" {
		t.Fatalf("comments = %+v, %v, want the confirmed C9", comments, err)
	}
	task, err := st.Tasks().Get(ctx, "T1")
	if err != nil || task.Title != "after" || task.UpdatedDate != "2026-09-03T10:05:00Z" {
		t.Fatalf("task = %+v, %v, the server response must land in the cache", task, err)
	}
}

func TestDrainRejectedWriteFailsRowAndContinues(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "doomed"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "fine"); err != nil {
		t.Fatal(err)
	}

	first := true
	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		if first {
			first = false
			return wrike.Comment{}, &wrike.APIError{StatusCode: 403, Code: "access_forbidden", Description: "no"}
		}
		return wrike.Comment{ID: "C2", TaskID: taskID, Text: text}, nil
	}}

	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 1 {
		t.Fatalf("counts = %d, %d, %v, want the rejection failed and the rest drained", pending, failed, err)
	}
	rows := failedRows(t, st)
	if len(rows) != 1 || rows[0].LastError == "" {
		t.Fatalf("failed rows = %+v, want the error text kept", rows)
	}
}

func TestDrainTransientStopsAndReschedules(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	firstID, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "two"); err != nil {
		t.Fatal(err)
	}

	// A 500 may be this payload's own fault, so the row waits longer each time, unlike a Wrike that does not answer.
	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		return wrike.Comment{}, &wrike.APIError{StatusCode: 500, Code: "server_error"}
	}}

	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err == nil {
		t.Fatal("want the transient error back so the engine goes to backoff")
	}
	if changed {
		t.Error("changed = true, nothing completed or failed")
	}
	if got := fc.callLog(); len(got) != 1 {
		t.Fatalf("calls = %v, the drain must stop at the first transient failure", got)
	}
	pending, failed, cerr := st.Outbox().Counts(ctx)
	if cerr != nil || pending != 2 || failed != 0 {
		t.Fatalf("counts = %d, %d, %v, a 500 must never fail a row", pending, failed, cerr)
	}
	row, rerr := st.Outbox().NextDue(ctx, "2027-01-01T00:00:00Z")
	if rerr != nil || row.ID != firstID || row.Attempts != 1 || row.NextAttemptAt == "" {
		t.Fatalf("row = %+v, %v, want attempts 1 and a future next attempt", row, rerr)
	}
}

func TestDrainDefersARowWhenWrikeIsUnavailable(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")
	firstID, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "two"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		return wrike.Comment{}, &wrike.APIError{StatusCode: 503}
	}}
	if _, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute); err == nil {
		t.Fatal("want the error back so the cycle fails and the engine waits")
	}
	if got := fc.callLog(); len(got) != 1 {
		t.Fatalf("calls = %v, the drain must stop at the first failure", got)
	}
	row, err := st.Outbox().Get(ctx, firstID)
	if err != nil || row.State != store.StatePending || row.Attempts != 0 || row.LastError == "" || row.NextAttemptAt != "" {
		t.Fatalf("row = %+v, %v, want pending with the error, no attempt and no wait of its own", row, err)
	}
	due, err := st.Outbox().NextDue(ctx, rfc3339(time.Now()))
	if err != nil || due.ID != firstID {
		t.Errorf("next due = %d, %v, want the oldest row again at once", due.ID, err)
	}
}

// seedEditable caches TASK1 with a status, a title, an importance, dates and a description, and the workflow that names its statuses.
func seedEditable(t *testing.T, st *store.Store, description string) {
	t.Helper()
	ctx := context.Background()
	if err := st.Workflows().ReplaceAll(ctx, []store.Workflow{{ID: "WF1", Name: "Default Workflow", Standard: true,
		CustomStatuses: []store.CustomStatus{
			{ID: "ST_NEW", Name: "New", Group: "Active"},
			{ID: "ST_PROG", Name: "In Progress", Group: "Active"},
			{ID: "ST_DONE", Name: "Completed", Group: "Completed"},
		}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Upsert(ctx, []store.Task{{ID: "TASK1", Title: "Offline mode", Status: "Active", CustomStatusID: "ST_NEW",
		Importance: "Normal", Description: description, Dates: &store.TaskDates{Type: "Backlog"},
		CreatedDate: "2026-10-01T10:00:00Z", UpdatedDate: "2026-10-01T10:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
}

// onWrike is TASK1 as the read by id answers it, the cached values unless changed.
func onWrike(change func(*wrike.Task)) func(ids, fields []string) ([]wrike.Task, error) {
	return func(ids, fields []string) ([]wrike.Task, error) {
		task := wrike.Task{ID: "TASK1", Title: "Offline mode", Status: "Active", CustomStatusID: "ST_NEW", Importance: "Normal",
			Dates: &wrike.TaskDates{Type: "Backlog"}}
		if change != nil {
			change(&task)
		}
		return []wrike.Task{task}, nil
	}
}

func TestDrainHoldsATaskEditWhoseFieldChangedOnWrike(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{CustomStatusID: "ST_PROG", Status: "Active"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(func(task *wrike.Task) { task.CustomStatusID, task.Status = "ST_DONE", "Completed" })}
	changed, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v, want a held row to count as a change and the pass to go on", changed, err)
	}
	for _, call := range fc.callLog() {
		if call == "UpdateTask TASK1" {
			t.Fatal("the edit was sent over a newer status on Wrike")
		}
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != store.StateFailed || row.LastError != `changed on Wrike since you set it: status "Completed" now, was "New"` {
		t.Errorf("row = %s %q, want failed with the reason", row.State, row.LastError)
	}
	var before store.TaskUpdatePayload
	if err := json.Unmarshal(row.Before, &before); err != nil || before.CustomStatusID != "ST_DONE" || before.Status != "Completed" {
		t.Errorf("before = %s, %v, want what Wrike holds now", row.Before, err)
	}
	if !store.IsHeld(row.LastError) {
		t.Error("store.IsHeld does not know the text")
	}
}

func TestDrainSendsATaskEditWhoseFieldsMatchWrike(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{CustomStatusID: "ST_PROG", Status: "Active",
		Title: "Offline mode, sent safely", Importance: "High", Dates: &store.TaskDates{Type: "Planned", Start: "2026-10-23", Due: "2026-10-24"}})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(nil)}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := fc.callLog(); len(got) != 2 || got[0] != "TasksByIDs TASK1" || got[1] != "UpdateTask TASK1" {
		t.Errorf("calls = %v, want the read and then the edit", got)
	}
	wantSent(t, st, id)
}

// A send that reached Wrike but lost its answer, a quit or a timeout mid-send, comes back with its own values on Wrike,
// and so does a change someone else made the same way. Neither is a conflict.
func TestDrainSendsAnEditWhoseValuesWrikeAlreadyHolds(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "<p>Old words</p>")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{CustomStatusID: "ST_PROG", Status: "Active",
		Title: "Offline mode, sent safely", Description: "<p>New words</p>", Dates: &store.TaskDates{Type: "Planned", Start: "2026-10-23", Due: "2026-10-24"}})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(func(task *wrike.Task) {
		task.CustomStatusID, task.Title, task.Description = "ST_PROG", "Offline mode, sent safely", "<br />New words<br />"
		task.Dates = &wrike.TaskDates{Type: "Planned", Duration: 960, Start: "2026-10-23T09:00:00", Due: "2026-10-24T17:00:00"}
	})}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	wantSent(t, st, id)
}

// One read serves every edit of a task in a pass, kept current with the answer of each send:
// the second title edit remembers the first one's title, which only the first send's answer has.
func TestDrainReadsATaskOncePerPass(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "")
	first, _ := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Title: "Offline mode, first"})
	second, _ := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Title: "Offline mode, second"})
	fc := &fakeClient{
		tasksByIDs: onWrike(nil),
		updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
			return wrike.Task{ID: taskID, Title: u.Title, Status: "Active", CustomStatusID: "ST_NEW", Importance: "Normal",
				Dates: &wrike.TaskDates{Type: "Backlog"}}, nil
		},
	}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, call := range fc.callLog() {
		if call == "TasksByIDs TASK1" {
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("task read %d times in one pass, want once: %v", reads, fc.callLog())
	}
	wantSent(t, st, first)
	wantSent(t, st, second)
}

// A joined emoji, a zero width joiner between two parts, stays as it is in the held text, an escape would print as letters.
func TestDrainHeldTextKeepsATitleAsItIs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Title: "Mine"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(func(task *wrike.Task) { task.Title = "Ship\u200d it" })}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil || !strings.Contains(row.LastError, "title \"Ship\u200d it\" now") || strings.Contains(row.LastError, `\u200d`) {
		t.Errorf("held text = %q, %v, want the title as Wrike holds it", row.LastError, err)
	}
}

// Two description edits of one task in one pass: the second remembers the first one's markdown HTML,
// Wrike's answer to the first send writes = in a link as &#61; and paragraphs as line breaks, and nothing is held.
func TestDrainSendsTwoDescriptionEditsOfOneTaskInOnePass(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, `<p>See https://x/?a=1</p>`)
	first, _ := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Description: `<p>See https://x/?a=1 once</p>`})
	second, _ := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Description: `<p>See https://x/?a=1 twice</p>`})
	fc := &fakeClient{
		tasksByIDs: onWrike(func(task *wrike.Task) { task.Description = `<p>See https://x/?a=1</p>` }),
		updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
			stored := strings.NewReplacer("=", "&#61;", "<p>", "<br />", "</p>", "<br />").Replace(u.Description)
			return wrike.Task{ID: taskID, Title: "Offline mode", Status: "Active", CustomStatusID: "ST_NEW", Importance: "Normal",
				Description: stored, Dates: &wrike.TaskDates{Type: "Backlog"}}, nil
		},
	}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	wantSent(t, st, first)
	wantSent(t, st, second)
}

func TestDrainSkipsTheReadForAFolderChange(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "")
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{AddParents: []string{"F2"}}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(nil)}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := fc.callLog(); len(got) != 1 || got[0] != "UpdateTask TASK1" {
		t.Errorf("calls = %v, a folder change cannot conflict and needs no read", got)
	}
}

func TestDrainSkipsTheReadForAnAssigneeChange(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "")
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{AddResponsibles: []string{"U2"}}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(nil)}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := fc.callLog(); len(got) != 1 || got[0] != "UpdateTask TASK1" {
		t.Errorf("calls = %v, an add or a remove cannot conflict and needs no read", got)
	}
}

// Wrike writes = in a URL as &#61; in its answer and turns paragraphs into line breaks a minute later,
// so the description is compared as text and those two never hold an edit.
func TestDrainComparesTheDescriptionAsText(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, `<p>See https://x/?a=1 and <a href="https://x/?a=1">x</a></p><p>Second</p>`)
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Description: "<p>New words</p>"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(func(task *wrike.Task) {
		task.Description = `<br />See https://x/?a&#61;1 and <a href="https://x/?a&#61;1">x</a><br />Second<br />`
	})}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	wantSent(t, st, id)
}

func TestDrainHoldsADescriptionWhoseTextChanged(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "<p>Old words</p>")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Description: "<p>New words</p>"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: onWrike(func(task *wrike.Task) { task.Description = "<p>Words someone else wrote</p>" })}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil || row.State != store.StateFailed || row.LastError != "changed on Wrike since you set it: description" {
		t.Errorf("row = %+v, %v, want held over the description", row, err)
	}
}

func TestDrainSendsAnEditWithoutABeforeUnchecked(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "GHOST", store.TaskUpdatePayload{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := fc.callLog(); len(got) != 1 || got[0] != "UpdateTask GHOST" {
		t.Errorf("calls = %v, an edit that remembers nothing has nothing to compare", got)
	}
	wantSent(t, st, id)
}

func TestDrainFailsTheRowWhenTheReadIs404(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedEditable(t, st, "")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Importance: "High"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		return nil, &wrike.APIError{StatusCode: 404, Code: "resource_not_found", Description: "Task not found or access denied"}
	}}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := fc.callLog(); len(got) != 1 {
		t.Errorf("calls = %v, want the read alone", got)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil || row.State != store.StateFailed || !strings.Contains(row.LastError, "Task not found") {
		t.Errorf("row = %+v, %v, want failed with the read's error", row, err)
	}
}

func TestDrainCompletesCreateThenDependentEdit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", store.TimelogCreatePayload{
		Hours: 1, TrackedDate: "2026-09-03",
	})
	if err != nil {
		t.Fatal(err)
	}
	localID := store.LocalIDPrefix + strconv.FormatInt(createID, 10)
	if _, err := st.Outbox().EnqueueTimelogUpdate(ctx, localID, store.TimelogUpdatePayload{Hours: 2}); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{
		createTimelog: func(taskID string, hours float64, trackedDate, comment string) (wrike.Timelog, error) {
			return wrike.Timelog{ID: "L9", TaskID: taskID, UserID: "U1", TrackedDate: trackedDate, Hours: hours}, nil
		},
		updateTimelog: func(timelogID string, u wrike.TimelogUpdate) (wrike.Timelog, error) {
			return wrike.Timelog{ID: timelogID, TaskID: "T1", UserID: "U1", TrackedDate: "2026-09-03", Hours: u.Hours}, nil
		},
	}
	if _, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	log := fc.callLog()
	if len(log) != 2 || log[0] != "CreateTimelog T1" || log[1] != "UpdateTimelog L9" {
		t.Fatalf("calls = %v, the dependent edit must drain against the confirmed id", log)
	}
	logs, err := st.Timelogs().ListForTask(ctx, "T1")
	if err != nil || len(logs) != 1 || logs[0].ID != "L9" || logs[0].Hours != 2 {
		t.Fatalf("timelogs = %+v, %v", logs, err)
	}
}

func TestDrainDeleteAlreadyGoneCompletes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Timelogs().Upsert(ctx, []store.Timelog{{ID: "L1", TaskID: "T1", UserID: "U1",
		TrackedDate: "2026-09-01", Hours: 1,
		CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogDelete(ctx, "L1"); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{deleteTimelog: func(timelogID string) error {
		return &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}
	}}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v, a delete of a vanished timelog is success", changed, err)
	}
	pending, failed, cerr := st.Outbox().Counts(ctx)
	if cerr != nil || pending != 0 || failed != 0 {
		t.Fatalf("counts = %d, %d, %v", pending, failed, cerr)
	}
}

func TestDrainEmptyQueueIsQuiet(t *testing.T) {
	st := newTestStore(t)
	fc := &fakeClient{}
	changed, err := drainOutbox(context.Background(), context.Background(), fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || changed {
		t.Fatalf("drain = %v, %v, want a no-op", changed, err)
	}
	if len(fc.callLog()) != 0 {
		t.Errorf("calls = %v", fc.callLog())
	}
}

func TestDrainCorruptRowFailsAndContinues(t *testing.T) {
	// The path is needed to reach behind the store, so this test opens its own database.
	path := filepath.Join(t.TempDir(), "wrike.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	badID, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "will be corrupted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "fine"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `UPDATE outbox SET payload = 'not json' WHERE id = ?`, badID); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v, a corrupt row must not stop the drain", changed, err)
	}
	if got := fc.callLog(); len(got) != 1 || got[0] != "CreateComment T1" {
		t.Fatalf("calls = %v, want only the good row sent", got)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 1 {
		t.Fatalf("counts = %d, %d, %v, want the corrupt row failed and the rest drained", pending, failed, err)
	}
	rows := failedRows(t, st)
	if len(rows) != 1 || rows[0].ID != badID || rows[0].LastError == "" {
		t.Fatalf("failed rows = %+v", rows)
	}
}

func TestDrainAuthFailureLeavesRowDue(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")
	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "waiting for a token")
	if err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		return wrike.Comment{}, &wrike.APIError{StatusCode: 401, Code: "not_authorized"}
	}}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if classify(err) != failAuth || changed {
		t.Fatalf("drain = %v, %v, want the auth error back and nothing changed", changed, err)
	}
	// The row must be pending and due right now, with no backoff and no attempt counted against it.
	row, err := st.Outbox().NextDue(ctx, rfc3339(time.Now()))
	if err != nil || row.ID != id || row.Attempts != 0 || row.State != store.StatePending {
		t.Fatalf("row = %+v, %v, want it back in the queue untouched", row, err)
	}
	if row.NextAttemptAt != "" {
		t.Fatalf("next attempt = %q, want none", row.NextAttemptAt)
	}
}

func TestDrainCreatesATaskThenItsDependents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "New one", Responsibles: []string{"U1"}})
	if err != nil {
		t.Fatal(err)
	}
	localID := store.LocalID(createID)
	if _, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, localID, store.TaskUpdatePayload{Importance: "High"}); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{createTask: func(folderID string, tc wrike.TaskCreate) (wrike.Task, error) {
		if tc.Title != "New one" || len(tc.Responsibles) != 1 || tc.Responsibles[0] != "U1" {
			t.Errorf("create sent %+v, want the queued title and responsible", tc)
		}
		return wrike.Task{ID: "T9", Title: tc.Title, Status: "Active", CustomStatusID: "CS1", Importance: "Normal",
			ResponsibleIDs: tc.Responsibles, ParentIDs: []string{folderID}, Permalink: "https://www.wrike.com/open.htm?id=9",
			CreatedDate: time.Date(2026, 10, 1, 9, 0, 5, 0, time.UTC), UpdatedDate: time.Date(2026, 10, 1, 9, 0, 5, 0, time.UTC)}, nil
	}, updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{ID: taskID, Title: "New one", Importance: u.Importance}, nil
	}}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}

	log := fc.callLog()
	if len(log) != 3 || log[0] != "CreateTask F1" || log[1] != "CreateComment T9" || log[2] != "UpdateTask T9" {
		t.Fatalf("calls = %v, want the create first and the dependents against the confirmed id", log)
	}
	if _, err := st.Tasks().Get(ctx, localID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("local task after the drain: %v, want gone", err)
	}
	task, err := st.Tasks().Get(ctx, "T9")
	if err != nil || task.Importance != "High" {
		t.Errorf("task = %+v, %v, want the server copy with the dependent edit applied", task, err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Errorf("counts = %d, %d, %v", pending, failed, err)
	}
}

func TestDrainRejectedCreateKeepsTheLocalTaskAndItsDependents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "New one"})
	if err != nil {
		t.Fatal(err)
	}
	localID := store.LocalID(createID)
	if _, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "first"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{createTask: func(folderID string, tc wrike.TaskCreate) (wrike.Task, error) {
		return wrike.Task{}, &wrike.APIError{StatusCode: 403, Code: "access_forbidden", Description: "no"}
	}}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 1 || failed != 1 {
		t.Errorf("counts = %d, %d, %v, want the create failed and the comment still waiting", pending, failed, err)
	}
	if _, err := st.Tasks().Get(ctx, localID); err != nil {
		t.Errorf("local task after the rejection: %v, want kept for the issues screen", err)
	}
	if log := fc.callLog(); len(log) != 1 {
		t.Errorf("calls = %v, the dependent must not be sent", log)
	}
}

func TestDrainCorruptCreateFailsAndContinues(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	// The box never queues an empty title, only a damaged row has one, and the client would refuse it with a plain error.
	if _, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "fine"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}
	if log := fc.callLog(); len(log) != 1 || log[0] != "CreateComment T1" {
		t.Errorf("calls = %v, the damaged create must fail without a request and the next row must drain", log)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 1 {
		t.Errorf("counts = %d, %d, %v, want the create failed for good", pending, failed, err)
	}
}

// The drain stops at the first write Wrike could not take, so the error has to say which row that was:
// a command that queued a later row would otherwise print another task's failure as its own.
func TestDrainNamesTheRowItStoppedAt(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "one")
	seedTask(t, st, "T2", "two")
	first, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Outbox().EnqueueTaskUpdate(ctx, "T2", store.TaskUpdatePayload{Title: "b"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{}, &wrike.APIError{StatusCode: 503, Method: "PUT", Path: "/tasks/" + taskID}
	}}
	_, err = drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	var se *SendError
	if !errors.As(err, &se) || se.Row.ID != first || se.Row.EntityID != "T1" {
		t.Fatalf("drain error = %#v, want a SendError for row %d of T1", err, first)
	}
	var apiErr *wrike.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 {
		t.Errorf("the cause should stay reachable through the error: %v", err)
	}
	row, err := st.Outbox().Get(ctx, second)
	if err != nil || row.State != store.StatePending || row.Attempts != 0 {
		t.Errorf("second row = %+v, %v, want untouched and pending", row, err)
	}
}

// A row in backoff is skipped by the due query, and a later row on the same task must not go out ahead of it,
// Wrike would hold the older change in the end.
// A row on another task still goes through.
func TestDrainKeepsTheOrderOnATaskAcrossPasses(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")
	seedTask(t, st, "T2", "b")
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "X"}); err != nil {
		t.Fatal(err)
	}
	down := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{}, &wrike.APIError{StatusCode: 500}
	}}
	if _, err := drainOutbox(ctx, ctx, down, st, time.Hour, time.Hour); err == nil {
		t.Fatal("want the transient error back")
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Y"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T2", "U1", "unrelated"); err != nil {
		t.Fatal(err)
	}
	up := &fakeClient{
		updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
			return wrike.Task{ID: taskID, Title: u.Title}, nil
		},
		createComment: func(taskID, text string) (wrike.Comment, error) {
			return wrike.Comment{ID: "C1", TaskID: taskID, Text: text}, nil
		},
	}
	if _, err := drainOutbox(ctx, ctx, up, st, time.Hour, time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := up.callLog(); len(got) != 1 || got[0] != "CreateComment T2" {
		t.Errorf("calls = %v, want the unrelated comment alone, Y waits for X", got)
	}
	pending, _, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 2 {
		t.Errorf("pending = %d, %v, want X and Y still queued in order", pending, err)
	}
}

func TestDrainSendsTheDescriptionAndTheImportanceOfACreate(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "Described", Description: "<p>Why</p>", Importance: "High"}); err != nil {
		t.Fatal(err)
	}
	var got wrike.TaskCreate
	fc := &fakeClient{createTask: func(folderID string, tc wrike.TaskCreate) (wrike.Task, error) {
		got = tc
		return wrike.Task{ID: "TNEW", Title: tc.Title, Status: "Active", Importance: tc.Importance, Description: tc.Description, ParentIDs: []string{folderID}}, nil
	}}
	if _, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got.Description != "<p>Why</p>" || got.Importance != "High" {
		t.Errorf("create sent = %+v, want the description and the importance", got)
	}
}

func TestDrainSendsABudgetEditAndAsksForTheEffortBack(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := store.Task{ID: "T1", Title: "t", Status: "Active", CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z", Effort: &store.TaskEffort{Mode: "Basic", TotalMinutes: 600}}
	if err := st.Tasks().Upsert(ctx, []store.Task{task}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{CustomFields: []store.CustomFieldValue{{ID: "CF1", Value: "765"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Effort: &store.TaskEffort{Mode: "Basic", TotalMinutes: 1200}}); err != nil {
		t.Fatal(err)
	}
	var sent []wrike.TaskUpdate
	// The fake keeps the task's values the way Wrike does, every answer carries them like a read by id does.
	values := []wrike.CustomFieldValue{}
	fc := &fakeClient{
		tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
			return []wrike.Task{{ID: "T1", Title: "t", Status: "Active", CustomFields: values, Effort: &wrike.TaskEffort{Mode: "Basic", TotalEffort: 600}}}, nil
		},
		updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
			sent = append(sent, u)
			for _, cf := range u.CustomFields {
				values = append(values, wrike.CustomFieldValue{ID: cf.ID, Value: cf.Value})
			}
			// The effort comes back only when the write asked for it.
			answer := wrike.Task{ID: "T1", Title: "t", Status: "Active", CustomFields: values}
			if u.Effort != nil {
				answer.Effort = &wrike.TaskEffort{Mode: "Basic", TotalEffort: u.Effort.TotalEffort}
			}
			return answer, nil
		},
	}
	if _, err := drainOutbox(ctx, ctx, fc, st, time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || len(sent[0].CustomFields) != 1 || sent[0].CustomFields[0].Value != "765" || sent[0].Effort != nil || sent[0].Fields != nil {
		t.Errorf("first send = %+v", sent)
	}
	if len(sent) == 2 && (sent[1].Effort == nil || sent[1].Effort.TotalEffort != 1200 || !slices.Equal(sent[1].Fields, []string{"effortAllocation"})) {
		t.Errorf("second send = %+v", sent[1])
	}
	got, _ := st.Tasks().Get(ctx, "T1")
	if got.Effort == nil || got.Effort.TotalMinutes != 1200 {
		t.Errorf("the answer's effort was not applied: %+v", got.Effort)
	}
	if len(got.CustomFields) != 1 || got.CustomFields[0].Value != "765" {
		t.Errorf("the value the first edit set is gone after the second edit's answer: %+v", got.CustomFields)
	}
}
