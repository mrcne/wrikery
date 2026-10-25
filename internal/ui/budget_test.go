package ui

import (
	"context"
	"testing"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestTimeLineShapes(t *testing.T) {
	budget := func(h float64) budgetInfo { return budgetInfo{source: "field", hours: h, has: true} }
	for _, tc := range []struct {
		spent      float64
		b          budgetInfo
		text, over string
	}{
		{12.5, budget(20), "12h 30m of 20h, 7h 30m left", ""},
		{22, budget(20), "22h of 20h", "2h over"},
		{20, budget(20), "20h of 20h, 0h left", ""},
		{0, budget(20), "0h of 20h, 20h left", ""},
		{12.5, budgetInfo{}, "12h 30m", ""},
		{0, budgetInfo{}, "", ""},
	} {
		text, over := timeLine(tc.spent, tc.b)
		if text != tc.text || over != tc.over {
			t.Errorf("timeLine(%v, %+v) = %q, %q, want %q, %q", tc.spent, tc.b, text, over, tc.text, tc.over)
		}
	}
}

// seedStoreForBudget is a store with two spaces, Platform over F1 and Mobile over F2, the tree the field resolution walks.
func seedStoreForBudget(t *testing.T) *store.Store {
	t.Helper()
	st := rootTestOptions(t).Store
	if err := st.Folders().ReplaceTree(context.Background(), []store.Folder{
		{ID: "SP1", Title: "Platform", Space: true, ChildIDs: []string{"F1"}}, {ID: "F1", Title: "API"},
		{ID: "SP2", Title: "Mobile", Space: true, ChildIDs: []string{"F2"}}, {ID: "F2", Title: "iOS"},
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestResolveBudgetNamesTheReason(t *testing.T) {
	st := seedStoreForBudget(t)
	ctx := context.Background()
	fields := []store.CustomField{{ID: "CF1", Title: "Budget", Type: "Duration"}, {ID: "CF2", Title: "Notes", Type: "Text"}, {ID: "CF3", Title: "Hours", Type: "Numeric", SpaceID: "SP2"}}
	task := store.Task{ID: "T1", ParentIDs: []string{"F1"}, CustomFields: []store.CustomFieldValue{{ID: "CF1", Value: "1200"}}, Effort: &store.TaskEffort{Mode: "Basic", TotalMinutes: 600}}
	for name, tc := range map[string]struct {
		cfg    config.BudgetConfig
		effort bool
		want   budgetInfo
	}{
		"none":        {config.BudgetConfig{}, false, budgetInfo{reason: "No budget source is set, see budget.source in the config"}},
		"field":       {config.BudgetConfig{Source: "field", Field: "Budget"}, false, budgetInfo{source: "field", field: fields[0], hours: 20, has: true}},
		"by id":       {config.BudgetConfig{Source: "field", Field: "CF1"}, false, budgetInfo{source: "field", field: fields[0], hours: 20, has: true}},
		"missing":     {config.BudgetConfig{Source: "field", Field: "Nope"}, false, budgetInfo{source: "field", reason: `Custom field "Nope" not found on this account`}},
		"wrong type":  {config.BudgetConfig{Source: "field", Field: "Notes"}, false, budgetInfo{source: "field", reason: `Custom field "Notes" is a Text field, a Duration or Numeric field is needed`}},
		"other space": {config.BudgetConfig{Source: "field", Field: "Hours"}, false, budgetInfo{source: "field", reason: `Custom field "Hours" is not in this task's space`}},
		"effort":      {config.BudgetConfig{Source: "effort"}, false, budgetInfo{source: "effort", effortMode: "Basic", hours: 10, has: true}},
		"no effort":   {config.BudgetConfig{Source: "effort"}, true, budgetInfo{source: "effort", reason: "This account has no Effort field"}},
	} {
		got, err := resolveBudget(ctx, st, tc.cfg, fields, tc.effort, task)
		if err != nil || got != tc.want {
			t.Errorf("%s: %+v, %v, want %+v", name, got, err, tc.want)
		}
	}
	// A value nobody can read is no budget and no reason, the box opens on an empty field.
	task.CustomFields = []store.CustomFieldValue{{ID: "CF1", Value: "soon"}}
	if got, _ := resolveBudget(ctx, st, config.BudgetConfig{Source: "field", Field: "Budget"}, fields, false, task); got.has || got.reason != "" || got.field.ID != "CF1" {
		t.Errorf("unreadable value: %+v", got)
	}
}
