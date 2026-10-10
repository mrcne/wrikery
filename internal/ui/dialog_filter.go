package ui

import (
	"cmp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type submitFilterMsg struct{ filter rowFilter }

// filterDialog is the box on f: the importance levels, the people in view and the statuses of the workflows in view,
// every ticked row a thing to show. People is a pick of one, ticking a person unticks the one before.
// A row id carries a letter for its section, a status name and a contact id could otherwise not be told apart.
type filterDialog struct {
	list checklist
}

func newFilterDialog(l *taskListModel) (filterDialog, tea.Cmd) {
	var rows []checkRow
	var checked []string
	for _, level := range importanceLevels {
		rows = append(rows, checkRow{id: "i:" + level, label: level, section: "Importance"})
		if l.narrow.importance[level] {
			checked = append(checked, "i:"+level)
		}
	}
	for _, id := range peopleInView(l) {
		label := contactName(id, l.ref)
		switch id {
		case l.ref.meID:
			label += " (me)"
		case unassigned:
			label = "Unassigned"
		}
		rows = append(rows, checkRow{id: "p:" + id, label: label, section: "People"})
	}
	if l.narrow.person != "" {
		checked = append(checked, "p:"+l.narrow.person)
	}
	// Each status name once, in the order of the workflows in view, the one most rows use first, a hidden one only when ticked.
	listed := map[string]bool{}
	for _, wf := range viewWorkflows(l.all, l.ref) {
		for _, cs := range wf.CustomStatuses {
			k := statusKey(cs.Name)
			if listed[k] || (cs.Hidden && !l.narrow.statuses[k]) {
				continue
			}
			rows = append(rows, checkRow{id: "s:" + k, label: cs.Name, section: "Status"})
			listed[k] = true
		}
	}
	// A status ticked on another folder stays listed, so it can be unticked from here.
	extra := map[string]bool{}
	for k := range l.narrow.statuses {
		checked = append(checked, "s:"+k)
		if !listed[k] {
			extra[k] = true
		}
	}
	for _, name := range statusNames(extra, l.ref) {
		rows = append(rows, checkRow{id: "s:" + statusKey(name), label: name, section: "Status"})
	}
	list, cmd := newChecklist(rows, checked, "type to narrow down")
	return filterDialog{list: list}, cmd
}

// peopleInView lists you first, then the people on the rows in view by name, the one the filter names among them,
// and Unassigned last, the order the by assignee sections have.
func peopleInView(l *taskListModel) []string {
	ids := map[string]bool{"": true}
	for _, r := range l.all {
		for _, id := range r.task.ResponsibleIDs {
			ids[id] = true
		}
	}
	if l.narrow.person != "" && l.narrow.person != unassigned {
		ids[l.narrow.person] = true
	}
	if l.ref.meID != "" {
		ids[l.ref.meID] = true
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	slices.SortStableFunc(out, func(a, b string) int {
		ra, na := personRank(a, l.ref)
		rb, nb := personRank(b, l.ref)
		return cmp.Or(cmp.Compare(ra, rb), strings.Compare(na, nb), strings.Compare(a, b))
	})
	for i, id := range out {
		if id == "" {
			out[i] = unassigned
		}
	}
	return out
}

func (d filterDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	switch msg.Type {
	case tea.KeySpace:
		if r, ok := d.list.current(); ok && strings.HasPrefix(r.id, "p:") && !d.list.checked[r.id] {
			for id := range d.list.checked {
				if strings.HasPrefix(id, "p:") {
					d.list.checked[id] = false
				}
			}
		}
		d.list.toggle()
		return d, nil
	case tea.KeyEnter:
		if add, remove := d.list.diff(); len(add) == 0 && len(remove) == 0 {
			return d, intent(closeDialogMsg{})
		}
		return d, tea.Batch(intent(submitFilterMsg{filter: d.filter()}), intent(closeDialogMsg{}))
	}
	return d, d.list.update(msg)
}

// filter is the ticked rows as the list reads them, a dimension nobody ticked left nil.
func (d filterDialog) filter() rowFilter {
	var f rowFilter
	for id, on := range d.list.checked {
		if !on {
			continue
		}
		switch kind, rest := id[:2], id[2:]; kind {
		case "i:":
			if f.importance == nil {
				f.importance = map[string]bool{}
			}
			f.importance[rest] = true
		case "p:":
			f.person = rest
		case "s:":
			if f.statuses == nil {
				f.statuses = map[string]bool{}
			}
			f.statuses[rest] = true
		}
	}
	return f
}

func (d filterDialog) View(th Theme, width, height int) string {
	width = min(width, 50)
	// Border, filter, blank, blank and the hint take six lines and every section a header, the rest is the list, fourteen rows at most.
	sections, last := 0, ""
	for _, r := range d.list.rows {
		if r.section != last {
			sections, last = sections+1, r.section
		}
	}
	body := d.list.view(th, width-2, max(min(14, height-6-sections), 3))
	body += "\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("space toggle   enter apply   esc cancel")
	return th.box("Filter", body, width, lipgloss.Height(body)+2, true)
}
