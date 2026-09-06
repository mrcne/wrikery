package store

import (
	"context"
	"errors"
	"testing"
)

func TestCommentReplaceKeepsLocalRows(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a")}); err != nil {
		t.Fatal(err)
	}

	server := []Comment{
		{ID: "C1", TaskID: "T1", AuthorID: "U1", Text: "old", CreatedDate: "2026-09-01T10:00:00Z"},
		{ID: "C2", TaskID: "T1", AuthorID: "U2", Text: "stale", CreatedDate: "2026-09-01T11:00:00Z"},
	}
	if err := st.Comments().ReplaceForTask(ctx, "T1", server); err != nil {
		t.Fatal(err)
	}
	// An optimistic row a queued write created, not yet drained.
	if _, err := st.writer.Exec(`
		INSERT INTO comments (id, task_id, author_id, text, created_date)
		VALUES ('local:7', 'T1', 'U1', 'queued offline', '2026-09-03T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	// The next refresh no longer contains C2 but must keep local:7.
	if err := st.Comments().ReplaceForTask(ctx, "T1", server[:1]); err != nil {
		t.Fatal(err)
	}
	got, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "C1" || got[1].ID != "local:7" {
		t.Fatalf("comments = %+v, want [C1 local:7] by created date", got)
	}
}

func TestTimelogUpsertAndReplace(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	logs := []Timelog{
		{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: "2026-09-01", Hours: 2,
			CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z"},
		{ID: "L2", TaskID: "T9", UserID: "U1", TrackedDate: "2026-09-02", Hours: 1,
			CreatedDate: "2026-09-02T10:00:00Z", UpdatedDate: "2026-09-02T10:00:00Z"},
	}
	// No task row for T9 exists, timelogs must not require one.
	if err := st.Timelogs().Upsert(ctx, logs); err != nil {
		t.Fatal(err)
	}
	logs[0].Hours = 3
	logs[0].LockStatus = "Locked"
	if err := st.Timelogs().Upsert(ctx, logs[:1]); err != nil {
		t.Fatal(err)
	}
	got, err := st.Timelogs().ListForTask(ctx, "T9")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Hours != 3 || got[0].LockStatus != "Locked" {
		t.Fatalf("timelogs = %+v", got)
	}

	if err := st.Timelogs().ReplaceForTask(ctx, "T9", logs[:1]); err != nil {
		t.Fatal(err)
	}
	got, err = st.Timelogs().ListForTask(ctx, "T9")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "L1" {
		t.Fatalf("timelogs after replace = %+v, want [L1]", got)
	}
}

func TestTimelogGet(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	log := Timelog{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: "2026-09-01", Hours: 2,
		CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z"}
	if err := st.Timelogs().Upsert(ctx, []Timelog{log}); err != nil {
		t.Fatal(err)
	}

	got, err := st.Timelogs().Get(ctx, "L1")
	if err != nil {
		t.Fatal(err)
	}
	if got != log {
		t.Fatalf("Get(L1) = %+v, want %+v", got, log)
	}

	if _, err := st.Timelogs().Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrNotFound", err)
	}
}
