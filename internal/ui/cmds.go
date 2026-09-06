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
// markOpened is a hint for the syncer, which refreshes the threads of recently opened tasks.
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
