package ui

import (
	"reflect"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

// A tree the way loadTree flattens it: Platform holds the project API with the folder Auth under it and the folder Web,
// and Solo is a followed project at the top level.
func testFolderIndex() folderIndex {
	return newFolderIndex([]treeNode{
		{id: "me", title: "My tasks", kind: nodeMe},
		{id: "S1", title: "Platform", kind: nodeSpace, children: []int{2, 4}},
		{id: "P1", title: "API", kind: nodeProject, depth: 1, children: []int{3}},
		{id: "F1", title: "Auth", kind: nodeFolder, depth: 2},
		{id: "F2", title: "Web", kind: nodeFolder, depth: 1},
		{id: "P9", title: "Solo", kind: nodeProject},
	})
}

func rowsOf(tasks ...store.Task) ([]taskRow, []int) {
	all := make([]taskRow, 0, len(tasks))
	kept := make([]int, 0, len(tasks))
	for i, t := range tasks {
		all = append(all, taskRow{task: t})
		kept = append(kept, i)
	}
	return all, kept
}

func titlesOf(groups []taskGroup) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g.title)
	}
	return out
}

func columnTitles(cols []boardColumn) []string {
	var out []string
	for _, c := range cols {
		out = append(out, c.title)
	}
	return out
}

func idsIn(all []taskRow, g taskGroup) []string {
	var out []string
	for _, ri := range g.rows {
		out = append(out, all[ri].task.ID)
	}
	return out
}

func TestGroupByFolderUsesTheChildOfTheNodeInView(t *testing.T) {
	all, kept := rowsOf(
		store.Task{ID: "web", ParentIDs: []string{"F2"}},
		store.Task{ID: "auth", ParentIDs: []string{"F1"}},
		store.Task{ID: "root", ParentIDs: []string{"S1"}},
		store.Task{ID: "shared", ParentIDs: []string{"F2", "P1"}},
		store.Task{ID: "outside", ParentIDs: []string{"X", "P1"}},
	)
	groups := groupByFolder(all, kept, "S1", testFolderIndex())
	if got, want := titlesOf(groups), []string{"Platform", "API", "Web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	if got := idsIn(all, groups[1]); !reflect.DeepEqual(got, []string{"auth", "shared", "outside"}) {
		t.Errorf("API holds %v", got)
	}
	if got := idsIn(all, groups[2]); !reflect.DeepEqual(got, []string{"web", "shared"}) {
		t.Errorf("Web holds %v, a task in two children belongs to both", got)
	}
}

