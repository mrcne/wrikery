package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
)

func testCrumbs() map[string]string {
	return map[string]string{
		"MOB": "Mobile", "IOS": "Mobile / iOS app", "PLT": "Platform", "API": "Platform / API",
		"DSG": "Platform / Design system", "INF": "Platform / Infra", "ONC": "Platform / Infra / On-call",
	}
}

func createDialogFor(presetID string, mine bool) dialog {
	d, _ := newCreateDialog(testTree(), testCrumbs(), presetID, "CS1", mine, 60)
	return d
}

func createView(d dialog) string {
	return d.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40)
}

func TestCreateDialogPresetsTheFolderInView(t *testing.T) {
	dl := createDialogFor("API", false)
	if view := createView(dl); !strings.Contains(view, "Platform / API") {
		t.Errorf("the folder line should show the preset:\n%s", view)
	}
	dl = typeRunes(dl, "Fix it")
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitCreateMsg{folderID: "API", title: "Fix it", statusID: "CS1"}
	if len(msgs) != 2 || msgs[0] != want || msgs[1] != (closeDialogMsg{}) {
		t.Errorf("enter -> %#v", msgs)
	}
}

func TestCreateDialogPicksAnotherFolder(t *testing.T) {
	dl := createDialogFor("API", false)
	dl = typeRunes(dl, "Fix it")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	if view := createView(dl); !strings.Contains(view, "Design system") || strings.Contains(view, "[ ]") {
		t.Errorf("tab should open the tree, drawn without check boxes:\n%s", view)
	}
	dl = typeRunes(dl, "des")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := createView(dl); !strings.Contains(view, "Platform / Design system") {
		t.Errorf("enter on the tree should pick and show the folder:\n%s", view)
	}
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (submitCreateMsg{folderID: "DSG", title: "Fix it", statusID: "CS1"}) {
		t.Errorf("enter -> %#v", msgs)
	}
}

func TestCreateDialogRefusesAnEmptyTitle(t *testing.T) {
	dl := createDialogFor("API", false)
	dl, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("enter on an empty title -> %#v, want nothing", msgs)
	}
	if view := createView(dl); !strings.Contains(view, "cannot be empty") {
		t.Errorf("view = %q, want the refusal", view)
	}
}

func TestCreateDialogOnMyTasksNeedsAFolderAndAssignsMe(t *testing.T) {
	dl := createDialogFor("", true)
	if view := createView(dl); !strings.Contains(view, "pick a folder") {
		t.Errorf("without a folder in view the line should ask for one:\n%s", view)
	}
	dl = typeRunes(dl, "Fix it")
	dl, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, m := range collect(cmd) {
		// Moving to the folder field starts the cursor blink, that message is not a submit.
		if _, ok := m.(submitCreateMsg); ok {
			t.Errorf("enter without a folder submitted %#v", m)
		}
	}
	if view := createView(dl); !strings.Contains(view, "> ") || !strings.Contains(view, "iOS app") {
		t.Errorf("enter without a folder should open the tree:\n%s", view)
	}
	dl = typeRunes(dl, "ios")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (submitCreateMsg{folderID: "IOS", title: "Fix it", statusID: "CS1", assignMe: true}) {
		t.Errorf("enter -> %#v", msgs)
	}
}
