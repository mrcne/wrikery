package store

import (
	"context"
	"math"
	"strconv"
	"strings"
)

// BudgetCandidates are the fields a budget setting names, by exact title or by id, in the order given.
// A title can name several fields, an account level one and one per space, BudgetFieldFor picks among them.
func BudgetCandidates(fields []CustomField, nameOrID string) []CustomField {
	var out []CustomField
	for _, f := range fields {
		if f.Title == nameOrID || f.ID == nameOrID {
			out = append(out, f)
		}
	}
	return out
}

// IsBudgetType says whether a field can hold a time budget, a Duration or a Numeric field.
func IsBudgetType(f CustomField) bool { return f.Type == "Duration" || f.Type == "Numeric" }

// BudgetHours reads a field value as hours, false for an empty, zero or unreadable value, Wrike does not check what a person types.
// A Duration value is taken as the number of minutes, the unit of dates.duration and totalEffort in the API.
// The reference does not say and no account at hand has such a field, so this and BudgetValue are the two places to change if it is hours.
// A Numeric value is decimal hours, the way a number field stands in for a budget on an account without the Duration type.
func BudgetHours(f CustomField, value string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	switch f.Type {
	case "Duration":
		return v / 60, true
	case "Numeric":
		return v, true
	}
	return 0, false
}

// SplitHours rounds a decimal number of hours to whole hours and minutes, sixty minutes carrying into the hour.
// Rounded and not truncated, so a value that went through BudgetValue or a time entry edit reads back the same and never drifts a minute short.
func SplitHours(h float64) (hours, minutes int) {
	whole := math.Trunc(h)
	minutes = int(math.Round((h - whole) * 60))
	if minutes == 60 {
		whole++
		minutes = 0
	}
	return int(whole), minutes
}

// HoursText writes hours the way the interface writes logged time: 2h, 45m or 1h 20m, exact to the minute.
func HoursText(h float64) string {
	hours, minutes := SplitHours(h)
	switch {
	case minutes == 0:
		return strconv.Itoa(hours) + "h"
	case hours == 0:
		return strconv.Itoa(minutes) + "m"
	}
	return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
}

// BudgetWords is a field value as hours, none for no value, the raw value when it cannot be read as a budget.
// The changes screen and a held edit's reason both use it.
func BudgetWords(f CustomField, value string) string {
	if value == "" {
		return "none"
	}
	if hours, ok := BudgetHours(f, value); ok {
		return HoursText(hours)
	}
	return value
}

// MinutesWords is the Effort field's minutes as hours, none for no budget.
func MinutesWords(minutes int) string {
	if minutes <= 0 {
		return "none"
	}
	return HoursText(float64(minutes) / 60)
}

// BudgetValue writes hours in the field's format: whole minutes for a Duration, hours with at most two decimals for a Numeric field.
func BudgetValue(f CustomField, hours float64) string {
	if f.Type == "Duration" {
		return strconv.Itoa(int(math.Round(hours * 60)))
	}
	return strconv.FormatFloat(math.Round(hours*100)/100, 'f', -1, 64)
}

// BudgetFieldFor picks the field of a task among the candidates: an account level field first,
// else the field owned by the space the task's folders are in, found through the folder tree.
// A subtask made under a task has no folder of its own, its super tasks' folders stand in for it.
// False when no candidate fits the task, a space field of another space for example.
func (s *Store) BudgetFieldFor(ctx context.Context, task Task, candidates []CustomField) (CustomField, bool, error) {
	for _, f := range candidates {
		if f.SpaceID == "" {
			return f, true, nil
		}
	}
	if len(candidates) == 0 {
		return CustomField{}, false, nil
	}
	parentIDs := task.ParentIDs
	if len(parentIDs) == 0 {
		var err error
		if parentIDs, err = s.superTaskFolders(ctx, task.ID); err != nil {
			return CustomField{}, false, err
		}
	}
	spaces := map[string]bool{}
	for _, id := range parentIDs {
		found, err := s.spacesAbove(ctx, id)
		if err != nil {
			return CustomField{}, false, err
		}
		for _, space := range found {
			spaces[space] = true
		}
	}
	for _, f := range candidates {
		if spaces[f.SpaceID] {
			return f, true, nil
		}
	}
	return CustomField{}, false, nil
}

// superTaskFolders are the folders of the tasks a task is a subtask of, one hop up.
func (s *Store) superTaskFolders(ctx context.Context, taskID string) ([]string, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT DISTINCT folder_id FROM task_parents
		WHERE task_id IN (SELECT super_id FROM task_supertasks WHERE task_id = ?)`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// spacesAbove walks the folder tree up from a folder and names the space roots it reaches, the walk statusForNewTask does.
func (s *Store) spacesAbove(ctx context.Context, folderID string) ([]string, error) {
	rows, err := s.reader.QueryContext(ctx, `
		WITH RECURSIVE up(id) AS (
			SELECT ?
			UNION
			SELECT fc.parent_id FROM folder_children fc JOIN up ON fc.child_id = up.id
		)
		SELECT f.id FROM up JOIN folders f ON f.id = up.id WHERE f.space = 1`, folderID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var space string
		if err := rows.Scan(&space); err != nil {
			return nil, err
		}
		out = append(out, space)
	}
	return out, rows.Err()
}

// customValueOf is the task's value of one field, empty when the task has none.
func customValueOf(values []CustomFieldValue, id string) string {
	for _, v := range values {
		if v.ID == id {
			return v.Value
		}
	}
	return ""
}
