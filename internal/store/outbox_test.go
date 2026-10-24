package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The queued time and the optimistic rows' dates come from the store's clock rather than SQLite's,
// so the changes screen agrees with the app's own clock, and a golden that shows a queued row does not change with the calendar.
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
	comments, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
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

func TestEnqueueTaskCreateWritesALocalTask(t *testing.T) {
	st := newTestStore(t)
	st.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	if err := st.Workflows().ReplaceAll(ctx, []Workflow{{ID: "W1", Name: "Default", Standard: true,
		CustomStatuses: []CustomStatus{{ID: "CS1", Name: "New", Group: "Active"}}}}); err != nil {
		t.Fatal(err)
	}

	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one", Responsibles: []string{"U1"}})
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(id)
	got, err := st.Tasks().Get(ctx, localID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New one" || got.Status != "Active" || got.CustomStatusID != "CS1" || got.Importance != "Normal" {
		t.Errorf("local task = %+v", got)
	}
	if strings.Join(got.ParentIDs, ",") != "F1" || strings.Join(got.ResponsibleIDs, ",") != "U1" {
		t.Errorf("parents = %v, responsibles = %v, want the folder and the user", got.ParentIDs, got.ResponsibleIDs)
	}
	if got.CreatedDate != "2026-10-01T09:00:00Z" {
		t.Errorf("created = %q, want the store clock", got.CreatedDate)
	}
	rows, err := st.Tasks().ListInFolder(ctx, "F1")
	if err != nil || len(rows) != 1 || rows[0].ID != localID {
		t.Errorf("list in F1 = %+v, %v, want the local task", rows, err)
	}
	row, err := st.Outbox().NextDue(ctx, "2030-01-01T00:00:00Z")
	if err != nil || row.Kind != KindTaskCreate || row.EntityID != "F1" {
		t.Errorf("next due = %+v, %v, want the create keyed by its folder", row, err)
	}
	var p TaskCreatePayload
	if err := json.Unmarshal(row.Payload, &p); err != nil || p.Title != "New one" || strings.Join(p.Responsibles, ",") != "U1" {
		t.Errorf("payload = %s, %v", row.Payload, err)
	}
	states, err := st.Outbox().StatesByEntity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states[localID] != StatePending || states["F1"] != "" {
		t.Errorf("states = %v, want the local task pending and the folder untouched", states)
	}
}

