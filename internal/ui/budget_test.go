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
	// The sum of entries is added at run time, 0.1 + 0.2 lands a hair above 0.3 and must not read as over.
	spent := 0.0
	for _, h := range []float64{0.1, 0.2} {
		spent += h
	}
	if text, over := timeLine(spent, budget(0.3)); text != "18m of 18m, 0h left" || over != "" {
		t.Errorf("timeLine(0.1 + 0.2, 0.3) = %q, %q, want spot on", text, over)
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
	// The line reads the field a write goes to: an account level field wins over a space field the task carries a value in,
	// or the detail would show one budget and the box write another.
	fields2 := []store.CustomField{{ID: "CF4", Title: "Budget", Type: "Duration", SpaceID: "SP1"}, {ID: "CF1", Title: "Budget", Type: "Duration"}}
	inSpace := store.Task{ID: "T3", ParentIDs: []string{"F1"}, CustomFields: []store.CustomFieldValue{{ID: "CF4", Value: "600"}}}
	if got, _ := resolveBudget(ctx, st, config.BudgetConfig{Source: "field", Field: "Budget"}, fields2, false, inSpace); got.has || got.field.ID != "CF1" || got.reason != "" {
		t.Errorf("a value in the space field next to an account field: %+v, want the account field and no budget", got)
	}
	// The value the task carries counts for the line even when the write target is another space's field.
	carried := store.Task{ID: "T2", ParentIDs: []string{"F1"}, CustomFields: []store.CustomFieldValue{{ID: "CF3", Value: "10"}}}
	if got, _ := resolveBudget(ctx, st, config.BudgetConfig{Source: "field", Field: "Hours"}, fields, false, carried); !got.has || got.hours != 10 || got.reason != `Custom field "Hours" is not in this task's space` || got.field.ID != "" {
		t.Errorf("a carried value of another space's field: %+v", got)
	}
	// A value nobody can read is no budget and no reason, the box opens on an empty field.
	task.CustomFields = []store.CustomFieldValue{{ID: "CF1", Value: "soon"}}
	if got, _ := resolveBudget(ctx, st, config.BudgetConfig{Source: "field", Field: "Budget"}, fields, false, task); got.has || got.reason != "" || got.field.ID != "CF1" {
		t.Errorf("unreadable value: %+v", got)
	}
}

// The reference data can land after the task, the hourly pull brings a new field for example, and the detail must follow it.
func TestDetailReloadsTheBudgetWhenTheReferenceDataLands(t *testing.T) {
	cfg := rootTestOptions(t).Config
	cfg.Budget = config.BudgetConfig{Source: "field", Field: "Budget"}
	m := demoModelWith(t, cfg)
	first, err := m.opts.Store.Tasks().Get(context.Background(), "IEAATASK00")
	if err != nil {
		t.Fatal(err)
	}
	m = pump(m, openTaskMsg{task: first})
	if !m.detail.budget.has {
		t.Fatalf("the detail did not load the budget: %+v", m.detail.budget)
	}
	bare := m.ref
	bare.customFields = nil
	m = pump(m, refLoadedMsg{ref: bare})
	if m.detail.budget.has || m.detail.budget.reason == "" {
		t.Errorf("after the fields went away the detail still has %+v", m.detail.budget)
	}
	full := bare
	full.customFields, _ = m.opts.Store.CustomFields().List(context.Background())
	m = pump(m, refLoadedMsg{ref: full})
	if !m.detail.budget.has {
		t.Errorf("after the fields came back the detail has %+v", m.detail.budget)
	}
}

// The Effort write sends Basic whatever mode the task had: a FullTime or Flexible total is Wrike's own and a sent one would be ignored or refused.
func TestBudgetPayloadSendsBasicAndClearsWithNone(t *testing.T) {
	for _, mode := range []string{"", "None", "Basic", "Flexible", "FullTime"} {
		p, ok := budgetPayload(budgetInfo{source: "effort", effortMode: mode}, 2.5)
		if !ok || p.Effort == nil || p.Effort.Mode != "Basic" || p.Effort.TotalMinutes != 150 {
			t.Errorf("mode %q: payload = %+v, %v, want Basic with 150 minutes", mode, p.Effort, ok)
		}
	}
	if p, ok := budgetPayload(budgetInfo{source: "effort", effortMode: "Basic"}, 0); !ok || p.Effort == nil || p.Effort.Mode != "None" || p.Effort.TotalMinutes != 0 {
		t.Errorf("a clear = %+v, %v, want None", p.Effort, ok)
	}
	field := store.CustomField{ID: "CF1", Type: "Duration"}
	if p, ok := budgetPayload(budgetInfo{source: "field", field: field}, 2.5); !ok || len(p.CustomFields) != 1 || p.CustomFields[0].Value != "150" {
		t.Errorf("a field write = %+v, %v", p.CustomFields, ok)
	}
	if _, ok := budgetPayload(budgetInfo{source: "field"}, 2.5); ok {
		t.Error("a field source without a resolved field has nothing to write to")
	}
}
