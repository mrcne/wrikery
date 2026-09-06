package ui

import (
	"context"
	"errors"
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
