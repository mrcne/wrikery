package ui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

// dropFolder replaces the cached tree without the folder, the way a sync after its deletion on Wrike does.
func dropFolder(t *testing.T, st *store.Store, id string) {
	t.Helper()
	ctx := context.Background()
	var kept []store.Folder
	for _, root := range []string{demo.SpacePlatform, demo.SpaceMobile} {
		sub, err := st.Folders().Subtree(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range sub {
			if f.ID == id {
				continue
			}
			f.ChildIDs = slices.DeleteFunc(f.ChildIDs, func(c string) bool { return c == id })
			kept = append(kept, f)
		}
	}
	if err := st.Folders().ReplaceTree(ctx, kept); err != nil {
		t.Fatal(err)
	}
}

// pinnedOnPlatform pins the Platform space and turns the pinned view on, with the cursor back on the API folder under it.
func pinnedOnPlatform(t *testing.T, m Model) Model {
	t.Helper()
	m = pump(m, focusMsg{pane: paneSidebar})
	m.sidebar.selectByID(demo.SpacePlatform)
	m = pump(m, tea.KeyMsg{Type: tea.KeySpace})
	m.sidebar.selectByID(demo.ProjectAPI)
	m = pump(m, typed("P"))
	if !m.sidebar.pinnedOnly || m.selectedNode.id != demo.ProjectAPI {
		t.Fatalf("pinned view %v on node %q, want the pinned view on API", m.sidebar.pinnedOnly, m.selectedNode.id)
	}
	return m
}

// A folder deleted on Wrike leaves the tree at the next sync while the user is in it.
// The sidebar goes to the nearest parent still there and the list shows that node, but the task in the detail pane stays,
// with the keys on it, and the bar says what moved.
// Only a move the user makes replaces the task in the pane.
func TestAFolderGoneFromTheTreeLeavesThePanesTask(t *testing.T) {
	t.Run("the task is not among the parent's rows", func(t *testing.T) {
		m := pinnedOnPlatform(t, demoModel(t))
		before := m.detail.task.ID
		if before == "" {
			t.Fatal("no task in the pane")
		}
		dropFolder(t, m.opts.Store, demo.ProjectAPI)
		m = pump(m, StoreChangedMsg{Entities: []string{"folders", "tasks"}})
		if n, _ := m.sidebar.current(); n.id != demo.SpacePlatform || m.selectedNode.id != demo.SpacePlatform || m.list.nodeID != demo.SpacePlatform {
			t.Errorf("sidebar on %q, node %q, list %q, want Platform in all three", n.id, m.selectedNode.id, m.list.nodeID)
		}
		if got, ok := m.selectedTask(); !ok || got.ID != before || m.detail.task.ID != before {
			t.Errorf("after the reload the keys act on %q and the pane shows %q, want %q", got.ID, m.detail.task.ID, before)
		}
		if m.focus != paneSidebar {
			t.Errorf("focus on %v, want it left on the sidebar the user was moving through", m.focus)
		}
		if !strings.Contains(m.status.toast, "API left the sidebar") {
			t.Errorf("toast = %q, want it to say that API left", m.status.toast)
		}
		if !m.sidebar.pinnedOnly {
			t.Error("the pinned view went off")
		}
	})
	t.Run("the task is among the parent's rows too", func(t *testing.T) {
		task := outsideTask("KUAAAAB1", demo.ProjectAPI, "IEAAWEB1")
		m := pinnedOnPlatform(t, demoModel(t, task))
		m = pump(m, taskSelectedMsg{id: task.ID})
		if m.detail.task.ID != task.ID {
			t.Fatalf("the pane shows %q, want %q", m.detail.task.ID, task.ID)
		}
		dropFolder(t, m.opts.Store, demo.ProjectAPI)
		m = pump(m, StoreChangedMsg{Entities: []string{"folders", "tasks"}})
		if cur, ok := m.list.current(); !ok || cur.task.ID != task.ID {
			t.Errorf("the list is on %q, want the pane's task %q, which Platform lists through Web", cur.task.ID, task.ID)
		}
		if got, ok := m.selectedTask(); !ok || got.ID != task.ID || m.detail.task.ID != task.ID {
			t.Errorf("after the reload the keys act on %q and the pane shows %q, want %q", got.ID, m.detail.task.ID, task.ID)
		}
	})
	// On the list the highlighted row would be the wrong task, so the pane takes the focus.
	// When that task then goes from the cache, the focus goes back to the list and its row fills the pane,
	// an empty pane with the focus would answer every key with "no task selected".
	t.Run("from the list the pane takes the focus and gives it back when the task goes", func(t *testing.T) {
		m := pump(pinnedOnPlatform(t, demoModel(t)), focusMsg{pane: paneList})
		before := m.detail.task.ID
		dropFolder(t, m.opts.Store, demo.ProjectAPI)
		m = pump(m, StoreChangedMsg{Entities: []string{"folders", "tasks"}})
		if got, ok := m.selectedTask(); !ok || got.ID != before || m.focus != paneDetail {
			t.Fatalf("after the reload the keys act on %q with the focus on %v, want %q in the pane with the focus", got.ID, m.focus, before)
		}
		m = pump(m, taskGoneMsg{id: before})
		cur, ok := m.list.current()
		if !ok {
			t.Fatal("the list shows no row")
		}
		if m.focus != paneList || m.selectedTaskID != cur.task.ID || m.detail.task.ID != cur.task.ID {
			t.Errorf("focus on %v, selection %q, pane %q, want the list with its row %q", m.focus, m.selectedTaskID, m.detail.task.ID, cur.task.ID)
		}
	})
}

// A sync emits the folders with the reference data, so the tree is loaded twice, once at the change and once after ref lands.
// The second load finds the cursor on the parent already and reads its rows again, and that read can land before
// the one the move started. The pane keeps its task whichever lands first.
func TestAFolderGoneFromTheTreeHoldsThePaneAgainstARacingLoad(t *testing.T) {
	m := pinnedOnPlatform(t, demoModel(t))
	before := m.detail.task.ID
	dropFolder(t, m.opts.Store, demo.ProjectAPI)
	first := collect(m.loadTree(false))
	next, moved := m.Update(first[0])
	m = next.(Model)
	if m.selectedNode.id != demo.SpacePlatform {
		t.Fatalf("node %q after the tree load, want Platform", m.selectedNode.id)
	}
	m = pump(m, collect(m.loadTree(false))...)
	if got, ok := m.selectedTask(); !ok || got.ID != before || m.detail.task.ID != before {
		t.Errorf("after the second load the keys act on %q and the pane shows %q, want %q", got.ID, m.detail.task.ID, before)
	}
	m = pump(m, collect(moved)...)
	if got, ok := m.selectedTask(); !ok || got.ID != before || m.detail.task.ID != before {
		t.Errorf("after the move's load the keys act on %q and the pane shows %q, want %q", got.ID, m.detail.task.ID, before)
	}
	if m.list.nodeID != demo.SpacePlatform {
		t.Errorf("list on %q, want Platform", m.list.nodeID)
	}
}

// Unfollowing the space in view is the user's move, not a sync's: My tasks takes the list and the pane with its first row,
// and the toast of the save stays.
func TestUnfollowingTheSpaceInViewMovesThePaneWithTheList(t *testing.T) {
	m := demoModel(t)
	ctx := context.Background()
	scopes, err := m.opts.Store.Scopes().Followed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scopes = slices.DeleteFunc(scopes, func(sc store.Scope) bool { return sc.ID == demo.SpacePlatform })
	if err := m.opts.Store.Scopes().SetFollowed(ctx, scopes); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(scopesSavedMsg{scopes: scopes, toast: "saved"})
	m = next.(Model)
	m = pump(m, collect(m.loadTree(true))...)
	cur, ok := m.list.current()
	if m.list.nodeID != store.ScopeKindMe || !ok {
		t.Fatalf("list on %q with a row %v, want My tasks with its rows", m.list.nodeID, ok)
	}
	if m.selectedTaskID != cur.task.ID || m.detail.task.ID != cur.task.ID {
		t.Errorf("selection %q, pane %q, want the list's row %q", m.selectedTaskID, m.detail.task.ID, cur.task.ID)
	}
	if m.status.toast != "saved" {
		t.Errorf("toast = %q, want the save's own", m.status.toast)
	}
}
