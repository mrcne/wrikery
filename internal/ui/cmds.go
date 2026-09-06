package ui

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

func (m Model) loadRef() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		ref := refData{contacts: map[string]store.Contact{}, statuses: map[string]store.CustomStatus{}}
		contacts, err := st.Contacts().List(ctx)
		if err != nil {
			return errMsg{err}
		}
		for _, c := range contacts {
			ref.contacts[c.ID] = c
		}
		ref.workflows, err = st.Workflows().List(ctx)
		if err != nil {
			return errMsg{err}
		}
		for _, w := range ref.workflows {
			for _, cs := range w.CustomStatuses {
				ref.statuses[cs.ID] = cs
			}
		}
		ref.meID, err = st.GetMeta(ctx, store.MetaKeyMe)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return errMsg{err}
		}
		return refLoadedMsg{ref: ref}
	}
}

func (m Model) loadScopes() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		scopes, err := st.Scopes().Followed(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return scopesLoadedMsg{scopes: scopes}
	}
}

// loadPicker reads spaces and the projects right under each space root for the first run checklist.
func (m Model) loadPicker() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		spaces, err := st.Spaces().List(ctx)
		if err != nil {
			return errMsg{err}
		}
		out := pickerLoadedMsg{spaces: spaces, projects: map[string][]store.Folder{}}
		for _, sp := range spaces {
			children, err := st.Folders().Children(ctx, sp.ID)
			if err != nil {
				return errMsg{err}
			}
			for _, f := range children {
				if f.Project != nil {
					out.projects[sp.ID] = append(out.projects[sp.ID], f)
				}
			}
		}
		return out
	}
}

func (m Model) saveScopes(selected []store.Scope) tea.Cmd {
	st, hooks := m.opts.Store, m.opts.Hooks
	return func() tea.Msg {
		ctx := context.Background()
		all := append([]store.Scope{{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true}}, selected...)
		for _, sc := range all {
			if err := st.Scopes().Upsert(ctx, sc); err != nil {
				return errMsg{err}
			}
		}
		if hooks.Refresh != nil {
			hooks.Refresh()
		}
		scopes, err := st.Scopes().Followed(ctx)
		if err != nil {
			return errMsg{err}
		}
		return scopesLoadedMsg{scopes: scopes}
	}
}

// loadTree builds the sidebar from the followed scopes: My tasks first,
// then each followed space's subtree, then followed projects that are not inside a followed space.
func (m Model) loadTree() tea.Cmd {
	st, meID := m.opts.Store, m.ref.meID
	return func() tea.Msg {
		ctx := context.Background()
		scopes, err := st.Scopes().Followed(ctx)
		if err != nil {
			return errMsg{err}
		}
		statuses := m.ref.statuses
		var nodes []treeNode
		covered := map[string]bool{}
		open := 0
		if meID != "" {
			mine, err := st.Tasks().ListForResponsible(ctx, meID)
			if err != nil {
				return errMsg{err}
			}
			for _, t := range mine {
				if !isDone(t) {
					open++
				}
			}
		}
		nodes = append(nodes, treeNode{id: store.ScopeKindMe, title: "My tasks", kind: nodeMe, count: open})
		addSubtree := func(rootID string, rootKind nodeKind) error {
			folders, err := st.Folders().Subtree(ctx, rootID)
			if err != nil {
				return err
			}
			if len(folders) == 0 {
				// A followed space whose id is not also a folder id has no tree to draw, and dropping it silently looks like a sync bug.
				slog.Warn("followed scope has no folder tree", "scope", rootID)
				return nil
			}
			byID := map[string]store.Folder{}
			for _, f := range folders {
				byID[f.ID] = f
				covered[f.ID] = true
			}
			var add func(f store.Folder, depth int) int
			add = func(f store.Folder, depth int) int {
				idx := len(nodes)
				n := treeNode{id: f.ID, title: f.Title, kind: nodeFolder, depth: depth}
				if depth == 0 {
					n.kind, n.expanded = rootKind, true
				}
				if f.Project != nil {
					n.kind = nodeProject
					n.statusGroup = statuses[f.Project.CustomStatusID].Group
				}
				nodes = append(nodes, n)
				children := make([]store.Folder, 0, len(f.ChildIDs))
				for _, cid := range f.ChildIDs {
					if c, ok := byID[cid]; ok {
						children = append(children, c)
					}
				}
				sort.Slice(children, func(i, j int) bool { return children[i].Title < children[j].Title })
				for _, c := range children {
					// nodes grows while we recurse, so index by idx and never hold a pointer into the slice.
					nodes[idx].children = append(nodes[idx].children, add(c, depth+1))
				}
				return idx
			}
			add(folders[0], 0)
			return nil
		}
		for _, sc := range scopes {
			if sc.Kind == store.ScopeKindSpace {
				// The space root folder id is assumed equal to the space id, the demo data is built that way.
				if err := addSubtree(sc.ID, nodeSpace); err != nil {
					return errMsg{err}
				}
			}
		}
		for _, sc := range scopes {
			if sc.Kind == store.ScopeKindProject && !covered[sc.ID] {
				if err := addSubtree(sc.ID, nodeProject); err != nil {
					return errMsg{err}
				}
			}
		}
		return treeLoadedMsg{nodes: nodes}
	}
}

