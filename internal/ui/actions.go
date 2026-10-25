package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

// taskAction is one action on the selected task: its key, when it has one, its row in the actions list and the run.
type taskAction struct {
	key  key.Binding // the zero value for an action without a key, a row of the list alone
	name string      // the label of an action without a key, a keyed one is labeled by its binding's help text
	run  func(m *Model) tea.Cmd
}

// label is the row in the actions list, the words the help overlay shows for the key.
func (a taskAction) label() string {
	if a.hasKey() {
		return a.key.Help().Desc
	}
	return a.name
}

func (a taskAction) hasKey() bool { return len(a.key.Keys()) > 0 }

// KeylessActions names the actions of the list that have no key, in the order of the list, for the manual.
func KeylessActions() []string {
	var out []string
	for _, a := range taskActionTable(defaultKeyMap()) {
		if !a.hasKey() {
			out = append(out, a.label())
		}
	}
	return out
}

// taskActionTable is the one list of the actions: the key dispatch walks it, the actions list shows it
// and KeyMap.task() is built from it, so the overlay and the manual cannot drift from the keys that work.
func taskActionTable(k KeyMap) []taskAction {
	return []taskAction{
		{key: k.Comment, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				d, cmd := newCommentDialog(t.ID, t.Title, min(m.width-4, 80))
				m.openDialog(d)
				return cmd
			})
		}},
		{key: k.CommentEditor, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				cmd, err := openEditor(t.ID, nil)
				if err != nil {
					return m.status.show(err.Error(), true)
				}
				return cmd
			})
		}},
		{key: k.LogTime, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				// Beside the grid the entry goes on the day under the cursor, the day n on the cell takes.
				date := ""
				if m.screen == screenTimesheet && m.timesheet.loaded {
					date = m.timesheet.cellDate()
				}
				return m.openTimelogDialog(t.ID, t.Title, nil, date)
			})
		}},
		{key: k.Status, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				m.openDialog(newStatusDialog(t, m.ref, m.keys))
				return nil
			})
		}},
		{key: k.StatusPrev, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd { return m.moveStatus(t, -1) })
		}},
		{key: k.StatusNext, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd { return m.moveStatus(t, 1) })
		}},
		{key: k.Assignee, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				d, cmd := newAssigneeDialog(t, m.ref)
				m.openDialog(d)
				return cmd
			})
		}},
		{key: k.Dates, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				d, cmd := newDatesDialog(t, m.opts.Now())
				m.openDialog(d)
				return cmd
			})
		}},
		{key: k.EditTitle, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				d, cmd := newTitleDialog(t, min(m.width-4, 80))
				m.openDialog(d)
				return cmd
			})
		}},
		{key: k.EditDescription, run: func(m *Model) tea.Cmd {
			// A list row carries no description, so the task is read again before the editor opens.
			return m.withTask(func(t store.Task) tea.Cmd { return m.loadDescription(t.ID) })
		}},
		{key: k.Importance, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				m.openDialog(newImportanceDialog(t, m.keys))
				return nil
			})
		}},
		{key: k.Folders, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				// A move leaves the folders under the node in view. The timesheet shows no folder, so a move from there leaves none.
				node := m.list.nodeID
				if m.screen == screenTimesheet {
					node = ""
				}
				d, cmd := newFoldersDialog(t, m.sidebar.nodes, m.list.folders, node)
				m.openDialog(d)
				return cmd
			})
		}},
		{key: k.Open, run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				open := m.opts.Hooks.OpenURL
				if open == nil {
					return m.status.show("browser not available", true)
				}
				if unconfirmed(t) {
					return m.status.show(notOnWrike, true)
				}
				if t.Permalink == "" {
					return m.status.show(noPermalink, true)
				}
				// Starting a browser can block, so it runs as a command and reports back instead of stalling the key handler.
				return func() tea.Msg {
					if err := open(t.Permalink); err != nil {
						return toastMsg{text: "could not open browser: " + err.Error(), isErr: true}
					}
					return toastMsg{text: "Opened in browser"}
				}
			})
		}},
		{key: k.CopyLink, run: func(m *Model) tea.Cmd {
			return m.copy(func(t store.Task) (string, string) { return t.Permalink, "Copied permalink" })
		}},
		{key: k.CopyBranch, run: func(m *Model) tea.Cmd {
			return m.copy(func(t store.Task) (string, string) {
				name := branchName(m.opts.Config.UI.BranchTemplate, t, m.opts.Config.UI.HidePrefixes)
				return name, "Copied " + name
			})
		}},
		{key: k.CopyID, run: func(m *Model) tea.Cmd {
			return m.copy(func(t store.Task) (string, string) { return t.ID, "Copied task id" })
		}},
		// The budget is edited a few times a week at most, a row without a key is enough for it.
		{name: "budget", run: func(m *Model) tea.Cmd {
			return m.withTask(func(t store.Task) tea.Cmd {
				// The loader worked the budget out for the task the detail shows, the row acts on that task alone.
				if !m.detail.loaded || m.detail.task.ID != t.ID {
					return m.status.show("the task is still loading, try again", true)
				}
				b := m.detail.budget
				if b.reason != "" {
					return m.status.show(b.reason, true)
				}
				d, cmd := newBudgetDialog(t, b)
				m.openDialog(d)
				return cmd
			})
		}},
	}
}
