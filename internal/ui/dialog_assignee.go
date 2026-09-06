package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitAssigneesMsg struct {
	taskID      string
	add, remove []string
}

type assigneeDialog struct {
	taskID   string
	contacts []store.Contact
	visible  []int
	checked  map[string]bool
	original map[string]bool
	cursor   int
	filter   textinput.Model
	keys     KeyMap
}

func newAssigneeDialog(task store.Task, ref refData, keys KeyMap) (assigneeDialog, tea.Cmd) {
	d := assigneeDialog{taskID: task.ID, checked: map[string]bool{}, original: map[string]bool{}, keys: keys}
	for _, c := range ref.contacts {
		// A Wrike contact's type is Person or Group (https://developers.wrike.com/api/v4/contacts/).
		// The picker offers people only, so a group here is skipped along with a deleted contact.
		if c.Deleted || c.Type == "Group" {
			continue
		}
		d.contacts = append(d.contacts, c)
	}
	sort.SliceStable(d.contacts, func(i, j int) bool {
		a, b := d.contacts[i], d.contacts[j]
		if (a.ID == ref.meID) != (b.ID == ref.meID) {
			return a.ID == ref.meID
		}
		if a.FirstName != b.FirstName {
			return a.FirstName < b.FirstName
		}
		if a.LastName != b.LastName {
			return a.LastName < b.LastName
		}
		// The map iteration order over ref.contacts is random, so a tie on both names needs its own
		// tie-break or the picker's order would still vary between runs.
		return a.ID < b.ID
	})
	for _, id := range task.ResponsibleIDs {
		d.checked[id], d.original[id] = true, true
	}
	d.filter = textinput.New()
	d.filter.Prompt = "> "
	d.filter.Placeholder = "type a name"
	d.applyFilter()
	return d, d.filter.Focus()
}

func (d *assigneeDialog) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(d.filter.Value()))
	d.visible = d.visible[:0]
	for i, c := range d.contacts {
		if q == "" || strings.HasPrefix(strings.ToLower(c.FirstName), q) || strings.HasPrefix(strings.ToLower(c.LastName), q) {
			d.visible = append(d.visible, i)
		}
	}
	if d.cursor >= len(d.visible) {
		d.cursor = max(0, len(d.visible)-1)
	}
}

func (d assigneeDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.Type {
	case tea.KeyDown, tea.KeyCtrlN:
		if d.cursor < len(d.visible)-1 {
			d.cursor++
		}
		return d, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if d.cursor > 0 {
			d.cursor--
		}
		return d, nil
	case tea.KeySpace:
		if d.cursor < len(d.visible) {
			id := d.contacts[d.visible[d.cursor]].ID
			d.checked[id] = !d.checked[id]
		}
		return d, nil
	case tea.KeyEnter:
		var add, remove []string
		for id := range d.checked {
			if d.checked[id] && !d.original[id] {
				add = append(add, id)
			}
		}
		for id := range d.original {
			if !d.checked[id] {
				remove = append(remove, id)
			}
		}
		sort.Strings(add)
		sort.Strings(remove)
		if len(add) == 0 && len(remove) == 0 {
			return d, intent(closeDialogMsg{})
		}
		return d, tea.Batch(intent(submitAssigneesMsg{taskID: d.taskID, add: add, remove: remove}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	d.filter, cmd = d.filter.Update(msg)
	d.applyFilter()
	return d, cmd
}

func (d assigneeDialog) View(th Theme, width int) string {
	var b strings.Builder
	b.WriteString(d.filter.View() + "\n\n")
	for i, ci := range d.visible {
		if i >= 12 {
			b.WriteString(lipgloss.NewStyle().Foreground(th.Muted).Render("... type to narrow down"))
			break
		}
		c := d.contacts[ci]
		mark := "[ ]"
		if d.checked[c.ID] {
			mark = "[x]"
		}
		b.WriteString(rowLine(th, mark+" "+strings.TrimSpace(c.FirstName+" "+c.LastName), width-2, i == d.cursor, true) + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("space toggle   enter apply   esc cancel"))
	return th.box("Assignees", b.String(), min(width, 50), lipgloss.Height(b.String())+2, true)
}
