package ui

import (
	"context"
	"errors"
	"sort"
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

// loadTree builds the sidebar from the followed scopes: My tasks first, then each followed space's
// subtree, then followed projects that are not inside a followed space.
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
