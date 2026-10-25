package ui

import (
	"context"
	"fmt"
	"math"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// budgetInfo is what the loader worked out for one task: where its budget lives and what it is.
type budgetInfo struct {
	source     string            // "", "effort" or "field"
	field      store.CustomField // with source field, the field the task reads and writes
	effortMode string            // with source effort, the task's mode, empty when never read
	hours      float64
	has        bool   // the task has a budget
	reason     string // why no budget can be read or edited, the toast and the settings row text, empty when it can
}

// resolveBudget works budgetInfo out for a task from the config, the account's fields, the Effort flag and the task itself.
// A title can name an account level field and one per space, so the field is picked per task through its folders.
func resolveBudget(ctx context.Context, st *store.Store, cfg config.BudgetConfig, fields []store.CustomField, effortUnavailable bool, task store.Task) (budgetInfo, error) {
	b := budgetInfo{source: cfg.Source}
	switch cfg.Source {
	case "effort":
		if effortUnavailable {
			b.reason = "This account has no Effort field"
			return b, nil
		}
		if task.Effort != nil {
			b.effortMode = task.Effort.Mode
			if task.Effort.TotalMinutes > 0 {
				b.hours, b.has = float64(task.Effort.TotalMinutes)/60, true
			}
		}
		return b, nil
	case "field":
		candidates := store.BudgetCandidates(fields, cfg.Field)
		if len(candidates) == 0 {
			b.reason = fmt.Sprintf("Custom field %q not found on this account", cfg.Field)
			return b, nil
		}
		var typed []store.CustomField
		for _, f := range candidates {
			if store.IsBudgetType(f) {
				typed = append(typed, f)
			}
		}
		if len(typed) == 0 {
			b.reason = fmt.Sprintf("Custom field %q is a %s field, a Duration or Numeric field is needed", cfg.Field, candidates[0].Type)
			return b, nil
		}
		// The value the task carries counts for the line whichever of the fields it is in, the walk below only picks the one a write goes to.
		for _, f := range typed {
			for _, v := range task.CustomFields {
				if v.ID == f.ID && !b.has {
					b.hours, b.has = store.BudgetHours(f, v.Value)
				}
			}
		}
		field, ok, err := st.BudgetFieldFor(ctx, task.ParentIDs, typed)
		if err != nil {
			return b, err
		}
		if !ok {
			b.reason = fmt.Sprintf("Custom field %q is not in this task's space", cfg.Field)
			return b, nil
		}
		b.field = field
		return b, nil
	}
	b.reason = "No budget source is set, see budget.source in the config"
	return b, nil
}

// budgetPayload is the write for a new budget, hours of 0 clear it. False when the task's budget has no source to write to.
func budgetPayload(b budgetInfo, hours float64) (store.TaskUpdatePayload, bool) {
	switch b.source {
	case "field":
		value := ""
		if hours > 0 {
			value = store.BudgetValue(b.field, hours)
		}
		return store.TaskUpdatePayload{CustomFields: []store.CustomFieldValue{{ID: b.field.ID, Value: value}}}, b.field.ID != ""
	case "effort":
		// The task's mode stays unless it has none, Basic is the plain total the Modify Task reference describes, None clears.
		mode := b.effortMode
		if mode == "" || mode == "None" {
			mode = "Basic"
		}
		minutes := int(math.Round(hours * 60))
		if minutes == 0 {
			mode = "None"
		}
		return store.TaskUpdatePayload{Effort: &store.TaskEffort{Mode: mode, TotalMinutes: minutes}}, true
	}
	return store.TaskUpdatePayload{}, false
}

// timeLine is the Time line of the detail: the logged time against the budget, the over part apart so the caller colors it.
// Empty when the task has neither a budget nor an entry.
func timeLine(spent float64, b budgetInfo) (text, over string) {
	// The sum of the entries is a float, compared at the minute, the unit the line is written in, so 0.1 + 0.2 is not over 0.3.
	spentMinutes, budgetMinutes := math.Round(spent*60), math.Round(b.hours*60)
	switch {
	case !b.has && spent == 0:
		return "", ""
	case !b.has:
		return hoursText(spent), ""
	case spentMinutes > budgetMinutes:
		return hoursText(spent) + " of " + hoursText(b.hours), hoursText((spentMinutes-budgetMinutes)/60) + " over"
	}
	return fmt.Sprintf("%s of %s, %s left", hoursText(spent), hoursText(b.hours), hoursText((budgetMinutes-spentMinutes)/60)), ""
}