// The status of the local row is the first visible Active status of the workflow the folder's space gives new tasks.
// Wrike's Space object names that workflow in defaultTaskWorkflowId, and on the live account it was the standard one
// even in a space that owns a workflow of its own, so the guess has to read the field rather than the view.
func TestEnqueueTaskCreateGuessesTheStatusFromTheSpaceWorkflow(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Workflows().ReplaceAll(ctx, []Workflow{
		{ID: "W1", Name: "Default", Standard: true, CustomStatuses: []CustomStatus{
			{ID: "CS1", Name: "New", Group: "Active"}}},
		{ID: "W2", Name: "Space flow", CustomStatuses: []CustomStatus{
			{ID: "CS20", Name: "Backlog", Group: "Active", Hidden: true},
			{ID: "CS21", Name: "Done", Group: "Completed"},
			{ID: "CS22", Name: "Todo", Group: "Active"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Spaces().ReplaceAll(ctx, []Space{
		{ID: "S1", Title: "Dev", DefaultTaskWorkflowID: "W2"},
		{ID: "S2", Title: "Ops", DefaultTaskWorkflowID: "W404"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Folders().ReplaceTree(ctx, []Folder{
		{ID: "S1", Title: "Dev", Space: true, ChildIDs: []string{"F1"}},
		{ID: "F1", Title: "Api", ChildIDs: []string{"F2"}},
		{ID: "F2", Title: "Deep"},
		{ID: "S2", Title: "Ops", Space: true, ChildIDs: []string{"F3"}},
		{ID: "F3", Title: "Alerts"},
		{ID: "F9", Title: "Loose"},
	}); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, folder, want string }{
		{"a folder two levels under the space", "F2", "CS22"},
		{"the space root itself", "S1", "CS22"},
		{"a space whose workflow the cache lacks", "F3", "CS1"},
		{"a folder under no cached space", "F9", "CS1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, err := st.Outbox().EnqueueTaskCreate(ctx, c.folder, TaskCreatePayload{Title: "New one"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := st.Tasks().Get(ctx, LocalID(id))
			if err != nil {
				t.Fatal(err)
			}
			if got.CustomStatusID != c.want {
				t.Errorf("status = %q, want %q", got.CustomStatusID, c.want)
			}
		})
	}
}

func TestOutboxGetReadsOneRowAndKeepsADiscardedOne(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "One")}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != id || row.Kind != KindTaskUpdate || row.EntityID != "T1" || row.State != StatePending {
		t.Errorf("row = %+v", row)
	}
	if err := st.Outbox().Discard(ctx, id); err != nil {
		t.Fatal(err)
	}
	if row, err := st.Outbox().Get(ctx, id); err != nil || row.State != StateDiscarded {
		t.Errorf("after discard = %+v, %v, want the row kept as discarded", row, err)
	}
}

// readRow is the outbox row by id, the test fails when it is gone.
func readRow(t *testing.T, st *Store, id int64) OutboxRow {
	t.Helper()
	row, err := st.Outbox().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("outbox row %d: %v", id, err)
	}
	return row
}

func decodeTaskPayload(t *testing.T, raw []byte) TaskUpdatePayload {
	t.Helper()
	var p TaskUpdatePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("payload %q does not decode: %v", raw, err)
	}
	return p
}

// The enqueue is the last moment the cache still has the values a write replaces, so it keeps them on the row,
// as the payload that would put them back.
func TestEnqueueTaskUpdateKeepsTheValuesItReplaces(t *testing.T) {
	st := newTestStore(t)
	st.Source = SourceCLI
	ctx := context.Background()
	task := makeTask("T1", "before")
	task.CustomStatusID, task.Importance = "CS1", "Normal"
	task.Dates = &TaskDates{Type: "Planned", Duration: 480, Start: "2026-10-01", Due: "2026-10-03"}
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{
		Title: "after", Description: "<p>new</p>", CustomStatusID: "CS2", Status: "Completed", Importance: "High",
		Dates: &TaskDates{Type: "Planned", Start: "2026-10-05", Due: "2026-10-06"},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := readRow(t, st, id)
	if row.TaskID != "T1" || row.TaskTitle != "before" || row.Source != SourceCLI {
		t.Errorf("row = task %q title %q source %q, want T1, before, cli", row.TaskID, row.TaskTitle, row.Source)
	}
	b := decodeTaskPayload(t, row.Before)
	if b.Title != "before" || b.Description != "<p>body of before</p>" || b.CustomStatusID != "CS1" || b.Status != "Active" || b.Importance != "Normal" {
		t.Errorf("before = %+v, want the old title, description, status with its group and importance", b)
	}
	if b.Dates == nil || *b.Dates != (TaskDates{Type: "Planned", Duration: 480, Start: "2026-10-01", Due: "2026-10-03"}) {
		t.Errorf("before dates = %+v, want the old block", b.Dates)
	}
}

// A write that only sets some fields keeps only those, the before must not carry a title the write did not touch,
// or a revert of a status move would put an old title back too.
func TestEnqueueTaskUpdateKeepsOnlyTheFieldsTheWriteSets(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "keep me")
	task.CustomStatusID = "CS1"
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{CustomStatusID: "CS2", Status: "Completed"})
	if err != nil {
		t.Fatal(err)
	}
	b := decodeTaskPayload(t, readRow(t, st, id).Before)
	if b.CustomStatusID != "CS1" || b.Status != "Active" || b.Title != "" || b.Description != "" || b.Importance != "" || b.Dates != nil {
		t.Errorf("before = %+v, want the status alone", b)
	}
}

// A first date on a task without a dates block has Backlog as its before, the write that clears dates again.
func TestEnqueueTaskUpdateKeepsBacklogAsTheBeforeOfAFirstDate(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "undated")}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{Dates: &TaskDates{Type: "Planned", Due: "2026-10-06"}})
	if err != nil {
		t.Fatal(err)
	}
	b := decodeTaskPayload(t, readRow(t, st, id).Before)
	if b.Dates == nil || b.Dates.Type != "Backlog" || b.Dates.Start != "" || b.Dates.Due != "" {
		t.Errorf("before dates = %+v, want a Backlog block", b.Dates)
	}
}

