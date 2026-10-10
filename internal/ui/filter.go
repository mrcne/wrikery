package ui

import (
	"slices"
	"sort"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
)

// unassigned stands for a task with nobody on it in the person filter, where a contact id otherwise goes.
const unassigned = "unassigned"

// rowFilter is what the filter box narrows the list and the board to.
// A dimension left empty takes every row, the three combine, and a status set also names the board's columns.
// Statuses are keyed by name, not by id: every workflow has its own ids and an account's workflows share names like In Progress,
// so a set keyed by id would cover one workflow and leave a folder on another empty under a line naming a status it has.
// The sets hold true entries only, so two filters with the same choice compare equal.
type rowFilter struct {
	importance map[string]bool // the levels shown
	person     string          // a contact id, unassigned, or empty for everyone
	statuses   map[string]bool // the status names shown, as statusKey spells them
}

func (f rowFilter) active() bool {
	return len(f.importance) > 0 || f.person != "" || len(f.statuses) > 0
}

// statusKey is the name a status is filtered by, the same across workflows and letter cases, the demo has In Progress and In progress.
func statusKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// taskStatusKey is the key of a task's status: the cached status name, or the group the row falls back on when the cache has no such status.
func taskStatusKey(t store.Task, ref refData) string {
	if cs, ok := ref.statuses[t.CustomStatusID]; ok {
		return statusKey(cs.Name)
	}
	return statusKey(t.Status)
}

// matches takes a task with several people when any of them is the one picked.
func (f rowFilter) matches(t store.Task, ref refData) bool {
	if len(f.importance) > 0 && !f.importance[t.Importance] {
		return false
	}
	switch f.person {
	case "":
	case unassigned:
		if len(t.ResponsibleIDs) > 0 {
			return false
		}
	default:
		if !slices.Contains(t.ResponsibleIDs, f.person) {
			return false
		}
	}
	return len(f.statuses) == 0 || f.statuses[taskStatusKey(t, ref)]
}

// summary is the line under the rows: the levels, the person and the statuses, in the order the box lists them.
func (f rowFilter) summary(ref refData) string {
	var parts []string
	if len(f.importance) > 0 {
		var levels []string
		for _, level := range importanceLevels {
			if f.importance[level] {
				levels = append(levels, level)
			}
		}
		parts = append(parts, strings.Join(levels, ", "))
	}
	switch f.person {
	case "":
	case unassigned:
		parts = append(parts, "Unassigned")
	case ref.meID:
		parts = append(parts, contactName(f.person, ref)+" (me)")
	default:
		parts = append(parts, contactName(f.person, ref))
	}
	if len(f.statuses) > 0 {
		parts = append(parts, strings.Join(statusNames(f.statuses, ref), ", "))
	}
	return strings.Join(parts, " | ")
}

// statusNames lists the set as the workflows spell it, each name once in workflow order, and a name no cached workflow holds as it is at the end.
func statusNames(keys map[string]bool, ref refData) []string {
	var names []string
	seen := map[string]bool{}
	for _, wf := range ref.workflows {
		for _, cs := range wf.CustomStatuses {
			if k := statusKey(cs.Name); keys[k] && !seen[k] {
				names = append(names, cs.Name)
				seen[k] = true
			}
		}
	}
	var rest []string
	for k := range keys {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(names, rest...)
}

// doneKeys are the names of the completed and cancelled statuses of the given workflows, hidden ones left out.
func doneKeys(wfs []store.Workflow) map[string]bool {
	out := map[string]bool{}
	for _, wf := range wfs {
		for _, cs := range wf.CustomStatuses {
			if isDoneGroup(cs.Group) && !cs.Hidden {
				out[statusKey(cs.Name)] = true
			}
		}
	}
	return out
}

// hasDone tells whether the set holds a completed or cancelled status of any cached workflow, which the done toggle has to agree with.
func (f rowFilter) hasDone(ref refData) bool {
	done := doneKeys(ref.workflows)
	for k := range f.statuses {
		if done[k] {
			return true
		}
	}
	return false
}

// withDone is the set after the done toggle: on adds the done statuses of the workflows in view, off takes every done status out,
// the ones of other workflows too, and a set with nothing left is no filter.
// The set is built anew, a map shared with an older copy of the model would otherwise change under it.
func (f rowFilter) withDone(ref refData, inView []store.Workflow, on bool) rowFilter {
	done := doneKeys(ref.workflows)
	next := map[string]bool{}
	for k := range f.statuses {
		if on || !done[k] {
			next[k] = true
		}
	}
	if on {
		for k := range doneKeys(inView) {
			next[k] = true
		}
	}
	if len(next) == 0 {
		next = nil
	}
	f.statuses = next
	return f
}