func isDone(t store.Task) bool { return t.Status == "Completed" || t.Status == "Cancelled" }

func (m Model) loadTasks(node treeNode, crumb string) tea.Cmd {
	st, meID := m.opts.Store, m.ref.meID
	return func() tea.Msg {
		ctx := context.Background()
		var tasks []store.Task
		var err error
		if node.kind == nodeMe {
			tasks, err = st.Tasks().ListForResponsible(ctx, meID)
		} else {
			tasks, err = st.Tasks().ListInFolder(ctx, node.id)
		}
		if err != nil {
			return errMsg{err}
		}
		states, err := st.Outbox().StatesByEntity(ctx)
		if err != nil {
			return errMsg{err}
		}
		return tasksLoadedMsg{nodeID: node.id, crumb: crumb, tasks: tasks, states: states}
	}
}

// loadTask reads one task and its thread. It runs on every cursor move, so it only reads.
// Recording that a task was opened is markOpened's job.
func (m Model) loadTask(id string) tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		task, err := st.Tasks().Get(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		comments, err := st.Comments().ListForTask(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		logs, err := st.Timelogs().ListForTask(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		states, err := st.Outbox().StatesByEntity(ctx)
		if err != nil {
			return errMsg{err}
		}
		// A task can sit in more than one folder, the detail names them all.
		var crumbs []string
		for _, pid := range task.ParentIDs {
			if f, err := st.Folders().Get(ctx, pid); err == nil {
				crumbs = append(crumbs, f.Title)
			}
		}
		return taskLoadedMsg{task: task, comments: comments, logs: logs, states: states, crumb: strings.Join(crumbs, ", ")}
	}
}

// markOpened records that the reader opened the task, which is what puts it on the syncer's list of threads to refresh.
// It runs on the deliberate open, not on the cursor preview, or walking a folder would queue a refresh for every task in it.
// Nothing on screen waits for it, so a failure is logged and never shown.
func (m Model) markOpened(id string) tea.Cmd {
	st, now := m.opts.Store, m.opts.Now
	return func() tea.Msg {
		if err := st.Tasks().MarkOpened(context.Background(), id, now().UTC().Format(time.RFC3339)); err != nil {
			slog.Warn("mark opened", "task", id, "error", err)
		}
		return nil
	}
}

// runSearch reads the crumb for each hit's first parent, the same folder title the list pane shows.
func (m Model) runSearch(seq int, query string) tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		tasks, err := st.Tasks().Search(ctx, query, 30)
		if err != nil {
			return errMsg{err}
		}
		crumbs := map[string]string{}
		for _, t := range tasks {
			if len(t.ParentIDs) > 0 {
				if f, err := st.Folders().Get(ctx, t.ParentIDs[0]); err == nil {
					crumbs[t.ID] = f.Title
				}
			}
		}
		return searchResultsMsg{seq: seq, tasks: tasks, crumbs: crumbs}
	}
}

// enqueue runs one outbox call, wakes the engine and reports back. Every write in the UI goes through here.
// The counts are read right after, so the status bar reflects the new row without waiting for the
// next OutboxChangedMsg from the sync engine.
func (m Model) enqueue(op func(ctx context.Context) error, toast string) tea.Cmd {
	st, hooks := m.opts.Store, m.opts.Hooks
	return func() tea.Msg {
		ctx := context.Background()
		if err := op(ctx); err != nil {
			return errMsg{err}
		}
		if hooks.WakeOutbox != nil {
			hooks.WakeOutbox()
		}
		pending, failed, err := st.Outbox().Counts(ctx)
		if err != nil {
			return errMsg{err}
		}
		return writeQueuedMsg{toast: toast, pending: pending, failed: failed}
	}
}

