package store

import (
	"context"
	"math"
	"testing"
)

func TestBudgetCandidatesMatchByTitleOrID(t *testing.T) {
	fields := []CustomField{{ID: "CF1", Title: "Budget", Type: "Duration"}, {ID: "CF2", Title: "Budget", Type: "Numeric", SpaceID: "SP1"}, {ID: "CF3", Title: "Budget left", Type: "CalculatedNumeric"}}
	if got := BudgetCandidates(fields, "Budget"); len(got) != 2 || got[0].ID != "CF1" || got[1].ID != "CF2" {
		t.Errorf("by title = %+v", got)
	}
	if got := BudgetCandidates(fields, "CF3"); len(got) != 1 || got[0].ID != "CF3" {
		t.Errorf("by id = %+v", got)
	}
	if got := BudgetCandidates(fields, "budget"); len(got) != 0 {
		t.Errorf("the title match is exact, got %+v", got)
	}
	if !IsBudgetType(fields[0]) || !IsBudgetType(fields[1]) || IsBudgetType(fields[2]) {
		t.Error("Duration and Numeric hold a budget, a formula field does not")
	}
}

func TestBudgetHoursReadsMinutesAndDecimalHours(t *testing.T) {
	duration, numeric := CustomField{Type: "Duration"}, CustomField{Type: "Numeric"}
	for _, tc := range []struct {
		f     CustomField
		value string
		want  float64
		ok    bool
	}{
		{duration, "765", 12.75, true}, {duration, "0", 0, false}, {duration, "", 0, false}, {duration, "abc", 0, false}, {duration, "12h 45m", 0, false},
		{numeric, "1.75", 1.75, true}, {numeric, "20", 20, true}, {numeric, "", 0, false}, {numeric, "many", 0, false},
	} {
		got, ok := BudgetHours(tc.f, tc.value)
		if ok != tc.ok || got != tc.want {
			t.Errorf("BudgetHours(%s, %q) = %v, %v, want %v, %v", tc.f.Type, tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBudgetValueRoundTrips(t *testing.T) {
	duration, numeric := CustomField{Type: "Duration"}, CustomField{Type: "Numeric"}
	if got := BudgetValue(duration, 1+20.0/60); got != "80" {
		t.Errorf("1h 20m as a Duration = %q, want 80", got)
	}
	if got := BudgetValue(numeric, 1.75); got != "1.75" {
		t.Errorf("1.75 as a Numeric = %q", got)
	}
	if got := BudgetValue(numeric, 20); got != "20" {
		t.Errorf("20 as a Numeric = %q", got)
	}
	for _, f := range []CustomField{duration, numeric} {
		for _, hours := range []float64{0.5, 1 + 20.0/60, 1.75, 200} {
			back, ok := BudgetHours(f, BudgetValue(f, hours))
			if !ok || math.Abs(back-hours) > 1.0/120 {
				t.Errorf("%s: %v -> %q -> %v", f.Type, hours, BudgetValue(f, hours), back)
			}
		}
	}
}

func TestBudgetFieldForPrefersTheAccountFieldThenTheTasksSpace(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Folders().ReplaceTree(ctx, []Folder{
		{ID: "SP1", Title: "Platform", Space: true, ChildIDs: []string{"F1"}}, {ID: "F1", Title: "API"},
		{ID: "SP2", Title: "Mobile", Space: true, ChildIDs: []string{"F2"}}, {ID: "F2", Title: "iOS"},
	}); err != nil {
		t.Fatal(err)
	}
	account := CustomField{ID: "CF0", Title: "Budget", Type: "Duration"}
	sp1 := CustomField{ID: "CF1", Title: "Budget", Type: "Duration", SpaceID: "SP1"}
	sp2 := CustomField{ID: "CF2", Title: "Budget", Type: "Numeric", SpaceID: "SP2"}
	if got, ok, err := st.BudgetFieldFor(ctx, []string{"F2"}, []CustomField{sp1, account, sp2}); err != nil || !ok || got.ID != "CF0" {
		t.Errorf("with an account field: %+v %v %v", got, ok, err)
	}
	if got, ok, err := st.BudgetFieldFor(ctx, []string{"F2"}, []CustomField{sp1, sp2}); err != nil || !ok || got.ID != "CF2" {
		t.Errorf("space fields only, a task in F2: %+v %v %v", got, ok, err)
	}
	if _, ok, err := st.BudgetFieldFor(ctx, []string{"F1"}, []CustomField{sp2}); err != nil || ok {
		t.Errorf("a task in F1 with only the Mobile field: ok %v, %v", ok, err)
	}
	if _, ok, err := st.BudgetFieldFor(ctx, nil, []CustomField{sp1}); err != nil || ok {
		t.Errorf("a task without folders: ok %v, %v", ok, err)
	}
}
