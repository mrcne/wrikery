package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type submitFoldersMsg struct {
	taskID      string
	add, remove []string
}

// foldersDialog is the checklist of the followed folders a task sits in, with a move as its shortcut:
// enter with nothing toggled takes the task out of the folders under the node in view and puts it into the highlighted one.
// Parents the tree does not know are counted, never touched, and keep the task placed when the known ones are all unchecked.
type foldersDialog struct {
	taskID  string
	list    checklist
	titles  map[string]string
	leaving []string // the parents under the node in view, in tree order
	outside int
	errText string
}

func newFoldersDialog(task store.Task, nodes []treeNode, idx folderIndex, nodeID string) (foldersDialog, tea.Cmd) {
	d := foldersDialog{taskID: task.ID, titles: map[string]string{}}
	var rows []checkRow
	for _, n := range nodes {
		// The tree draws a folder once per parent, the checklist lists it once, where it first appears.
		if n.kind == nodeMe || d.titles[n.id] != "" {
			continue
		}
		rows = append(rows, checkRow{id: n.id, label: strings.Repeat("  ", n.depth) + n.title})
		d.titles[n.id] = n.title
	}
	var known []string
	for _, id := range task.ParentIDs {
		if d.titles[id] == "" {
			d.outside++
			continue
		}
		known = append(known, id)
	}
	for _, r := range rows {
		for _, id := range known {
			if r.id == id && idx.under(id, nodeID) {
				d.leaving = append(d.leaving, id)
			}
		}
	}
	list, cmd := newChecklist(rows, known, "type a folder name")
	if len(known) > 0 {
		list.selectID(known[0])
	}
	d.list = list
	return d, cmd
}

// move is what enter does with nothing toggled: into the target, out of the folders under the node in view.
func (d foldersDialog) move(target checkRow) (add, remove []string, ok bool) {
	if !d.list.original[target.id] {
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
	case tea.KeySpace:
		r, ok := d.list.current()
		if !ok {
			return d, nil
		}
		if d.list.checked[r.id] && d.outside == 0 && d.list.checkedCount() == 1 {
			// Wrike accepts a task with no parent left and moves it into the account root,
			// which no followed scope covers, so the task would drop out of the cache at the next sync.
			d.errText = "a task needs a folder"
			return d, nil
		}
		d.list.toggle()
		d.errText = ""
		return d, nil
	case tea.KeyEnter:
		add, remove := d.list.diff()
		if len(add) == 0 && len(remove) == 0 {
			target, found := d.list.current()
			if !found {
				d.errText = "no folder matches"
				return d, nil
			}
			var ok bool
			if add, remove, ok = d.move(target); !ok {
				d.errText = "already in " + d.titles[target.id]
				return d, nil
			}
		}
		return d, tea.Batch(intent(submitFoldersMsg{taskID: d.taskID, add: add, remove: remove}), intent(closeDialogMsg{}))
	}
	cmd := d.list.update(msg)
	d.errText = ""
	return d, cmd
}

// plan is the first line of the box: what enter does right now, so a move and an apply cannot be mistaken for each other.
func (d foldersDialog) plan() string {
	add, remove := d.list.diff()
	if n := len(add) + len(remove); n == 1 {
		return "enter applies 1 change"
	} else if n > 1 {
		return fmt.Sprintf("enter applies %d changes", n)
	}
	target, ok := d.list.current()
	if !ok {
		return "no folder matches"
	}
	_, remove, ok = d.move(target)
	if !ok {
		return "already in " + d.titles[target.id]
	}
	if len(remove) == 0 {
		return "enter adds to " + d.titles[target.id]
	}
	names := make([]string, len(remove))
	for i, id := range remove {
		names[i] = d.titles[id]
	}
	return "enter moves to " + d.titles[target.id] + ", leaves " + strings.Join(names, ", ")
}

func (d foldersDialog) View(th Theme, width, height int) string {
	width = min(width, 60)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	b.WriteString(muted.Render(d.plan()) + "\n")
	// Border, plan, filter, two blanks, the outside line and the hint take up to nine lines, the rest is the list, twelve at most.
	b.WriteString(d.list.view(th, width-2, max(min(12, height-9), 3)))
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
	return th.box("Folders", b.String(), width, lipgloss.Height(b.String())+2, true)
}

// foldersToast names the one folder a change touched, or counts a mixed one.
func foldersToast(add, remove []string, titles map[string]string) string {
	switch {
	case len(add) == 1 && len(remove) == 0:
		return "Added to " + titles[add[0]]
	case len(add) == 1:
		return "Moved to " + titles[add[0]]
	case len(add) == 0 && len(remove) == 1:
		return "Removed from " + titles[remove[0]]
	}
	return "Folders updated"
}