// loadCounts reads the outbox pending and failed counts once at startup.
// Without this, a demo store's seeded failures only reach the status bar on the first OutboxChangedMsg,
// which never comes in demo mode, so the bar would start blank instead of showing what was seeded.
func (m Model) loadCounts() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		pending, failed, err := st.Outbox().Counts(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return OutboxChangedMsg{Pending: pending, Failed: failed}
	}
}

// reloadCurrent re-reads the task and the list a write may have changed the outbox state of.
// The zero node has no folder to list, the same guard reload and OutboxChangedMsg use before the first selection.
func (m Model) reloadCurrent() tea.Cmd {
	cmds := []tea.Cmd{m.reloadTask()}
	if m.selectedNode.kind != nodeNone {
		cmds = append(cmds, m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode)))
	}
	return tea.Batch(cmds...)
}

// loadIssues reads the failed outbox rows for the sync issues screen.
// A timelog edit or delete names the timelog as its entity, so its task is looked up through the
// cached row, which is only there while nothing has evicted it yet.
func (m Model) loadIssues() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		failed, err := st.Outbox().ListFailed(ctx)
		if err != nil {
			return errMsg{err}
		}
		var rows []issueRow
		for _, r := range failed {
			ir := issueRow{row: r, summary: summarize(r)}
			switch r.Kind {
			case store.KindTimelogUpdate, store.KindTimelogDelete:
				if l, err := st.Timelogs().Get(ctx, r.EntityID); err == nil {
					ir.taskID = l.TaskID
				}
			default:
				ir.taskID = r.EntityID
			}
			if ir.taskID != "" {
				if t, err := st.Tasks().Get(ctx, ir.taskID); err == nil {
					ir.title = t.Title
					if len(t.ParentIDs) > 0 {
						ir.parentID = t.ParentIDs[0]
					}
				} else {
					ir.title = "(task " + ir.taskID + ")"
				}
			} else {
				ir.title = "(time entry " + r.EntityID + ")"
			}
			rows = append(rows, ir)
		}
		return issuesLoadedMsg{rows: rows}
	}
}

// enqueueIssueOp runs a retry or discard for the issues screen. ErrNotFound means the engine
// already took the row inflight or somebody else cleared it, which is not a failure worth an
// error toast, just a sign the list is stale and needs another read.
func (m Model) enqueueIssueOp(op func(ctx context.Context) error, doneToast string) tea.Cmd {
	st, hooks := m.opts.Store, m.opts.Hooks
	return func() tea.Msg {
		ctx := context.Background()
		err := op(ctx)
		if errors.Is(err, store.ErrNotFound) {
			return writeQueuedMsg{toast: "already being sent, list refreshed"}
		}
		if err != nil {
			return errMsg{err}
		}
		if hooks.WakeOutbox != nil {
			hooks.WakeOutbox()
		}
		pending, failed, err := st.Outbox().Counts(ctx)
		if err != nil {
			return errMsg{err}
		}
		return writeQueuedMsg{toast: doneToast, pending: pending, failed: failed}
	}
}

// loadWeek reads the current user's timelogs for the seven days starting at start,
// along with the title of each task involved and the outbox state of each entry, for the pending marker.
// A zero start means the week the clock is in right now.
func (m Model) loadWeek(start time.Time) tea.Cmd {
	st, meID := m.opts.Store, m.ref.meID
	if start.IsZero() {
		start = weekOf(m.opts.Now())
	}
	return func() tea.Msg {
		ctx := context.Background()
		from, to := start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")
		logs, err := st.Timelogs().ListForUser(ctx, meID, from, to)
		if err != nil {
			return errMsg{err}
		}
		titles := map[string]string{}
		for _, l := range logs {
			if _, done := titles[l.TaskID]; done {
				continue
			}
			if t, err := st.Tasks().Get(ctx, l.TaskID); err == nil {
				titles[l.TaskID] = t.Title
			} else {
				titles[l.TaskID] = ""
			}
		}
		states, err := st.Outbox().StatesByEntity(ctx)
		if err != nil {
			return errMsg{err}
		}
		return weekLoadedMsg{weekStart: start, logs: logs, titles: titles, states: states}
	}
}

func (m Model) verifyToken(token string) tea.Cmd {
	verify := m.opts.Hooks.VerifyToken
	return func() tea.Msg {
		if verify == nil {
			return tokenVerifiedMsg{err: errors.New("token verification is not available")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		name, err := verify(ctx, token)
		return tokenVerifiedMsg{name: name, err: err}
	}
}
