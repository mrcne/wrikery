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
	// The engine's hook runs once per switch off, the interface reloads its reference data on it.
	hooked := 0
	r.onOff = func() { hooked++ }
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
	if hooked != 1 {
		t.Errorf("the hook ran %d times, want once", hooked)
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
	// A folder the user lost access to answers 403 access_forbidden, which says nothing about the plan.
	for _, apiErr := range []*wrike.APIError{{StatusCode: 404, Code: "resource_not_found"}, {StatusCode: 403, Code: "access_forbidden"}} {
		fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) { return wrike.TasksPage{}, apiErr }}
		r := newEffortReads(context.Background(), fc, st, slog.Default())
		if _, err := r.Tasks(context.Background(), wrike.TaskParams{}); err == nil || r.off.Load() {
			t.Errorf("%s must come back as it is and leave the field on: %v, off %v", apiErr.Code, err, r.off.Load())
		}
	}
}

// Wrike may leave the key out for a task whose Effort was cleared, and the upsert keeps the columns on a nil Effort,
// so a read that asked for the field fills a None in, which the upsert then stores.
func TestEffortReadsFillANoneInForATaskWithoutTheField(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	fc := &fakeClient{
		tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
			return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T1"}, {ID: "T2", Effort: &wrike.TaskEffort{Mode: "Basic", TotalEffort: 60}}}}, nil
		},
		tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) { return []wrike.Task{{ID: "T1"}}, nil },
	}
	r := newEffortReads(ctx, fc, st, slog.Default())
	page, err := r.Tasks(ctx, wrike.TaskParams{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Tasks[0].Effort == nil || page.Tasks[0].Effort.Mode != "None" || page.Tasks[1].Effort.TotalEffort != 60 {
		t.Errorf("tasks = %+v, want None filled in and the answered effort kept", page.Tasks)
	}
	byID, _ := r.TasksByIDs(ctx, []string{"T1"}, nil)
	if byID[0].Effort == nil || byID[0].Effort.Mode != "None" {
		t.Errorf("by id = %+v, want None filled in", byID)
	}
	// Off, the read did not ask, nothing is filled in.
	if err := r.st.SetMeta(ctx, store.MetaKeyEffortUnavailable, "1"); err != nil {
		t.Fatal(err)
	}
	off := newEffortReads(ctx, fc, st, slog.Default())
	if page, _ := off.Tasks(ctx, wrike.TaskParams{}); page.Tasks[0].Effort != nil {
		t.Errorf("a read that did not ask filled %+v in", page.Tasks[0].Effort)
	}
}