// The dialogs compute add and remove from the cached state, so the before is the mirror: what was added is removed and back.
func TestEnqueueTaskUpdateMirrorsAssigneesAndFolders(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "a")
	task.ResponsibleIDs, task.ParentIDs = []string{"U1"}, []string{"F1"}
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{
		AddResponsibles: []string{"U2"}, RemoveResponsibles: []string{"U1"}, AddParents: []string{"F2"}, RemoveParents: []string{"F1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := decodeTaskPayload(t, readRow(t, st, id).Before)
	if strings.Join(b.AddResponsibles, ",") != "U1" || strings.Join(b.RemoveResponsibles, ",") != "U2" ||
		strings.Join(b.AddParents, ",") != "F1" || strings.Join(b.RemoveParents, ",") != "F2" {
		t.Errorf("before = %+v, want the add and remove lists swapped", b)
	}
}

// A write on a task the cache does not have is still queued, with nothing to remember.
func TestEnqueueOnAGhostTaskKeepsNoBefore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "GHOST", TaskUpdatePayload{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	row := readRow(t, st, id)
	if len(row.Before) != 0 || row.TaskID != "GHOST" || row.TaskTitle != "" {
		t.Errorf("row = before %q task %q title %q, want an empty before, the id and no title", row.Before, row.TaskID, row.TaskTitle)
	}
}

// The task a create made has a local row whose status is a guess and whose description is this client's own HTML,
// so an edit queued on it before the create lands remembers nothing, the drain then has nothing to hold it against.
func TestEnqueueOnALocalTaskKeepsNoBefore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, LocalID(createID), TaskUpdatePayload{CustomStatusID: "CS2", Status: "Completed"})
	if err != nil {
		t.Fatal(err)
	}
	row := readRow(t, st, id)
	if len(row.Before) != 0 || row.TaskID != LocalID(createID) || row.TaskTitle != "New one" {
		t.Errorf("row = before %q task %q title %q, want no before, the local id and the title", row.Before, row.TaskID, row.TaskTitle)
	}
}

// queueStatusEdits caches T1 in New with the title "a" and queues three edits on it:
// status In Progress, then Completed with the title "b", then On Hold, each remembering the cache the one before left.
func queueStatusEdits(t *testing.T, st *Store) (first, second, third int64) {
	t.Helper()
	ctx := context.Background()
	task := makeTask("T1", "a")
	task.CustomStatusID = "ST_NEW"
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 3)
	for i, p := range []TaskUpdatePayload{
		{CustomStatusID: "ST_PROG", Status: "Active"},
		{CustomStatusID: "ST_DONE", Status: "Completed", Title: "b"},
		{CustomStatusID: "ST_HOLD", Status: "Deferred"},
	} {
		id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", p)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	return ids[0], ids[1], ids[2]
}

// A dropped edit never reached Wrike, so the next edit of the same field must remember what the dropped one replaced,
// or the check before the send would hold it against a value Wrike never had.
func TestDiscardAndFailPassTheBeforeOnToTheNextEdit(t *testing.T) {
	for name, drop := range map[string]func(OutboxRepo, int64) error{
		"discard": func(o OutboxRepo, id int64) error { return o.Discard(context.Background(), id) },
		"fail": func(o OutboxRepo, id int64) error {
			return o.Fail(context.Background(), id, "wrike: invalid_parameter (http 400)")
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := newTestStore(t)
			first, second, third := queueStatusEdits(t, st)
			if err := drop(st.Outbox(), first); err != nil {
				t.Fatal(err)
			}
			got := decodeTaskPayload(t, readRow(t, st, second).Before)
			if got.CustomStatusID != "ST_NEW" || got.Status != "Active" || got.Title != "a" {
				t.Errorf("second before = %+v, want the status the dropped edit replaced and its own title", got)
			}
			if got := decodeTaskPayload(t, readRow(t, st, third).Before); got.CustomStatusID != "ST_DONE" {
				t.Errorf("third before = %+v, want the second edit's status, only the next edit of a field takes it", got)
			}
		})
	}
}

