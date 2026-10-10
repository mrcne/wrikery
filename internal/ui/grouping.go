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
	id    string // the folder of a by folder section, empty for the other groupings and for Elsewhere
	title string
	rows  []int
}

// folderIndex is what grouping by folder needs from the sidebar tree.
// The sidebar sorts children by title, so order gives the sections the order the sidebar shows.
type folderIndex struct {
	title   map[string]string
	parents map[string][]string // every parent a folder is drawn under, the tree lists a shared folder once per parent
	order   map[string]int
}

func newFolderIndex(nodes []treeNode) folderIndex {
	idx := folderIndex{title: map[string]string{}, parents: map[string][]string{}, order: map[string]int{}}
	for i, n := range nodes {
		if n.kind == nodeMe {
			continue
		}
		idx.title[n.id] = n.title
		if _, seen := idx.order[n.id]; !seen {
			idx.order[n.id] = i
		}
		for _, c := range n.children {
			child := nodes[c].id
			if !slices.Contains(idx.parents[child], n.id) {
				idx.parents[child] = append(idx.parents[child], n.id)
			}
		}
	}
	return idx
}

// under reports whether the folder is the node in view or descends from it.
// My tasks is no folder, so nothing is under it.
func (f folderIndex) under(folderID, nodeID string) bool {
	seen := map[string]bool{}
	var climb func(id string) bool
	climb = func(id string) bool {
		if id == nodeID {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, p := range f.parents[id] {
			if climb(p) {
				return true
			}
		}
		return false
	}
	return climb(folderID)
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
	// The first path up that reaches the node wins, and a folder that reaches none answers with the first root it climbs to.
	seen := map[string]bool{}
	root := ""
	var climb func(id string) (string, bool)
	climb = func(id string) (string, bool) {
		if seen[id] {
			return "", false
		}
		seen[id] = true
		parents := f.parents[id]
		if len(parents) == 0 && root == "" {
			root = id
		}
		for _, p := range parents {
			if p == nodeID {
				return id, true
			}
			if s, ok := climb(p); ok {
				return s, true
			}
		}
		return "", false
	}
	if s, ok := climb(folderID); ok {
		return s, true
	}
	return root, mine
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
		g.id, g.title = k, idx.title[k]
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
	slices.SortStableFunc(keys, func(a, b string) int {
		ra, na := personRank(a, ref)
		rb, nb := personRank(b, ref)
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

// personRank orders people the way a standup reads: me first, then by the name as drawn, nobody ("") last.
// contactName falls back to the id, so a contact the cache does not know sorts by that and not ahead of everyone.
// The by assignee sections and the filter box share it, so the box lists people in the order of the lanes.
func personRank(id string, ref refData) (int, string) {
	switch id {
	case "":
		return 2, ""
	case ref.meID:
		return 0, ""
	}
	return 1, strings.ToLower(contactName(id, ref))
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

// statusWorkflows maps every cached status to the index of its workflow in ref.
func statusWorkflows(ref refData) map[string]int {
	wfOf := map[string]int{}
	for i, wf := range ref.workflows {
		for _, cs := range wf.CustomStatuses {
			wfOf[cs.ID] = i
		}
	}
	return wfOf
}

// workflowsByUse ranks the workflows the given rows sit on, as indices into ref.workflows:
// the one most rows use first and the standard one ahead on a tie, which is the board's main workflow and the box's order.
// With no row on any workflow the standard one stands in, or the first.
func workflowsByUse(all []taskRow, rows []int, ref refData) []int {
	wfOf := statusWorkflows(ref)
	counts := make([]int, len(ref.workflows))
	var idx []int
	for _, ri := range rows {
		if i, ok := wfOf[all[ri].task.CustomStatusID]; ok {
			if counts[i] == 0 {
				idx = append(idx, i)
			}
			counts[i]++
		}
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		if c := cmp.Compare(counts[b], counts[a]); c != 0 {
			return c
		}
		if sa, sb := ref.workflows[a].Standard, ref.workflows[b].Standard; sa != sb {
			if sa {
				return -1
			}
			return 1
		}
		return 0
	})
	if len(idx) == 0 {
		for i, wf := range ref.workflows {
			if wf.Standard {
				return []int{i}
			}
		}
		if len(ref.workflows) > 0 {
			return []int{0}
		}
	}
	return idx
}

func allRows(all []taskRow) []int {
	rows := make([]int, len(all))
	for i := range all {
		rows[i] = i
	}
	return rows
}

// viewWorkflows lists the workflows the rows of the node sit on, the one most rows use first, see workflowsByUse.
func viewWorkflows(all []taskRow, ref refData) []store.Workflow {
	var out []store.Workflow
	for _, i := range workflowsByUse(all, allRows(all), ref) {
		out = append(out, ref.workflows[i])
	}
	return out
}

// boardColumns lays the kept rows over the statuses of the workflow most of them sit on.
// A folder has no workflow field in the API and a space's default can name another workflow than its tasks use,
// so the rows themselves are the only honest source for the columns.
// A status set from the filter box names the columns outright, by status name, the done toggle already agrees with it.
// The map gives the column of every kept row, keyed by its index into all.
func boardColumns(all []taskRow, kept []int, ref refData, showDone bool, only map[string]bool) ([]boardColumn, map[int]int) {
	wfOf := statusWorkflows(ref)
	held := map[string]bool{}
	for _, ri := range kept {
		held[all[ri].task.CustomStatusID] = true
	}
	ranked := workflowsByUse(all, kept, ref)
	if len(kept) == 0 {
		// A filter that leaves no rows must not swap the columns to the standard workflow, the rows of the node say which one is in view.
		ranked = workflowsByUse(all, allRows(all), ref)
	}
	main := -1
	if len(ranked) > 0 {
		main = ranked[0]
	}
	var cols []boardColumn
	colOfStatus := map[string]int{}
	if main >= 0 {
		for _, cs := range ref.workflows[main].CustomStatuses {
			if len(only) > 0 {
				if !only[statusKey(cs.Name)] {
					continue
				}
			} else if (cs.Hidden && !held[cs.ID]) || (isDoneGroup(cs.Group) && !showDone) {
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
