package ui

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
)

type groupKey int

const (
	groupNone groupKey = iota
	groupFolder
	groupAssignee
	groupStatus
)

func (g groupKey) String() string {
	switch g {
	case groupFolder:
		return "folder"
	case groupAssignee:
		return "assignee"
	case groupStatus:
		return "status"
	}
	return ""
}

// taskGroup is one section of the list or one lane of the board.
// rows index taskListModel.all, and a task can sit in more than one group.
type taskGroup struct {
	title string
	rows  []int
}

// folderIndex is what grouping by folder needs from the sidebar tree.
// The sidebar sorts children by title, so order gives the sections the order the sidebar shows.
type folderIndex struct {
	title  map[string]string
	parent map[string]string // "" at the top level
	order  map[string]int
}

func newFolderIndex(nodes []treeNode) folderIndex {
	idx := folderIndex{title: map[string]string{}, parent: map[string]string{}, order: map[string]int{}}
	for i, n := range nodes {
		if n.kind == nodeMe {
			continue
		}
		idx.title[n.id] = n.title
		idx.order[n.id] = i
		for _, c := range n.children {
			idx.parent[nodes[c].id] = n.id
		}
	}
	return idx
}

// under reports whether the folder is the node in view or descends from it.
// My tasks is no folder, so nothing is under it.
func (f folderIndex) under(folderID, nodeID string) bool {
	for id := folderID; id != ""; id = f.parent[id] {
		if id == nodeID {
			return true
		}
	}
	return false
}

const elsewhere = "Elsewhere"

// sectionFor maps a parent folder to its section under the node in view:
// the node itself, or the child of the node the folder descends from.
// My tasks spans the whole tree, there the section is the top level ancestor,
// and a folder the tree does not know goes to Elsewhere ("").
// In a folder view a parent outside the node is not a section, the task is in the view because of another parent, so ok is false.
func (f folderIndex) sectionFor(folderID, nodeID string) (section string, ok bool) {
	if folderID == nodeID {
		return nodeID, true
	}
	mine := nodeID == store.ScopeKindMe
	if _, known := f.title[folderID]; !known {
		return "", mine
	}
	id := folderID
	for {
		parent := f.parent[id]
		if parent == nodeID {
			return id, true
		}
		if parent == "" {
			return id, mine
		}
		id = parent
	}
}

func groupByFolder(all []taskRow, kept []int, nodeID string, idx folderIndex) []taskGroup {
	byKey := map[string]*taskGroup{}
	var keys []string
	add := func(key string, ri int) {
		g, ok := byKey[key]
		if !ok {
			g = &taskGroup{}
			byKey[key] = g
			keys = append(keys, key)
		}
		g.rows = append(g.rows, ri)
	}
	for _, ri := range kept {
		placed := false
		seen := map[string]bool{}
		for _, p := range all[ri].task.ParentIDs {
			key, ok := idx.sectionFor(p, nodeID)
			if !ok || seen[key] {
				continue
			}
			seen[key] = true
			add(key, ri)
			placed = true
		}
		if !placed {
			add("", ri)
		}
	}
	rank := func(key string) int {
		switch key {
		case nodeID:
			return -1
		case "":
			// Last, whatever the tree holds. The node slice can be longer than the index when a folder sits under two parents.
			return math.MaxInt
		}
		return idx.order[key]
	}
	slices.SortStableFunc(keys, func(a, b string) int { return cmp.Compare(rank(a), rank(b)) })
	out := make([]taskGroup, 0, len(keys))
	for _, k := range keys {
		g := byKey[k]
		g.title = idx.title[k]
		if k == "" {
			g.title = elsewhere
		}
		out = append(out, *g)
	}
	return out
}

func groupByAssignee(all []taskRow, kept []int, ref refData) []taskGroup {
	byKey := map[string]*taskGroup{}
	var keys []string
	add := func(key string, ri int) {
		g, ok := byKey[key]
		if !ok {
			g = &taskGroup{}
			byKey[key] = g
			keys = append(keys, key)
		}
		g.rows = append(g.rows, ri)
	}
	for _, ri := range kept {
		ids := all[ri].task.ResponsibleIDs
		if len(ids) == 0 {
			add("", ri)
		}
		for _, id := range ids {
			add(id, ri)
		}
	}
	// Me first, then by the name as drawn, Unassigned last, the way a standup reads.
	// contactName falls back to the id, so a contact the cache does not know sorts by that and not ahead of everyone.
	rank := func(id string) (int, string) {
		switch id {
		case "":
			return 2, ""
		case ref.meID:
			return 0, ""
		}
		return 1, strings.ToLower(contactName(id, ref))
	}
	slices.SortStableFunc(keys, func(a, b string) int {
		ra, na := rank(a)
		rb, nb := rank(b)
		return cmp.Or(cmp.Compare(ra, rb), strings.Compare(na, nb), strings.Compare(a, b))
	})
	out := make([]taskGroup, 0, len(keys))
	for _, k := range keys {
		g := byKey[k]
		switch k {
		case "":
			g.title = "Unassigned"
		case ref.meID:
			g.title = contactName(k, ref) + " (me)"
		default:
			g.title = contactName(k, ref)
		}
		out = append(out, *g)
	}
	return out
}

