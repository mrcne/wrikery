package ui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

// pump feeds the messages through Update and runs every command they produce, the way the program loop does,
// up to a queued write or a toast, so a test reads the model after the round trip.
// Those two end it because the toast they show ticks for three seconds before it clears.
// The reload a write starts (the list, the changes screen or the week) is dropped with the tick,
// it is what the engine's OutboxChangedMsg does again, and a test sends that one itself.
func pump(m Model, msgs ...tea.Msg) Model {
	for len(msgs) > 0 {
		var msg tea.Msg
		msg, msgs = msgs[0], msgs[1:]
		next, cmd := m.Update(msg)
		m = next.(Model)
		switch msg.(type) {
		case writeQueuedMsg, toastMsg:
			continue
		}
		msgs = append(msgs, collect(cmd)...)
	}
	return m
}

// outsideFolder is a folder id the cache has no folder for, so the sidebar cannot show it.
const outsideFolder = "IEAAELSE"

// demoModel is the root on the demo store with the API folder on screen, the keys on the sidebar.
// The extra tasks sit in the cache beside the demo's, as a task from an unfollowed space does, a search finds them.
func demoModel(t *testing.T, extra ...store.Task) Model {
	t.Helper()
	opts := rootTestOptions(t)
	ctx := context.Background()
	opts.Store.Now = opts.Now
	if err := demo.Seed(ctx, opts.Store, opts.Now()); err != nil {
		t.Fatal(err)
	}
	if err := opts.Store.Tasks().Upsert(ctx, extra); err != nil {
		t.Fatal(err)
	}
	m := New(opts)
	m = pump(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m = pump(m, collect(m.Init())...)
	if !m.sidebar.reveal(demo.ProjectAPI) {
		t.Fatal("the API folder is not in the tree")
	}
	n, _ := m.sidebar.current()
	m = pump(m, nodeSelectedMsg{node: n})
	if m.selectedNode.id != demo.ProjectAPI || len(m.list.rows) == 0 {
		t.Fatalf("the API folder did not load: node %q with %d rows", m.selectedNode.id, len(m.list.rows))
	}
	return m
}

// outsideTask is a task in folders the sidebar does not show, as a task from an unfollowed space is in the cache.
func outsideTask(responsible string, parents ...string) store.Task {
	return store.Task{
		ID: "IEAAOUT01", Title: "Rotate the signing key", Status: "Active", CustomStatusID: "IEAAST01", Importance: "Normal",
		ResponsibleIDs: []string{responsible}, ParentIDs: parents,
		Permalink: "https://www.wrike.com/open.htm?id=1299001", CreatedDate: "2026-09-01T09:00:00Z", UpdatedDate: "2026-09-01T09:00:00Z",
	}
}

// A task found by the search can sit in a folder the sidebar does not show.
// The reload after a sync and the one after an outbox change reread the folder left behind,
// and neither may put that folder's row in the pane, the keys would then act on the wrong task.
func TestJumpOutsideTheTreeSurvivesTheReloads(t *testing.T) {
	for name, reload := range map[string]tea.Msg{
		"sync":   StoreChangedMsg{Entities: []string{"tasks"}},
		"outbox": OutboxChangedMsg{Pending: 1},
	} {
		t.Run(name, func(t *testing.T) {
			task := outsideTask("KUAAAAB1", outsideFolder)
			m := demoModel(t, task)
			m = pump(m, openTaskMsg{task: task})
			if got, ok := m.selectedTask(); !ok || got.ID != task.ID {
				t.Fatalf("after the jump the keys act on %q, want %q", got.ID, task.ID)
			}
			m = pump(m, reload)
			if got, ok := m.selectedTask(); !ok || got.ID != task.ID {
				t.Errorf("after the reload the keys act on %q, want %q", got.ID, task.ID)
			}
			if m.detail.task.ID != task.ID {
				t.Errorf("after the reload the pane shows %q, want %q", m.detail.task.ID, task.ID)
			}
		})
	}
}

// A task in several folders lands in the first one the sidebar shows, not only in the first folder it has.
func TestJumpLandsInALaterFolderOfTheTask(t *testing.T) {
	task := outsideTask("KUAAAAB1", outsideFolder, "IEAAWEB1")
	m := demoModel(t, task)
	m = pump(m, openTaskMsg{task: task})
	if m.selectedNode.id != "IEAAWEB1" {
		t.Errorf("the jump landed on node %q, want the Web folder", m.selectedNode.id)
	}
	if cur, ok := m.list.current(); !ok || cur.task.ID != task.ID {
		t.Errorf("the list is on %q, want %q", cur.task.ID, task.ID)
	}
}

// The user's own task outside every shown folder lands in My tasks, the one node that lists it.
func TestJumpToOwnTaskOutsideTheTreeLandsInMyTasks(t *testing.T) {
	task := outsideTask(demo.MeID, outsideFolder)
	m := demoModel(t, task)
	m = pump(m, openTaskMsg{task: task})
	if m.selectedNode.kind != nodeMe {
		t.Errorf("the jump landed on node %q, want My tasks", m.selectedNode.id)
	}
	if cur, ok := m.list.current(); !ok || cur.task.ID != task.ID {
		t.Errorf("the list is on %q, want %q", cur.task.ID, task.ID)
	}
	m = pump(m, StoreChangedMsg{Entities: []string{"tasks"}})
	if got, ok := m.selectedTask(); !ok || got.ID != task.ID {
		t.Errorf("after the reload the keys act on %q, want %q", got.ID, task.ID)
	}
}

// Once the keys go to the list, the action keys read the row under its cursor,
// so the pane leaves a task the list does not hold for that row, the same way a move in the list would.
func TestFocusOnTheListLeavesAHeldTask(t *testing.T) {
	for name, key := range map[string]tea.KeyMsg{
		"h":         {Type: tea.KeyRunes, Runes: []rune("h")},
		"shift+tab": {Type: tea.KeyShiftTab},
	} {
		t.Run(name, func(t *testing.T) {
			task := outsideTask("KUAAAAB1", outsideFolder)
			m := demoModel(t, task)
			m = pump(m, openTaskMsg{task: task})
			m = pump(m, key)
			if m.focus != paneList {
				t.Fatalf("focus %v, want the list", m.focus)
			}
			cur, ok := m.list.current()
			if !ok {
				t.Fatal("the list is empty")
			}
			if got, ok := m.selectedTask(); !ok || got.ID != cur.task.ID {
				t.Errorf("the keys act on %q, want the list's row %q", got.ID, cur.task.ID)
			}
			if m.detail.task.ID != cur.task.ID {
				t.Errorf("the pane shows %q, want the list's row %q", m.detail.task.ID, cur.task.ID)
			}
		})
	}
}

// The crumb under a search result names the first of the task's folders the cache holds.
func TestSearchCrumbNamesAKnownFolder(t *testing.T) {
	task := outsideTask("KUAAAAB1", outsideFolder, "IEAAWEB1")
	m := demoModel(t, task)
	msgs := collect(m.runSearch(1, "signing"))
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want the results", len(msgs))
	}
	res, ok := msgs[0].(searchResultsMsg)
	if !ok {
		t.Fatalf("got %T, want searchResultsMsg", msgs[0])
	}
	if res.crumbs[task.ID] != "Web" {
		t.Errorf("crumb = %q, want Web", res.crumbs[task.ID])
	}
}

// A link, a number or an id typed into the search is tried in the cache first, and named as a reach when the cache lacks it.
func TestRunSearchPutsACachedReferenceFirstAndNamesAnUncachedOne(t *testing.T) {
	m := demoModel(t)
	res := m.runSearch(1, "https://www.wrike.com/open.htm?id=1200000")().(searchResultsMsg)
	if len(res.tasks) == 0 || res.tasks[0].Permalink != "https://www.wrike.com/open.htm?id=1200000" || res.ref != "" {
		t.Errorf("a cached link gave %+v, ref %q, want the task first and no reach", res.tasks, res.ref)
	}
	res = m.runSearch(2, " MAAAAAEPXpuV ")().(searchResultsMsg)
	if len(res.tasks) != 0 || res.ref != "MAAAAAEPXpuV" {
		t.Errorf("an uncached id gave %+v, ref %q, want no hits and the trimmed reference", res.tasks, res.ref)
	}
	res = m.runSearch(3, "fix auth")().(searchResultsMsg)
	if len(res.tasks) == 0 || res.ref != "" {
		t.Errorf("a fragment gave %+v, ref %q, want hits and no reach", res.tasks, res.ref)
	}
}
