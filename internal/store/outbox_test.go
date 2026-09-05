package store

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
)

func TestEnqueueTaskUpdateAppliesOptimistically(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	task := makeTask("T1", "before")
	task.ResponsibleIDs = []string{"U1"}
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}

	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{
		Title:              "after",
		CustomStatusID:     "CS2",
		AddResponsibles:    []string{"U2"},
		RemoveResponsibles: []string{"U1"},
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
	if got.Title != "after" || got.CustomStatusID != "CS2" {
		t.Errorf("task = %+v, optimistic apply missing", got)
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