func TestGroupByFolderOnMyTasksUsesTheTopLevel(t *testing.T) {
	all, kept := rowsOf(
		store.Task{ID: "solo", ParentIDs: []string{"P9"}},
		store.Task{ID: "auth", ParentIDs: []string{"F1"}},
		store.Task{ID: "lost", ParentIDs: []string{"X"}},
		store.Task{ID: "none"},
	)
	groups := groupByFolder(all, kept, store.ScopeKindMe, testFolderIndex())
	if got, want := titlesOf(groups), []string{"Platform", "Solo", "Elsewhere"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	if got := idsIn(all, groups[2]); !reflect.DeepEqual(got, []string{"lost", "none"}) {
		t.Errorf("Elsewhere holds %v", got)
	}
}

func TestGroupByAssigneePutsMeFirstAndUnassignedLast(t *testing.T) {
	ref := refData{meID: "ME", contacts: map[string]store.Contact{
		"ME": {FirstName: "Ada", LastName: "Nowak"},
		"B1": {FirstName: "Bartek", LastName: "Lis"},
		"C1": {FirstName: "Celina", LastName: "Wrona"},
	}}
	all, kept := rowsOf(
		store.Task{ID: "c", ResponsibleIDs: []string{"C1"}},
		store.Task{ID: "shared", ResponsibleIDs: []string{"B1", "ME"}},
		store.Task{ID: "nobody"},
		store.Task{ID: "gone", ResponsibleIDs: []string{"ZZ"}},
	)
	groups := groupByAssignee(all, kept, ref)
	want := []string{"Ada Nowak (me)", "Bartek Lis", "Celina Wrona", "ZZ", "Unassigned"}
	if got := titlesOf(groups); !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	if got := idsIn(all, groups[1]); !reflect.DeepEqual(got, []string{"shared"}) {
		t.Errorf("a shared task belongs to every one of its people, Bartek holds %v", got)
	}
}

func testWorkflows() refData {
	wfs := []store.Workflow{
		{ID: "W1", Name: "Default Workflow", Standard: true, CustomStatuses: []store.CustomStatus{
			{ID: "S1", Name: "New", Group: "Active"},
			{ID: "S2", Name: "In Progress", Group: "Active"},
			{ID: "S3", Name: "Secret", Group: "Active", Hidden: true},
			{ID: "S4", Name: "On Hold", Group: "Deferred"},
			{ID: "S5", Name: "Completed", Group: "Completed"},
		}},
		{ID: "W2", Name: "Task workflow", CustomStatuses: []store.CustomStatus{
			{ID: "T1", Name: "Planned", Group: "Active"},
			{ID: "T2", Name: "Done", Group: "Completed"},
		}},
	}
	ref := refData{workflows: wfs, statuses: map[string]store.CustomStatus{}}
	for _, wf := range wfs {
		for _, cs := range wf.CustomStatuses {
			ref.statuses[cs.ID] = cs
		}
	}
	return ref
}

func TestBoardColumnsFollowTheMainWorkflow(t *testing.T) {
	ref := testWorkflows()
	all, kept := rowsOf(
		store.Task{ID: "a", CustomStatusID: "S2"},
		store.Task{ID: "b", CustomStatusID: "S2"},
		store.Task{ID: "c", CustomStatusID: "T1"},
		store.Task{ID: "d", CustomStatusID: "S3"},
		store.Task{ID: "e", CustomStatusID: "??"},
	)
	cols, colOf := boardColumns(all, kept, ref, false)
	want := []string{"New", "In Progress", "Secret", "On Hold", "Task workflow", "other"}
	if got := columnTitles(cols); !reflect.DeepEqual(got, want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	if colOf[2] != 4 || colOf[4] != 5 || colOf[3] != 2 {
		t.Errorf("colOf = %v", colOf)
	}
	if !cols[4].bucket || !cols[5].bucket || cols[1].bucket {
		t.Error("the trailing columns are buckets and the workflow columns are not")
	}
	if len(cols[0].rows) != 0 || len(cols[1].rows) != 2 {
		t.Errorf("rows per column: New %d, In Progress %d", len(cols[0].rows), len(cols[1].rows))
	}
	withDone, _ := boardColumns(all, kept, ref, true)
	if got := columnTitles(withDone); got[4] != "Completed" {
		t.Errorf("with showDone the Completed column is back: %v", got)
	}
}

func TestBoardColumnsWithNoRowsShowTheStandardWorkflow(t *testing.T) {
	cols, _ := boardColumns(nil, nil, testWorkflows(), false)
	if got, want := columnTitles(cols), []string{"New", "In Progress", "On Hold"}; !reflect.DeepEqual(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

func TestGroupByStatusMakesASectionPerColumnWithRows(t *testing.T) {
	ref := testWorkflows()
	all, kept := rowsOf(store.Task{ID: "a", CustomStatusID: "S4"}, store.Task{ID: "b", CustomStatusID: "S1"})
	cols, _ := boardColumns(all, kept, ref, false)
	groups := groupByStatus(cols)
	if got, want := titlesOf(groups), []string{"New", "On Hold"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sections = %v, want %v", got, want)
	}
}

func TestStepStatusWalksTheWorkflowAndSkipsHidden(t *testing.T) {
	ref := testWorkflows()
	cs, known, ok := stepStatus(store.Task{CustomStatusID: "S2"}, ref, +1)
	if !known || !ok || cs.ID != "S4" {
		t.Errorf("next after In Progress = %+v, %v, %v, want On Hold (Secret is hidden)", cs, known, ok)
	}
	if _, _, ok := stepStatus(store.Task{CustomStatusID: "S1"}, ref, -1); ok {
		t.Error("there is nothing before New")
	}
	if _, known, _ := stepStatus(store.Task{CustomStatusID: "??"}, ref, +1); known {
		t.Error("an unknown status has no workflow to walk")
	}
}

func TestElsewhereStaysLastWhenAFolderSitsUnderTwoParents(t *testing.T) {
	// F1 is drawn twice, under S1 and under F2, so the flat node slice is longer than the number of folders.
	idx := newFolderIndex([]treeNode{
		{id: "me", title: "My tasks", kind: nodeMe},
		{id: "S1", title: "Platform", kind: nodeSpace, children: []int{2, 3}},
		{id: "F1", title: "Auth", kind: nodeFolder, depth: 1},
		{id: "F2", title: "Web", kind: nodeFolder, depth: 1, children: []int{4}},
		{id: "F1", title: "Auth", kind: nodeFolder, depth: 2},
	})
	all, kept := rowsOf(
		store.Task{ID: "lost", ParentIDs: []string{"X"}},
		store.Task{ID: "auth", ParentIDs: []string{"F1"}},
	)
	groups := groupByFolder(all, kept, store.ScopeKindMe, idx)
	if got := titlesOf(groups); len(got) != 2 || got[1] != "Elsewhere" {
		t.Errorf("sections = %v, Elsewhere belongs last", got)
	}
}

// loadTree appends a node per parent, so a folder under two parents is in the slice twice,
// and the index has to answer for both parents or a move out of the second one is missed.
func sharedTree() []treeNode {
	return []treeNode{
		{id: "PLT", title: "Platform", kind: nodeSpace, children: []int{1, 3}},
		{id: "API", title: "API", kind: nodeProject, depth: 1, children: []int{2}},
		{id: "SHR", title: "Shared", kind: nodeFolder, depth: 2},
		{id: "DSG", title: "Design system", kind: nodeFolder, depth: 1, children: []int{4}},
		{id: "SHR", title: "Shared", kind: nodeFolder, depth: 2},
	}
}

func TestFolderIndexKeepsEveryParent(t *testing.T) {
	idx := newFolderIndex(sharedTree())
	for _, tc := range []struct {
		folder, node string
		want         bool
	}{{"SHR", "API", true}, {"SHR", "DSG", true}, {"SHR", "PLT", true}, {"API", "DSG", false}} {
		if got := idx.under(tc.folder, tc.node); got != tc.want {
			t.Errorf("under(%s, %s) = %v, want %v", tc.folder, tc.node, got, tc.want)
		}
	}
	for _, tc := range []struct {
		folder, node, want string
		ok                 bool
	}{{"SHR", "DSG", "SHR", true}, {"SHR", "API", "SHR", true}, {"SHR", "PLT", "API", true}, {"API", "DSG", "PLT", false}} {
		got, ok := idx.sectionFor(tc.folder, tc.node)
		if got != tc.want || ok != tc.ok {
			t.Errorf("sectionFor(%s, %s) = %q, %v, want %q, %v", tc.folder, tc.node, got, ok, tc.want, tc.ok)
		}
	}
}
