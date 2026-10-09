package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

// submitScopesMsg carries the whole followed set, My tasks left out, and the toast for the status bar.
type submitScopesMsg struct {
	scopes []store.Scope
	toast  string
}

// scopesDialog is the checklist of spaces and projects to follow, opened from the settings screen.
// It lists what the first run lists, the followed ones checked, and enter hands over the full set rather than a diff,
// so the store write replaces the followed set in one go.
type scopesDialog struct {
	items []pickerItem
	list  checklist
}

// newScopesDialog builds the box from the picker's items with the followed set ticked.
// A followed scope the picker does not list, a project moved deeper into a folder for example, gets a row at the end,
// ticked, so the box counts it and drops it only when it is unticked, never silently on the next apply.
func newScopesDialog(items []pickerItem, followed []store.Scope) (scopesDialog, tea.Cmd) {
	listed := map[string]bool{}
	for _, it := range items {
		listed[it.scope.ID] = true
	}
	items = append([]pickerItem(nil), items...)
	for _, sc := range followed {
		if sc.Followed && sc.Kind != store.ScopeKindMe && !listed[sc.ID] {
			items = append(items, pickerItem{scope: sc, checked: true})
		}
	}
	var rows []checkRow
	var checked []string
	for _, it := range items {
		if it.disabled {
			continue
		}
		rows = append(rows, checkRow{id: it.scope.ID, label: strings.Repeat("  ", it.depth) + it.scope.Title})
		if it.checked {
			checked = append(checked, it.scope.ID)
		}
	}
	list, cmd := newChecklist(rows, checked, "type a space or project name")
	return scopesDialog{items: items, list: list}, cmd
}

// reload rebuilds the box from a fresh picker, the spaces can land while it is open after a cache reset.
// The ticks and the typed text stay as they are, the followed set is the one the fresh read gives.
func (d scopesDialog) reload(msg pickerLoadedMsg) (scopesDialog, tea.Cmd) {
	ticked := map[string]bool{}
	for id, on := range d.list.checked {
		ticked[id] = on
	}
	next, cmd := newScopesDialog(pickerItems(msg, ticked), msg.followed)
	// The rows are built from the ticks, so the baseline the plan counts against is set back to the followed set.
	next.list.checked, next.list.original = ticked, map[string]bool{}
	for _, sc := range msg.followed {
		if sc.Followed && sc.Kind != store.ScopeKindMe {
			next.list.original[sc.ID] = true
		}
	}
	next.list.filter.SetValue(d.list.filter.Value())
	next.list.applyFilter()
	return next, cmd
}

func (d scopesDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.Type {
	case tea.KeySpace:
		d.list.toggle()
		return d, nil
	case tea.KeyEnter:
		add, remove := d.list.diff()
		if len(add) == 0 && len(remove) == 0 {
			return d, intent(closeDialogMsg{})
		}
		var scopes []store.Scope
		titles := map[string]string{}
		for _, it := range d.items {
			titles[it.scope.ID] = it.scope.Title
			if !it.disabled && d.list.checked[it.scope.ID] {
				scopes = append(scopes, it.scope)
			}
		}
		return d, tea.Batch(intent(submitScopesMsg{scopes: scopes, toast: scopesToast(add, remove, titles)}), intent(closeDialogMsg{}))
	}
	return d, d.list.update(msg)
}

// plan is the first line of the box: what enter does right now.
func (d scopesDialog) plan() string {
	add, remove := d.list.diff()
	switch {
	case len(add) > 0 && len(remove) > 0:
		return fmt.Sprintf("enter follows %d and unfollows %d", len(add), len(remove))
	case len(add) > 0:
		return fmt.Sprintf("enter follows %d", len(add))
	case len(remove) > 0:
		return fmt.Sprintf("enter unfollows %d", len(remove))
	}
	return "nothing to change"
}

func (d scopesDialog) View(th Theme, width, height int) string {
	width = min(width, 60)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	if len(d.list.rows) == 0 {
		// Right after a cache reset the spaces are not in yet, the box fills in when they land.
		b.WriteString(muted.Render("No spaces in the cache yet, they arrive with the next sync.") + "\n")
	} else {
		b.WriteString(muted.Render(d.plan()) + "\n")
	}
	b.WriteString(muted.Render("Your own tasks are always included.") + "\n")
	// Border, plan, the My tasks line, filter, two blanks and the hint take eight lines, the rest is the list, twelve at most.
	b.WriteString(d.list.view(th, width-2, max(min(12, height-8), 3)))
	b.WriteString("\n" + muted.Render("space toggle   enter apply   esc cancel"))
	return th.box("Follow", b.String(), width, lipgloss.Height(b.String())+2, true)
}

// scopesToast names the one scope a change touched, or says that several changed.
func scopesToast(add, remove []string, titles map[string]string) string {
	switch {
	case len(add) == 1 && len(remove) == 0:
		return "Following " + titles[add[0]]
	case len(add) == 0 && len(remove) == 1:
		return "No longer following " + titles[remove[0]]
	}
	return "Followed spaces and projects updated"
}

// pickerItems lists My tasks, then each space with the projects right under its root, the way the first run and the settings screen both offer them.
func pickerItems(msg pickerLoadedMsg, checked map[string]bool) []pickerItem {
	items := []pickerItem{{scope: store.Scope{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true}, checked: true, disabled: true}}
	for _, sp := range msg.spaces {
		items = append(items, pickerItem{scope: store.Scope{ID: sp.ID, Kind: store.ScopeKindSpace, Title: sp.Title, Followed: true}, checked: checked[sp.ID]})
		for _, p := range msg.projects[sp.ID] {
			items = append(items, pickerItem{scope: store.Scope{ID: p.ID, Kind: store.ScopeKindProject, Title: p.Title, Followed: true}, depth: 1, checked: checked[p.ID]})
		}
	}
	return items
}

func followedSet(scopes []store.Scope) map[string]bool {
	set := map[string]bool{}
	for _, sc := range scopes {
		set[sc.ID] = sc.Followed
	}
	return set
}
