package store

import (
	"context"
	"testing"
	"time"
)

func TestListForUserRange(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Timelogs().Upsert(ctx, []Timelog{
		{ID: "a", TaskID: "T", UserID: "ME", TrackedDate: "2026-08-31", Hours: 1},
		{ID: "b", TaskID: "T", UserID: "ME", TrackedDate: "2026-09-06", Hours: 2},
		{ID: "c", TaskID: "T", UserID: "ME", TrackedDate: "2026-09-07", Hours: 3},
		{ID: "d", TaskID: "T", UserID: "OTHER", TrackedDate: "2026-09-01", Hours: 4},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Timelogs().ListForUser(ctx, "ME", "2026-08-31", "2026-09-06")
	if err != nil || len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestReplaceForUserRangeKeepsLocalRowsAndReportsChange(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T", "t")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogCreate(ctx, "T", "ME", TimelogCreatePayload{Hours: 1, TrackedDate: "2026-09-02"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Timelogs().Upsert(ctx, []Timelog{{ID: "old", TaskID: "T", UserID: "ME", TrackedDate: "2026-09-01", Hours: 1}}); err != nil {
		t.Fatal(err)
	}
	changed, err := st.Timelogs().ReplaceForUserRange(ctx, "ME", "2026-08-31", "2026-09-06", []Timelog{
		{ID: "new", TaskID: "T", UserID: "ME", TrackedDate: "2026-09-03", Hours: 2, UpdatedDate: "2026-09-03T10:00:00Z"},
	})
	if err != nil || !changed {
		t.Fatalf("changed = %v, %v", changed, err)
	}
	got, _ := st.Timelogs().ListForUser(ctx, "ME", "2026-08-31", "2026-09-06")
	ids := map[string]bool{}
	for _, l := range got {
		ids[l.ID] = true
	}
	if ids["old"] || !ids["new"] || len(got) != 2 {
		t.Errorf("rows after replace: %+v (want new plus the local row)", got)
	}
	changed, err = st.Timelogs().ReplaceForUserRange(ctx, "ME", "2026-08-31", "2026-09-06", []Timelog{
		{ID: "new", TaskID: "T", UserID: "ME", TrackedDate: "2026-09-03", Hours: 2, UpdatedDate: "2026-09-03T10:00:00Z"},
	})
	if err != nil || changed {
		t.Errorf("same data again should report no change: %v, %v", changed, err)
	}
}

func TestTimelogWindow(t *testing.T) {
	from, to := TimelogWindow(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)) // Thursday
	if from != "2026-07-06" || to != "2026-09-06" {
		t.Errorf("window = %s..%s", from, to)
	}
	from, to = TimelogWindow(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)) // Sunday belongs to the same week
	if from != "2026-07-06" || to != "2026-09-06" {
		t.Errorf("sunday window = %s..%s", from, to)
	}
}