// A held edit remembers Wrike's newer value, passing that on would let the next edit overwrite it without a hold.
func TestDiscardOfAHeldEditPassesNothingOn(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	first, second, _ := queueStatusEdits(t, st)
	if err := st.Outbox().Hold(ctx, first, HeldPrefix+`status "Completed" now, was "New"`, []byte(`{"customStatusId":"ST_DONE","status":"Completed"}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Discard(ctx, first); err != nil {
		t.Fatal(err)
	}
	if got := decodeTaskPayload(t, readRow(t, st, second).Before); got.CustomStatusID != "ST_PROG" {
		t.Errorf("second before = %+v, want it untouched", got)
	}
}

func TestHoldFailsTheRowAndRewritesTheBefore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "task")
	task.CustomStatusID = "ST_NEW"
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{CustomStatusID: "ST_PROG", Status: "Active"})
	if err != nil {
		t.Fatal(err)
	}
	const text = "changed on Wrike since you set it: status"
	if err := st.Outbox().Hold(ctx, id, text, []byte(`{"customStatusId":"ST_DONE"}`)); err != nil {
		t.Fatal(err)
	}
	row := readRow(t, st, id)
	if row.State != StateFailed || row.LastError != text || row.Attempts != 1 || string(row.Before) != `{"customStatusId":"ST_DONE"}` {
		t.Errorf("row = state %s error %q attempts %d before %s, want failed with the text, one attempt and the new before",
			row.State, row.LastError, row.Attempts, row.Before)
	}
	// A retried row that is held again is still in the queue.
	if err := st.Outbox().Hold(ctx, id, text, []byte(`{"customStatusId":"ST_DONE"}`)); err != nil {
		t.Errorf("a second hold of a failed row: %v", err)
	}
	if err := st.Outbox().Discard(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Hold(ctx, id, text, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("hold of a discarded row = %v, want ErrNotFound", err)
	}
}

func TestDeferKeepsTheRowDueAndCountsNoAttempt(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "task")}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Defer(ctx, id, "dial tcp: connection refused"); err != nil {
		t.Fatal(err)
	}
	row := readRow(t, st, id)
	if row.State != StatePending || row.Attempts != 0 || row.LastError != "dial tcp: connection refused" || row.NextAttemptAt != "" {
		t.Errorf("row = state %s attempts %d error %q next %q, want pending, no attempt, the text and no wait",
			row.State, row.Attempts, row.LastError, row.NextAttemptAt)
	}
	due, err := st.Outbox().NextDue(ctx, st.Now().UTC().Format(time.RFC3339))
	if err != nil || due.ID != id {
		t.Errorf("next due = %d, %v, want the deferred row at once", due.ID, err)
	}
}

func TestEnqueueTaskCreateNamesItsLocalTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"})
	if err != nil {
		t.Fatal(err)
	}
	row := readRow(t, st, id)
	if row.TaskID != LocalID(id) || row.TaskTitle != "New one" || len(row.Before) != 0 {
		t.Errorf("row = task %q title %q before %q, want the local id, the title and no before", row.TaskID, row.TaskTitle, row.Before)
	}
}

func TestEnqueueCommentAndTimelogCreateKeepTheTaskTitle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "the task")}); err != nil {
		t.Fatal(err)
	}
	cid, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	lid, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", TimelogCreatePayload{Hours: 1, TrackedDate: "2026-10-01"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{cid, lid} {
		row := readRow(t, st, id)
		if row.TaskID != "T1" || row.TaskTitle != "the task" || len(row.Before) != 0 {
			t.Errorf("row %d = task %q title %q before %q", id, row.TaskID, row.TaskTitle, row.Before)
		}
	}
}

// An edit keeps all three old values, the payload's fields are all omitempty and a before limited to the set ones could not say "the note was empty".
// A delete keeps the whole entry as a create payload, which is what puts it back.
// Both name the entry's task, which the delete removes from the cache in the same transaction.
func TestEnqueueTimelogUpdateAndDeleteKeepTheOldEntryAndItsTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "the task")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Timelogs().Upsert(ctx, []Timelog{{ID: "L1", TaskID: "T1", UserID: "U1", TrackedDate: "2026-10-01", Hours: 1, Comment: "old note",
		CreatedDate: "2026-10-01T10:00:00Z", UpdatedDate: "2026-10-01T10:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	uid, err := st.Outbox().EnqueueTimelogUpdate(ctx, "L1", TimelogUpdatePayload{Hours: 2})
	if err != nil {
		t.Fatal(err)
	}
	row := readRow(t, st, uid)
	var old TimelogUpdatePayload
	if err := json.Unmarshal(row.Before, &old); err != nil {
		t.Fatal(err)
	}
	if row.TaskID != "T1" || row.TaskTitle != "the task" || old.Hours != 1 || old.TrackedDate != "2026-10-01" || old.Comment != "old note" {
		t.Errorf("update row = task %q title %q before %+v", row.TaskID, row.TaskTitle, old)
	}
	did, err := st.Outbox().EnqueueTimelogDelete(ctx, "L1")
	if err != nil {
		t.Fatal(err)
	}
	row = readRow(t, st, did)
	var gone TimelogCreatePayload
	if err := json.Unmarshal(row.Before, &gone); err != nil {
		t.Fatal(err)
	}
	// The edit above already moved the cached entry to two hours, the delete remembers the entry as it stood.
	if row.TaskID != "T1" || gone.Hours != 2 || gone.TrackedDate != "2026-10-01" || gone.Comment != "old note" {
		t.Errorf("delete row = task %q before %+v", row.TaskID, gone)
	}
	gid, err := st.Outbox().EnqueueTimelogDelete(ctx, "NOSUCH")
	if err != nil {
		t.Fatal(err)
	}
	if row := readRow(t, st, gid); row.TaskID != "" || len(row.Before) != 0 {
		t.Errorf("delete of an unknown entry = task %q before %q, want nothing", row.TaskID, row.Before)
	}
}

// The mirror comes from the link tables and not from the payload alone: an id the dialog adds that a pull put on the task
// meanwhile changes nothing on Wrike, so putting it back must not remove it, and the same for a remove of an id already gone.
func TestEnqueueTaskUpdateMirrorsOnlyWhatReallyChanges(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "a")
	task.ResponsibleIDs, task.ParentIDs = []string{"U1", "U2"}, []string{"F1", "F2"}
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{
		AddResponsibles: []string{"U1", "U3"}, RemoveResponsibles: []string{"U2", "U9"},
		AddParents: []string{"F1", "F3"}, RemoveParents: []string{"F2", "F9"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := decodeTaskPayload(t, readRow(t, st, id).Before)
	if strings.Join(b.RemoveResponsibles, ",") != "U3" || strings.Join(b.AddResponsibles, ",") != "U2" ||
		strings.Join(b.RemoveParents, ",") != "F3" || strings.Join(b.AddParents, ",") != "F2" {
		t.Errorf("before = %+v, want only U3 and F3 to remove and only U2 and F2 to add back", b)
	}
}

func TestEnqueueTaskCreateCarriesTheDescriptionAndTheImportance(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Workflows().ReplaceAll(ctx, []Workflow{{ID: "W1", Name: "Default", Standard: true,
		CustomStatuses: []CustomStatus{{ID: "CS1", Name: "New", Group: "Active"}}}}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "Described", Description: "<p>Why <b>now</b></p>", Importance: "High"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().Get(ctx, LocalID(id))
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "<p>Why <b>now</b></p>" || got.DescriptionPlain != "Why now" || got.Importance != "High" {
		t.Errorf("local task = %q %q %q, want the description, its plain text and the importance", got.Description, got.DescriptionPlain, got.Importance)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var p TaskCreatePayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Description != "<p>Why <b>now</b></p>" || p.Importance != "High" {
		t.Errorf("payload = %+v, want both fields for the drain", p)
	}
}

// A row whose payload does not decode is the one the drain fails so it cannot block the queue,
// and x on the changes screen must still drop it: the hand-on of its old values skips what it cannot read.
func TestFailAndDiscardTakeAnEditThatDoesNotDecode(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	first, second, third := queueStatusEdits(t, st)
	if _, err := st.writer.ExecContext(ctx, `UPDATE outbox SET payload = 'not json' WHERE id IN (?, ?)`, first, third); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, first, "sync: corrupt outbox row"); err != nil {
		t.Fatalf("fail of a corrupt row = %v", err)
	}
	if row := readRow(t, st, first); row.State != StateFailed {
		t.Errorf("corrupt row = %s, want failed", row.State)
	}
	// The second edit's own hand-on meets the corrupt third edit after it.
	if err := st.Outbox().Discard(ctx, second); err != nil {
		t.Fatalf("discard before a corrupt later edit = %v", err)
	}
	if err := st.Outbox().Discard(ctx, first); err != nil {
		t.Fatalf("discard of the corrupt row = %v", err)
	}
}
