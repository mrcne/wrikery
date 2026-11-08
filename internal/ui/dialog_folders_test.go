package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// testTree is the demo's shape: two spaces, projects and folders under the first, one nested folder.
func testTree() []treeNode {
	return []treeNode{
		{id: store.ScopeKindMe, title: "My tasks", kind: nodeMe},
		{id: "MOB", title: "Mobile", kind: nodeSpace, children: []int{2}},
		{id: "IOS", title: "iOS app", kind: nodeProject, depth: 1},
		{id: "PLT", title: "Platform", kind: nodeSpace, children: []int{4, 5, 6}},
		{id: "API", title: "API", kind: nodeProject, depth: 1},
		{id: "DSG", title: "Design system", kind: nodeFolder, depth: 1},
		{id: "INF", title: "Infra", kind: nodeFolder, depth: 1, children: []int{7}},
		{id: "ONC", title: "On-call", kind: nodeFolder, depth: 2},
	}
}

func foldersDialogFor(task store.Task, nodeID string) dialog {
	nodes := testTree()
	d, _ := newFoldersDialog(task, nodes, nil, newFolderIndex(nodes), nodeID)
	return d
}

func TestFoldersDialogMovesOutOfTheFolderInView(t *testing.T) {
	dl := foldersDialogFor(store.Task{ID: "T", ParentIDs: []string{"ONC"}}, "INF")
	dl = typeRunes(dl, "des")
	view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40)
	if !strings.Contains(view, "enter moves to Design system, leaves On-call") {
		t.Errorf("the first line should say what enter does:\n%s", view)
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitFoldersMsg{taskID: "T", add: []string{"DSG"}, remove: []string{"ONC"}}
	if len(msgs) != 2 || !reflect.DeepEqual(msgs[0], want) {
		t.Errorf("enter -> %#v", msgs)
	}
}

func TestFoldersDialogOnlyAddsWhereNoFolderIsInView(t *testing.T) {
	dl := foldersDialogFor(store.Task{ID: "T", ParentIDs: []string{"API"}}, store.ScopeKindMe)
	dl = typeRunes(dl, "web")
	if view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40); !strings.Contains(view, "no folder matches") {
		t.Errorf("an empty match should say so:\n%s", view)
	}
	for range 3 {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	dl = typeRunes(dl, "ios")
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitFoldersMsg{taskID: "T", add: []string{"IOS"}}
	if len(msgs) != 2 || !reflect.DeepEqual(msgs[0], want) {
		t.Errorf("enter on My tasks -> %#v", msgs)
	}
}

func TestFoldersDialogAppliesTogglesInsteadOfMoving(t *testing.T) {
	dl := foldersDialogFor(store.Task{ID: "T", ParentIDs: []string{"API"}}, "PLT")
	dl = typeRunes(dl, "des")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	for range 3 {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	dl = typeRunes(dl, "inf")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	for range 3 {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40)
	if !strings.Contains(view, "enter applies 2 changes") || !strings.Contains(view, "[x] Design system") || !strings.Contains(view, "[x] Infra") {
		t.Errorf("toggled rows and the count should show:\n%s", view)
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitFoldersMsg{taskID: "T", add: []string{"DSG", "INF"}}
	if len(msgs) != 2 || !reflect.DeepEqual(msgs[0], want) {
		t.Errorf("enter with toggles -> %#v", msgs)
	}
}

func TestFoldersDialogKeepsTheLastFolder(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	dl := foldersDialogFor(store.Task{ID: "T", ParentIDs: []string{"API"}}, "PLT")
	dl = typeRunes(dl, "api")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	if view := dl.View(th, 60, 40); !strings.Contains(view, "a task needs a folder") || !strings.Contains(view, "[x] API") {
		t.Errorf("unchecking the only folder should be refused:\n%s", view)
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("enter on the folder the task is in with nothing to leave -> %#v, want a refusal", msgs)
	}
	if view := dl.View(th, 60, 40); !strings.Contains(view, "already in API") {
		t.Errorf("the refusal should name the folder:\n%s", view)
	}

	// A parent outside the followed scopes keeps the task placed, so the known one may go.
	dl = foldersDialogFor(store.Task{ID: "T", ParentIDs: []string{"API", "ZZZ"}}, "PLT")
	if view := dl.View(th, 60, 40); !strings.Contains(view, "and 1 folder outside the followed scopes") {
		t.Errorf("the unknown parent should be counted:\n%s", view)
	}
	dl = typeRunes(dl, "api")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitFoldersMsg{taskID: "T", remove: []string{"API"}}
	if len(msgs) != 2 || !reflect.DeepEqual(msgs[0], want) {
		t.Errorf("enter -> %#v", msgs)
	}
}

func TestFolderIndexUnder(t *testing.T) {
	idx := newFolderIndex(testTree())
	for _, tc := range []struct {
		folder, node string
		want         bool
	}{{"ONC", "INF", true}, {"ONC", "PLT", true}, {"INF", "INF", true}, {"API", "INF", false}, {"API", store.ScopeKindMe, false}, {"ZZZ", "PLT", false}} {
		if got := idx.under(tc.folder, tc.node); got != tc.want {
			t.Errorf("under(%s, %s) = %v, want %v", tc.folder, tc.node, got, tc.want)
		}
	}
}

func TestFoldersDialogListsAFolderUnderTwoParentsOnce(t *testing.T) {
	nodes := sharedTree()
	d, _ := newFoldersDialog(store.Task{ID: "T", ParentIDs: []string{"SHR"}}, nodes, nil, newFolderIndex(nodes), "DSG")
	if n := strings.Count(d.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40), "[x] Shared"); n != 1 {
		t.Errorf("Shared listed %d times, want once", n)
	}
	dl := typeRunes(dialog(d), "api")
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitFoldersMsg{taskID: "T", add: []string{"API"}, remove: []string{"SHR"}}
	if len(msgs) != 2 || !reflect.DeepEqual(msgs[0], want) {
		t.Errorf("enter -> %#v, want one remove of the folder reached through its second parent", msgs)
	}
}

func TestFoldersToastNamesTheFolder(t *testing.T) {
	titles := map[string]string{"A": "API", "D": "Design system"}
	for _, tc := range []struct {
		add, remove []string
		want        string
	}{
		{[]string{"D"}, nil, "Added to Design system"},
		{[]string{"D"}, []string{"A"}, "Moved to Design system"},
		{nil, []string{"A"}, "Removed from API"},
		{[]string{"D"}, []string{"A", "X"}, "Moved to Design system"},
		{[]string{"D", "A"}, nil, "Folders updated"},
	} {
		if got := foldersToast(tc.add, tc.remove, titles); got != tc.want {
			t.Errorf("toast(%v, %v) = %q, want %q", tc.add, tc.remove, got, tc.want)
		}
	}
}

func TestFoldersDialogClearsTheRefusalOnAMove(t *testing.T) {
	dl := foldersDialogFor(store.Task{ID: "T", ParentIDs: []string{"API"}}, "PLT")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyDown})
	if view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40); strings.Contains(view, "a task needs a folder") {
		t.Errorf("moving the cursor should bring the key hint back:\n%s", view)
	}
	dl = typeRunes(dl, "zzz")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40); !strings.Contains(view, "no folder matches") || strings.Contains(view, "already in") {
		t.Errorf("enter with no match should say so:\n%s", view)
	}
}

