package syncer

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestEffortReadsDropTheFieldAfterA403(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	var asked [][]string
	fc := &fakeClient{
		tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
			asked = append(asked, p.Fields)
			if slices.Contains(p.Fields, "effortAllocation") {
				return wrike.TasksPage{}, &wrike.APIError{StatusCode: 403, Code: "not_allowed", Description: "license"}
			}
			return wrike.TasksPage{}, nil
		},
		tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
			asked = append(asked, fields)
			return nil, nil
		},
	}
	r := newEffortReads(ctx, fc, st, slog.Default())
	if _, err := r.Tasks(ctx, wrike.TaskParams{Fields: pullFields}); err != nil {
		t.Fatalf("the 403 must be swallowed and the read repeated: %v", err)
	}
	if _, err := r.TasksByIDs(ctx, []string{"T1"}, reachFields); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 || !slices.Contains(asked[0], "effortAllocation") || slices.Contains(asked[1], "effortAllocation") || slices.Contains(asked[2], "effortAllocation") {
		t.Errorf("fields asked = %q, want the effort once and then never", asked)
	}
	if v, _ := st.GetMeta(ctx, store.MetaKeyEffortUnavailable); v != "1" {
		t.Errorf("meta row = %q, want 1", v)
	}
	// A new engine starts from the row, a manual cycle asks again.
	again := newEffortReads(ctx, fc, st, slog.Default())
	if !again.off.Load() {
		t.Error("a fresh wrapper did not read the meta row")
	}
	if err := again.reset(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := again.Tasks(ctx, wrike.TaskParams{Fields: pullFields}); err != nil {
		t.Fatal(err)
	}
	// The fake answers the 403 again, so the read is asked with the effort once more and repeated without it.
	if last := asked[len(asked)-2]; !slices.Contains(last, "effortAllocation") {
		t.Errorf("after the reset the read asked %q, want the effort again", last)
	}
}

func TestEffortReadsPassOtherErrorsThrough(t *testing.T) {
	st := newTestStore(t)
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		return wrike.TasksPage{}, &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}
	}}
	r := newEffortReads(context.Background(), fc, st, slog.Default())
	if _, err := r.Tasks(context.Background(), wrike.TaskParams{}); err == nil || r.off.Load() {
		t.Errorf("a 404 must come back as it is and leave the field on: %v, off %v", err, r.off.Load())
	}
}
