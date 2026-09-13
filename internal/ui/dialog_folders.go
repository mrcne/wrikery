package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitFoldersMsg struct {
	taskID      string
	add, remove []string
	toast       string
}

type folderRow struct {
	id, title string
	depth     int
}

// foldersDialog is the checklist of the followed folders a task sits in, with a move as its shortcut:
// enter with nothing toggled takes the task out of the folders under the node in view and puts it into the highlighted one.
// Parents the tree does not know are counted, never touched, and keep the task placed when the known ones are all unchecked.
type foldersDialog struct {
	taskID   string
	rows     []folderRow
	visible  []int
	checked  map[string]bool
	original map[string]bool
	leaving  []string // the parents under the node in view, in tree order
	outside  int
	cursor   int
	filter   textinput.Model
	errText  string
}

func newFoldersDialog(task store.Task, nodes []treeNode, idx folderIndex, nodeID string) (foldersDialog, tea.Cmd) {
	d := foldersDialog{taskID: task.ID, checked: map[string]bool{}, original: map[string]bool{}}
	known := map[string]bool{}
	for _, n := range nodes {
		if n.kind == nodeMe {
			continue
		}
		d.rows = append(d.rows, folderRow{id: n.id, title: n.title, depth: n.depth})
		known[n.id] = true
	}
	for _, id := range task.ParentIDs {
		if !known[id] {
			d.outside++
			continue
		}
		d.checked[id], d.original[id] = true, true
	}
	for _, r := range d.rows {
		if d.original[r.id] && idx.under(r.id, nodeID) {
			d.leaving = append(d.leaving, r.id)
		}
	}
	d.filter = textinput.New()
	d.filter.Prompt = "> "
	d.filter.Placeholder = "type a folder name"
	d.applyFilter()
	for i, ri := range d.visible {
		if d.original[d.rows[ri].id] {
			d.cursor = i
			break
		}
	}
	return d, d.filter.Focus()
}

func (d *foldersDialog) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(d.filter.Value()))
	d.visible = d.visible[:0]
	for i, r := range d.rows {
		if q == "" || wordPrefix(strings.ToLower(r.title), q) {
			d.visible = append(d.visible, i)
		}
	}
	if d.cursor >= len(d.visible) {
		d.cursor = max(0, len(d.visible)-1)
	}
}

// wordPrefix matches the query against the start of the title or of any word in it, so "sys" finds "Design system".
func wordPrefix(title, q string) bool {
	if strings.HasPrefix(title, q) {
		return true
	}
	for _, w := range strings.Fields(title) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}

func (d foldersDialog) title(id string) string {
	for _, r := range d.rows {
		if r.id == id {
			return r.title
		}
	}
	return id
}

func (d foldersDialog) highlighted() (folderRow, bool) {
	if d.cursor < 0 || d.cursor >= len(d.visible) {
		return folderRow{}, false
	}
	return d.rows[d.visible[d.cursor]], true
}

// toggles is the diff between the checks as they stand and as the box opened, sorted for stable messages.
func (d foldersDialog) toggles() (add, remove []string) {
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
	return add, remove
}

// move is what enter does with nothing toggled: into the highlighted folder, out of the folders under the node in view.
func (d foldersDialog) move() (add, remove []string, ok bool) {
	target, found := d.highlighted()
	if !found {
		return nil, nil, false
	}
	if !d.original[target.id] {
		add = []string{target.id}
	}
	for _, id := range d.leaving {
		if id != target.id {
			remove = append(remove, id)
		}
	}
	return add, remove, len(add) > 0 || len(remove) > 0
}

func (d foldersDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
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
		r, ok := d.highlighted()
		if !ok {
			return d, nil
		}
		if d.checked[r.id] && d.outside == 0 && d.checkedCount() == 1 {
			d.errText = "a task needs a folder"
			return d, nil
		}
		d.checked[r.id] = !d.checked[r.id]
		d.errText = ""
		return d, nil
	case tea.KeyEnter:
		add, remove := d.toggles()
		toast := "Folders updated"
		switch {
		case len(add) == 1 && len(remove) == 0:
			toast = "Added to " + d.title(add[0])
		case len(add) == 0 && len(remove) == 1:
			toast = "Removed from " + d.title(remove[0])
		case len(add) == 0 && len(remove) == 0:
			var ok bool
			add, remove, ok = d.move()
			target, _ := d.highlighted()
			if !ok {
				d.errText = "already in " + target.title
				return d, nil
			}
			toast = "Moved to " + target.title
			if len(remove) == 0 {
				toast = "Added to " + target.title
			}
		}
		return d, tea.Batch(intent(submitFoldersMsg{taskID: d.taskID, add: add, remove: remove, toast: toast}), intent(closeDialogMsg{}))
	}
	var cmd tea.Cmd
	d.filter, cmd = d.filter.Update(msg)
	d.applyFilter()
	d.errText = ""
	return d, cmd
}

func (d foldersDialog) checkedCount() int {
	n := 0
	for _, on := range d.checked {
		if on {
			n++
		}
	}
	return n
}

// plan is the first line of the box: what enter does right now, so a move and an apply cannot be mistaken for each other.
func (d foldersDialog) plan() string {
	add, remove := d.toggles()
	if n := len(add) + len(remove); n == 1 {
		return "enter applies 1 change"
	} else if n > 1 {
		return fmt.Sprintf("enter applies %d changes", n)
	}
	target, ok := d.highlighted()
	if !ok {
		return "no folder matches"
	}
	_, remove, ok = d.move()
	if !ok {
		return "already in " + target.title
	}
	if len(remove) == 0 {
		return "enter adds to " + target.title
	}
	names := make([]string, len(remove))
	for i, id := range remove {
		names[i] = d.title(id)
	}
	return "enter moves to " + target.title + ", leaves " + strings.Join(names, ", ")
}

func (d foldersDialog) View(th Theme, width int) string {
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	b.WriteString(muted.Render(d.plan()) + "\n")
	b.WriteString(d.filter.View() + "\n\n")
	for i, ri := range d.visible {
		if i >= 12 {
			b.WriteString(muted.Render("... type to narrow down") + "\n")
			break
		}
		r := d.rows[ri]
		mark := "[ ]"
		if d.checked[r.id] {
			mark = "[x]"
		}
		b.WriteString(rowLine(th, strings.Repeat("  ", r.depth)+mark+" "+r.title, width-2, i == d.cursor, true) + "\n")
	}
	if d.outside > 0 {
		noun := "folders"
		if d.outside == 1 {
			noun = "folder"
		}
		b.WriteString(muted.Render(fmt.Sprintf("and %d %s outside the followed scopes", d.outside, noun)) + "\n")
	}
	hint := muted.Render("space toggle   enter move or apply   esc cancel")
	if d.errText != "" {
		hint = lipgloss.NewStyle().Foreground(th.Error).Render(d.errText)
	}
	b.WriteString("\n" + hint)
	return th.box("Folders", b.String(), min(width, 60), lipgloss.Height(b.String())+2, true)
}
