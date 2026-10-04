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

// Name is the first and the last name, trimmed.
// The interface and the commands both print a person this way.
func (c Contact) Name() string {
	return strings.TrimSpace(c.FirstName + " " + c.LastName)
}
