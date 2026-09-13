package ui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitAssigneesMsg struct {
	taskID      string
	add, remove []string
}

type assigneeDialog struct {
	taskID string
	list   checklist
}

func newAssigneeDialog(task store.Task, ref refData) (assigneeDialog, tea.Cmd) {
	var contacts []store.Contact
	for _, c := range ref.contacts {
		// A Wrike contact's type is Person or Group (https://developers.wrike.com/api/v4/contacts/).
		// The picker offers people only, so a group here is skipped along with a deleted contact.
		if c.Deleted || c.Type == "Group" {
			continue
		}
		contacts = append(contacts, c)
	}
	sort.SliceStable(contacts, func(i, j int) bool {
		a, b := contacts[i], contacts[j]
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
	rows := make([]checkRow, len(contacts))
	for i, c := range contacts {
		rows[i] = checkRow{id: c.ID, label: strings.TrimSpace(c.FirstName + " " + c.LastName)}
	}
	list, cmd := newChecklist(rows, task.ResponsibleIDs, "type a name")
	return assigneeDialog{taskID: task.ID, list: list}, cmd
}

func (d assigneeDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.Type {
	case tea.KeySpace:
		d.list.toggle()
		return d, nil
	case tea.KeyEnter:
		add, remove := d.list.diff()
		if len(add) == 0 && len(remove) == 0 {
			return d, intent(closeDialogMsg{})
		}
		return d, tea.Batch(intent(submitAssigneesMsg{taskID: d.taskID, add: add, remove: remove}), intent(closeDialogMsg{}))
	}
	cmd := d.list.update(msg)
	return d, cmd
}

func (d assigneeDialog) View(th Theme, width, height int) string {
	width = min(width, 50)
	// Border, filter, blank, blank and the hint take six lines, the rest is the list, twelve at most.
	body := d.list.view(th, width-2, max(min(12, height-6), 3))
	body += "\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("space toggle   enter apply   esc cancel")
	return th.box("Assignees", body, width, lipgloss.Height(body)+2, true)
}
