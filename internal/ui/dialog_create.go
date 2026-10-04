package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type submitCreateMsg struct {
	folderID, title string
	where           string // the folder's path, for the toast
	assignMe        bool
}

type createField int

const (
	fieldTitle createField = iota
	fieldFolder
)

// createDialog is the quick add box: a title and the folder the task goes into, preset to the node in view.
// The folder field is the followed tree the folders box lists, a single pick drawn without check boxes.
type createDialog struct {
	title    textinput.Model
	folders  checklist
	crumbs   map[string]string // the path of every folder, as the pane title shows it
	folderID string
	mine     bool // the view is My tasks, the task is assigned to the user so it stays in that view
	field    createField
	done     bool // submitted, a second enter already in the queue must not create the task twice
	errText  string
}

func newCreateDialog(nodes []treeNode, crumbs map[string]string, presetID string, mine bool, width int) (createDialog, tea.Cmd) {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "task title"
	in.Width = width - 4
	// Focus first, the model is copied into the dialog below and a focus after that would reach the copy.
	cmd := in.Focus()
	rows, _ := treeRows(nodes)
	list, _ := newChecklist(rows, nil, "type a folder name")
	list.single = true
	list.filter.Blur()
	folderID := ""
	if presetID != "" {
		// A preset the tree no longer holds stays unpicked, the dialog asks for a folder rather than creating into it blind.
		list.selectID(presetID)
		if r, ok := list.current(); ok && r.id == presetID {
			folderID = presetID
		}
	}
	d := createDialog{title: in, folders: list, crumbs: crumbs, folderID: folderID, mine: mine}
	return d, cmd
}

// focus moves the cursor between the two fields, the one left behind stops drawing a cursor.
func (d createDialog) focus(f createField) (createDialog, tea.Cmd) {
	// The cmd is taken before the return copies d, the order inside one return is not specified.
	d.field = f
	if f == fieldFolder {
		d.title.Blur()
		cmd := d.folders.filter.Focus()
		return d, cmd
	}
	d.folders.filter.Blur()
	cmd := d.title.Focus()
	return d, cmd
}

func (d createDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	if d.done {
		// The close travels through the queue, a key that arrives before it must not act.
		return d, nil
	}
	switch {
	case msg.Type == tea.KeyTab:
		d.errText = ""
		if d.field == fieldTitle {
			return d.focus(fieldFolder)
		}
		return d.focus(fieldTitle)
	case msg.Type == tea.KeyEnter && d.field == fieldFolder:
		r, ok := d.folders.current()
		if !ok {
			d.errText = "no folder matches"
			return d, nil
		}
		d.folderID = r.id
		d.folders.filter.SetValue("")
		d.folders.applyFilter()
		d.folders.selectID(r.id)
		d.errText = ""
		return d.focus(fieldTitle)
	case msg.Type == tea.KeyEnter:
		// Only the ends are trimmed, spaces inside a title are the user's own.
		title := strings.TrimSpace(d.title.Value())
		if title == "" {
			d.errText = "a title cannot be empty"
			return d, nil
		}
		if d.folderID == "" {
			d.errText = "pick a folder"
			return d.focus(fieldFolder)
		}
		d.done = true
		return d, tea.Batch(
			intent(submitCreateMsg{folderID: d.folderID, title: title, where: d.crumbs[d.folderID], assignMe: d.mine}),
			intent(closeDialogMsg{}))
	}
	d.errText = ""
	if d.field == fieldFolder {
		cmd := d.folders.update(msg)
		return d, cmd
	}
	var cmd tea.Cmd
	d.title, cmd = d.title.Update(msg)
	return d, cmd
}

func (d createDialog) View(th Theme, width, height int) string {
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	b.WriteString(d.title.View() + "\n\n")
	hint := "enter creates   tab folder   esc cancels"
	if d.field == fieldFolder {
		// Border, title, two blanks, filter, blank, hint: eight lines around the list, eight rows of tree at most.
		b.WriteString(d.folders.view(th, width-2, max(min(8, height-10), 3)))
		hint = "enter picks   tab title   esc cancels"
	} else {
		where := "pick a folder"
		if d.folderID != "" {
			where = d.crumbs[d.folderID]
		}
		b.WriteString(muted.Render("in: ") + where + "\n")
	}
	line := muted.Render(hint)
	if d.errText != "" {
		line = lipgloss.NewStyle().Foreground(th.Error).Render(d.errText)
	}
	b.WriteString("\n" + line)
	return th.box("New task", b.String(), width, lipgloss.Height(b.String())+2, true)
}
