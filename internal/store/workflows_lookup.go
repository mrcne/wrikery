package store

import "strings"

// WorkflowFor returns the workflow that holds the custom status id.
// The status dialog and the task commands both offer only the statuses of the task's own workflow, because a status picked from another one moves the task onto it.
// The rule lives here so the two front ends cannot drift apart.
func WorkflowFor(workflows []Workflow, statusID string) (Workflow, bool) {
	for _, wf := range workflows {
		for _, cs := range wf.CustomStatuses {
			if cs.ID == statusID {
				return wf, true
			}
		}
	}
	return Workflow{}, false
}

// FirstActiveStatus returns the id of the first visible Active status of the standard workflow, empty when there is none.
// The interface and the commands show it on a new task until Wrike answers, and both queue it with the create.
// One copy keeps the two from showing different statuses for the same task.
func FirstActiveStatus(workflows []Workflow) string {
	for _, wf := range workflows {
		if !wf.Standard {
			continue
		}
		for _, cs := range wf.CustomStatuses {
			if cs.Group == "Active" && !cs.Hidden {
				return cs.ID
			}
		}
	}
	return ""
}

// Name is the first and the last name, trimmed.
// The interface and the commands both print a person this way.
func (c Contact) Name() string {
	return strings.TrimSpace(c.FirstName + " " + c.LastName)
}
