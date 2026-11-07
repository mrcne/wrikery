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
// with the keys on it, and the bar says what moved. Only a move the user makes replaces the task in the pane.
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
		if m.focus != paneDetail {
			t.Errorf("focus on %v, want the pane, the open and copy keys read the list's row otherwise", m.focus)
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
}
