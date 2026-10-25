package ui

import (
	"context"
	"fmt"

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
		field, ok, err := st.BudgetFieldFor(ctx, task.ParentIDs, typed)
		if err != nil {
			return b, err
		}
		if !ok {
			b.reason = fmt.Sprintf("Custom field %q is not in this task's space", cfg.Field)
			return b, nil
		}
		b.field = field
		for _, v := range task.CustomFields {
			if v.ID == field.ID {
				b.hours, b.has = store.BudgetHours(field, v.Value)
			}
		}
		return b, nil
	}
	b.reason = "No budget source is set, see budget.source in the config"
	return b, nil
}

// timeLine is the Time line of the detail: the logged time against the budget, the over part apart so the caller colors it.
// Empty when the task has neither a budget nor an entry.
func timeLine(spent float64, b budgetInfo) (text, over string) {
	switch {
	case !b.has && spent == 0:
		return "", ""
	case !b.has:
		return hoursText(spent), ""
	case spent > b.hours:
		return hoursText(spent) + " of " + hoursText(b.hours), hoursText(spent-b.hours) + " over"
	}
	return fmt.Sprintf("%s of %s, %s left", hoursText(spent), hoursText(b.hours), hoursText(b.hours-spent)), ""
}
