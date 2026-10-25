package syncer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestConflictHoldsABudgetEdit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.CustomFields().ReplaceAll(ctx, []store.CustomField{{ID: "CF1", Title: "Budget", Type: "Duration"}}); err != nil {
		t.Fatal(err)
	}
	task := store.Task{ID: "T1", Title: "t", Status: "Active", CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z",
		CustomFields: []store.CustomFieldValue{{ID: "CF1", Value: "1200"}}, Effort: &store.TaskEffort{Mode: "Basic", TotalMinutes: 1200}}
	if err := st.Tasks().Upsert(ctx, []store.Task{task}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{CustomFields: []store.CustomFieldValue{{ID: "CF1", Value: "1500"}}})
	if err != nil {
		t.Fatal(err)
	}
	row, _ := st.Outbox().Get(ctx, id)
	var p store.TaskUpdatePayload
	_ = json.Unmarshal(row.Payload, &p)
	// Someone set 30h on Wrike meanwhile: held, with the hours in the wording.
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		return []wrike.Task{{ID: "T1", Title: "t", Status: "Active", CustomFields: []wrike.CustomFieldValue{{ID: "CF1", Value: "1800"}}}}, nil
	}}
	c, err := conflictOf(ctx, fc, st, row, p, map[string]store.Task{})
	if err != nil || c == nil || !strings.Contains(c.Error(), "budget 30h now, was 20h") {
		t.Errorf("conflict = %v, %v", c, err)
	}
	// Wrike already holds the edit's own value: no conflict.
	fc.tasksByIDs = func(ids, fields []string) ([]wrike.Task, error) {
		return []wrike.Task{{ID: "T1", Title: "t", Status: "Active", CustomFields: []wrike.CustomFieldValue{{ID: "CF1", Value: "1500"}}}}, nil
	}
	if c, err := conflictOf(ctx, fc, st, row, p, map[string]store.Task{}); err != nil || c != nil {
		t.Errorf("same value: conflict = %v, %v", c, err)
	}
	// An effort edit against a read that did not carry the effort says nothing and holds nothing.
	id, _ = st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Effort: &store.TaskEffort{Mode: "Basic", TotalMinutes: 600}})
	row, _ = st.Outbox().Get(ctx, id)
	_ = json.Unmarshal(row.Payload, &p)
	fc.tasksByIDs = func(ids, fields []string) ([]wrike.Task, error) {
		return []wrike.Task{{ID: "T1", Title: "t", Status: "Active"}}, nil
	}
	if c, err := conflictOf(ctx, fc, st, row, p, map[string]store.Task{}); err != nil || c != nil {
		t.Errorf("effort unknown: conflict = %v, %v", c, err)
	}
	fc.tasksByIDs = func(ids, fields []string) ([]wrike.Task, error) {
		return []wrike.Task{{ID: "T1", Title: "t", Status: "Active", Effort: &wrike.TaskEffort{Mode: "Basic", TotalEffort: 900}}}, nil
	}
	if c, err := conflictOf(ctx, fc, st, row, p, map[string]store.Task{}); err != nil || c == nil || !strings.Contains(c.Error(), "budget 15h now, was 20h") {
		t.Errorf("effort changed: conflict = %v, %v", c, err)
	}
}