func groupByStatus(cols []boardColumn) []taskGroup {
	var out []taskGroup
	for _, c := range cols {
		if len(c.rows) == 0 {
			continue
		}
		out = append(out, taskGroup{title: c.title, rows: c.rows})
	}
	return out
}

// boardColumn is one column of the board and one section of the by status grouping.
// A bucket gathers the rows of a workflow other than the main one, or rows whose status the cache does not know,
// and takes no card move.
type boardColumn struct {
	title  string
	status store.CustomStatus // zero for a bucket
	bucket bool
	rows   []int
}

func isDoneGroup(group string) bool { return group == "Completed" || group == "Cancelled" }

// boardColumns lays the kept rows over the statuses of the workflow most of them sit on.
// A folder has no workflow field in the API and a space's default can name another workflow than its tasks use,
// so the rows themselves are the only honest source for the columns.
// The map gives the column of every kept row, keyed by its index into all.
func boardColumns(all []taskRow, kept []int, ref refData, showDone bool) ([]boardColumn, map[int]int) {
	wfOf := map[string]int{}
	for i, wf := range ref.workflows {
		for _, cs := range wf.CustomStatuses {
			wfOf[cs.ID] = i
		}
	}
	counts := make([]int, len(ref.workflows))
	held := map[string]bool{}
	for _, ri := range kept {
		id := all[ri].task.CustomStatusID
		held[id] = true
		if i, ok := wfOf[id]; ok {
			counts[i]++
		}
	}
	main := -1
	for i, wf := range ref.workflows {
		if main < 0 || counts[i] > counts[main] || (counts[i] == counts[main] && wf.Standard && !ref.workflows[main].Standard) {
			main = i
		}
	}
	var cols []boardColumn
	colOfStatus := map[string]int{}
	if main >= 0 {
		for _, cs := range ref.workflows[main].CustomStatuses {
			if (cs.Hidden && !held[cs.ID]) || (isDoneGroup(cs.Group) && !showDone) {
				continue
			}
			colOfStatus[cs.ID] = len(cols)
			cols = append(cols, boardColumn{title: cs.Name, status: cs})
		}
	}
	extra := map[int]bool{}
	unknown := false
	for _, ri := range kept {
		id := all[ri].task.CustomStatusID
		if _, ok := colOfStatus[id]; ok {
			continue
		}
		if wi, ok := wfOf[id]; ok {
			extra[wi] = true
		} else {
			unknown = true
		}
	}
	bucketOf := map[int]int{}
	for wi := range ref.workflows {
		if extra[wi] {
			bucketOf[wi] = len(cols)
			cols = append(cols, boardColumn{title: ref.workflows[wi].Name, bucket: true})
		}
	}
	other := -1
	if unknown {
		other = len(cols)
		cols = append(cols, boardColumn{title: "other", bucket: true})
	}
	colOf := make(map[int]int, len(kept))
	for _, ri := range kept {
		id := all[ri].task.CustomStatusID
		c, ok := colOfStatus[id]
		if !ok {
			if wi, known := wfOf[id]; known {
				c = bucketOf[wi]
			} else {
				c = other
			}
		}
		cols[c].rows = append(cols[c].rows, ri)
		colOf[ri] = c
	}
	return cols, colOf
}

// stepStatus is the status delta places before or after the task's own in its workflow, hidden ones skipped.
// known is false when no cached workflow holds the task's status, ok is false at either end of the workflow.
func stepStatus(t store.Task, ref refData, delta int) (cs store.CustomStatus, known, ok bool) {
	for _, wf := range ref.workflows {
		at := -1
		var visible []store.CustomStatus
		for _, s := range wf.CustomStatuses {
			if s.ID == t.CustomStatusID {
				at = len(visible)
			} else if s.Hidden {
				continue
			}
			visible = append(visible, s)
		}
		if at < 0 {
			continue
		}
		to := at + delta
		if to < 0 || to >= len(visible) {
			return store.CustomStatus{}, true, false
		}
		return visible[to], true, true
	}
	return store.CustomStatus{}, false, false
}
