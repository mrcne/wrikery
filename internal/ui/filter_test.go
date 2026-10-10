package ui

import (
	"reflect"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func setOf(keys ...string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func testFilterRef() refData {
	ref := testWorkflows()
	ref.meID = "ME"
	ref.contacts = map[string]store.Contact{
		"ME": {ID: "ME", FirstName: "Ada", LastName: "Nowak"},
		"B1": {ID: "B1", FirstName: "Bartek", LastName: "Lis"},
	}
	return ref
}

func TestRowFilterMatchesImportancePersonAndStatus(t *testing.T) {
	ref := testFilterRef()
	high := store.Task{Importance: "High", CustomStatusID: "S2", ResponsibleIDs: []string{"B1", "ME"}}
	normal := store.Task{Importance: "Normal", CustomStatusID: "S1", ResponsibleIDs: []string{"B1"}}
	nobody := store.Task{Importance: "Low", CustomStatusID: "S2"}
	cases := []struct {
		name   string
		filter rowFilter
		want   []bool // high, normal, nobody
	}{
		{"empty takes all", rowFilter{}, []bool{true, true, true}},
		{"importance", rowFilter{importance: setOf("High", "Low")}, []bool{true, false, true}},
		{"person among several", rowFilter{person: "ME"}, []bool{true, false, false}},
		{"unassigned", rowFilter{person: unassigned}, []bool{false, false, true}},
		{"status by name", rowFilter{statuses: setOf("in progress")}, []bool{true, false, true}},
		{"all three", rowFilter{importance: setOf("High"), person: "ME", statuses: setOf("in progress")}, []bool{true, false, false}},
	}
	for _, c := range cases {
		got := []bool{c.filter.matches(high, ref), c.filter.matches(normal, ref), c.filter.matches(nobody, ref)}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: matches = %v, want %v", c.name, got, c.want)
		}
		if c.filter.active() != (c.name != "empty takes all") {
			t.Errorf("%s: active = %v", c.name, c.filter.active())
		}
	}
}

// Every workflow has its own ids and the same names come up in several, so a name ticked once covers them all,
// and a task whose status the cache does not know is matched by the group its row shows.
func TestRowFilterMatchesAStatusAcrossWorkflowsAndByGroupWhenUnknown(t *testing.T) {
	ref := testFilterRef()
	ref.workflows = append(ref.workflows, store.Workflow{ID: "W3", Name: "Other", CustomStatuses: []store.CustomStatus{{ID: "O2", Name: "IN PROGRESS", Group: "Active"}}})
	ref.statuses["O2"] = ref.workflows[2].CustomStatuses[0]
	f := rowFilter{statuses: setOf("in progress")}
	if !f.matches(store.Task{CustomStatusID: "O2"}, ref) {
		t.Error("In Progress ticked should take the other workflow's IN PROGRESS")
	}
	g := rowFilter{statuses: setOf("active")}
	if !g.matches(store.Task{CustomStatusID: "??", Status: "Active"}, ref) || g.matches(store.Task{CustomStatusID: "S2"}, ref) {
		t.Error("an unknown status is matched by its group and a known one by its name")
	}
}

func TestRowFilterSummaryNamesWhatItShowsInOrder(t *testing.T) {
	ref := testFilterRef()
	cases := []struct {
		filter rowFilter
		want   string
	}{
		{rowFilter{}, ""},
		{rowFilter{importance: setOf("Normal", "High")}, "High, Normal"},
		{rowFilter{person: "ME"}, "Ada Nowak (me)"},
		{rowFilter{person: "B1"}, "Bartek Lis"},
		{rowFilter{person: unassigned}, "Unassigned"},
		{rowFilter{statuses: setOf("planned", "in progress", "new", "gone")}, "New, In Progress, Planned, gone"},
		{rowFilter{importance: setOf("High"), person: "ME", statuses: setOf("in progress")}, "High | Ada Nowak (me) | In Progress"},
	}
	for _, c := range cases {
		if got := c.filter.summary(ref); got != c.want {
			t.Errorf("summary = %q, want %q", got, c.want)
		}
	}
}

func TestRowFilterWithDoneAddsAndRemovesTheDoneStatuses(t *testing.T) {
	ref := testFilterRef()
	inView := ref.workflows[:1] // Default Workflow, whose done status is Completed
	f := rowFilter{statuses: setOf("in progress")}
	if f.hasDone(ref) {
		t.Fatal("In Progress alone counts as done")
	}
	on := f.withDone(ref, inView, true)
	if !on.hasDone(ref) || !on.statuses["completed"] || !on.statuses["in progress"] || on.statuses["done"] {
		t.Errorf("z on should add Completed of the workflow in view and keep In Progress: %v", on.statuses)
	}
	if f.statuses["completed"] {
		t.Error("withDone changed the filter it was called on")
	}
	off := on.withDone(ref, inView, false)
	if off.hasDone(ref) || !off.statuses["in progress"] {
		t.Errorf("z off should take Completed out and keep In Progress: %v", off.statuses)
	}
	// A done status of another workflow goes too, z off means no done status at all.
	other := rowFilter{statuses: setOf("in progress", "done")}.withDone(ref, inView, false)
	if other.statuses["done"] {
		t.Errorf("Done of the other workflow stayed: %v", other.statuses)
	}
	if only := (rowFilter{statuses: setOf("completed")}).withDone(ref, inView, false); only.active() {
		t.Error("a set with nothing left is no filter")
	}
}
