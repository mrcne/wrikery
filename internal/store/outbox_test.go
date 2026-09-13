package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The queued time and the optimistic rows' dates come from the store's clock rather than SQLite's,
// so the sync issues screen agrees with the app's own clock, and a golden that shows a queued row does not change with the calendar.
func TestEnqueueStampsRowsFromTheStoreClock(t *testing.T) {
	st := newTestStore(t)
	st.Now = func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "task")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello"); err != nil {
		t.Fatal(err)
	}
	row, err := st.Outbox().NextDue(ctx, "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.CreatedAt != "2026-09-03T12:00:00Z" {
		t.Errorf("outbox created_at = %q, want the store clock", row.CreatedAt)
	}
	comments, _ := st.Comments().ListForTask(ctx, "T1")
	if len(comments) != 1 || comments[0].CreatedDate != "2026-09-03T12:00:00Z" {
		t.Errorf("optimistic comment = %+v, want created at the store clock", comments)
	}
}

func TestEnqueueTaskUpdateAppliesOptimistically(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	task := makeTask("T1", "before")
	task.ResponsibleIDs = []string{"U1"}
	task.ParentIDs = []string{"F1", "F2"}
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}

	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{
		Title:              "after",
		Description:        "<p>after <b>all</b></p>",
		CustomStatusID:     "CS2",
		AddResponsibles:    []string{"U2"},
		RemoveResponsibles: []string{"U1"},
		Importance:         "High",
		AddParents:         []string{"F3"},
		RemoveParents:      []string{"F1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("outbox id = 0")
	}

	got, err := st.Tasks().Get(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "after" || got.CustomStatusID != "CS2" || got.Importance != "High" {
		t.Errorf("task = %+v, optimistic apply missing", got)
	}
	if got.Description != "<p>after <b>all</b></p>" || got.DescriptionPlain != "after all" {
		t.Errorf("description = %q, plain = %q, want the queued HTML and its text", got.Description, got.DescriptionPlain)
	}
	if strings.Join(got.ParentIDs, ",") != "F2,F3" {
		t.Errorf("parents = %v, want F1 gone and F3 added", got.ParentIDs)
	}
	if len(got.ResponsibleIDs) != 1 || got.ResponsibleIDs[0] != "U2" {
		t.Errorf("responsibles = %v, want [U2]", got.ResponsibleIDs)
	}
	// The title change must reach the search index through the trigger.
	if hits, err := st.Tasks().Search(ctx, "after", 10); err != nil || len(hits) != 1 {
		t.Errorf("search after optimistic rename = %v, %v", hits, err)
	}

	var payload string
	if err := st.reader.QueryRow(
		`SELECT payload FROM outbox WHERE id = ?`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p TaskUpdatePayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("payload does not decode: %v", err)
	}
	if p.Title != "after" || len(p.AddResponsibles) != 1 {
		t.Errorf("decoded payload = %+v", p)
	}
}

// The group rides along with the status id, so the list treats the task as done immediately,
// not waiting on the server round trip that derives the group from the status.
func TestEnqueueTaskUpdateAppliesStatusGroup(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a")}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{CustomStatusID: "S9", Status: "Completed"}); err != nil {
		t.Fatal(err)
	}
	task, err := st.Tasks().Get(ctx, "T1")
	if err != nil || task.Status != "Completed" || task.CustomStatusID != "S9" {
		t.Errorf("task after enqueue = %+v, %v", task, err)
	}
}

// A Backlog update clears start and due the same way the task upsert path does, or an empty
// string in the column would sort the task ahead of every task with a real due date.
func TestEnqueueTaskUpdateBacklogClearsDates(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "a")
	task.Dates = &TaskDates{Type: "Planned", Start: "2026-09-01", Due: "2026-09-02"}
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{Dates: &TaskDates{Type: "Backlog"}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().Get(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dates == nil || got.Dates.Type != "Backlog" || got.Dates.Start != "" || got.Dates.Due != "" {
		t.Errorf("task after backlog update = %+v, want empty start and due", got.Dates)
	}
	// sql.NullString.String reads back "" whether the column is NULL or the literal empty
	// string, so the read above cannot tell the two apart, only a direct NULL check can.
	var startNull, dueNull bool
	if err := st.reader.QueryRow(
		`SELECT dates_start IS NULL, dates_due IS NULL FROM tasks WHERE id = ?`, "T1",
	).Scan(&startNull, &dueNull); err != nil {
		t.Fatal(err)
	}
	if !startNull || !dueNull {
		t.Errorf("dates_start/dates_due not NULL after backlog update")
	}
}

func TestEnqueueCommentCreatesLocalRow(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a")}); err != nil {
		t.Fatal(err)
	}

	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello from offline")
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("comments = %+v, want the optimistic row", got)
	}
	want := "local:" + strconv.FormatInt(id, 10)
	if got[0].ID != want || got[0].AuthorID != "U1" || got[0].Text != "hello from offline" {
		t.Errorf("comment = %+v, want id %s", got[0], want)
	}
	if got[0].CreatedDate == "" {
		t.Error("created date empty")
	}
}

func TestEnqueueTimelogLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	id, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", TimelogCreatePayload{
		Hours: 2.5, TrackedDate: "2026-09-03", Comment: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	localID := "local:" + strconv.FormatInt(id, 10)

	logs, err := st.Timelogs().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != localID || logs[0].Hours != 2.5 || logs[0].UserID != "U1" {
		t.Fatalf("timelogs = %+v", logs)
	}

	if _, err := st.Outbox().EnqueueTimelogUpdate(ctx, localID, TimelogUpdatePayload{Hours: 3}); err != nil {
		t.Fatal(err)
	}
	logs, err = st.Timelogs().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if logs[0].Hours != 3 || logs[0].TrackedDate != "2026-09-03" {
		t.Errorf("after update = %+v, want hours 3 and date kept", logs[0])
	}

	if _, err := st.Outbox().EnqueueTimelogDelete(ctx, localID); err != nil {
		t.Fatal(err)
	}
	logs, err = st.Timelogs().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Errorf("after delete = %+v, want empty", logs)
	}

	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 3 || failed != 0 {
		t.Errorf("counts = %d pending %d failed, want 3 and 0", pending, failed)
	}
}

func TestStatesByEntityFailedWins(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T2", "b")}); err != nil {
		t.Fatal(err)
	}
	id1, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T2", TaskUpdatePayload{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, id1, "rejected"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Outbox().StatesByEntity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["T1"] != StateFailed || got["T2"] != StatePending || len(got) != 2 {
		t.Errorf("states = %v", got)
	}
}

func TestCountsSeparatesFailed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "x"); err == nil {
		t.Fatal("comment for uncached task must fail its foreign key")
	}
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a")}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.writer.Exec(
		`UPDATE outbox SET state = 'failed' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 || failed != 1 {
		t.Errorf("counts = %d pending %d failed, want 0 and 1", pending, failed)
	}
}