func TestFoldersDialogListsThePinnedFoldersFirst(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	if view := foldersDialogFor(store.Task{ID: "T", ParentIDs: []string{"API"}}, "PLT").View(th, 60, 40); strings.Contains(view, "Pinned") || strings.Contains(view, "All") {
		t.Errorf("without pins the box has no headers:\n%s", view)
	}
	nodes := testTree()
	pinned := map[string]bool{"ONC": true, "API": true, "GONE": true}
	// The task sits outside the followed scopes, so the cursor starts at the top, on the first pinned folder.
	dl, _ := newFoldersDialog(store.Task{ID: "T", ParentIDs: []string{"OUT"}}, nodes, pinned, newFolderIndex(nodes), "PLT")
	view := dl.View(th, 60, 40)
	// The pinned folders come first in tree order, without the tree's indent, then the whole tree keeps its shape.
	order := []string{"Pinned", "[ ] API", "[ ] On-call", "All", "[ ] Mobile", "  [ ] API", "    [ ] On-call"}
	at := 0
	for _, s := range order {
		i := strings.Index(view[at:], s)
		if i < 0 {
			t.Fatalf("%q missing after position %d:\n%s", s, at, view)
		}
		at += i + len(s)
	}
	if !strings.Contains(view, "enter adds to API") {
		t.Errorf("the first line should offer the pinned folder under the cursor:\n%s", view)
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitFoldersMsg{taskID: "T", add: []string{"API"}}
	if len(msgs) != 2 || !reflect.DeepEqual(msgs[0], want) {
		t.Errorf("enter -> %#v", msgs)
	}
}

func TestFoldersDialogCountsTheHeadersInItsHeight(t *testing.T) {
	nodes := []treeNode{{id: store.ScopeKindMe, title: "My tasks", kind: nodeMe}, {id: "S", title: "Space", kind: nodeSpace}}
	for i := range 20 {
		id := fmt.Sprintf("F%02d", i)
		nodes[1].children = append(nodes[1].children, len(nodes))
		nodes = append(nodes, treeNode{id: id, title: "Folder " + id, kind: nodeFolder, depth: 1})
	}
	pinned := map[string]bool{"F03": true, "F10": true}
	dl, _ := newFoldersDialog(store.Task{ID: "T", ParentIDs: []string{"OUT", "F03"}}, nodes, pinned, newFolderIndex(nodes), "S")
	view := dl.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 20)
	if h := lipgloss.Height(view); h > 20 {
		t.Errorf("the box is %d lines tall on a 20 line screen:\n%s", h, view)
	}
}
