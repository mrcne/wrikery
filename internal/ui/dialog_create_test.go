package ui

import (
	"reflect"
	"regexp"
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
	d, _ := newCreateDialog(testTree(), testCrumbs(), presetID, mine, 60)
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
	want := submitCreateMsg{folderID: "API", title: "Fix it", where: "Platform / API"}
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
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	if view := createView(dl); !strings.Contains(view, "On-call") || !regexp.MustCompile(`> +Design system`).MatchString(view) {
		t.Errorf("a second visit to the folder field should show the whole tree with the cursor on the pick:\n%s", view)
	}
	// Enter on the cursor row picks the same folder again.
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (submitCreateMsg{folderID: "DSG", title: "Fix it", where: "Platform / Design system"}) {
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
	if len(msgs) != 2 || msgs[0] != (submitCreateMsg{folderID: "IOS", title: "Fix it", where: "Mobile / iOS app", assignMe: true}) {
		t.Errorf("enter -> %#v", msgs)
	}
}

func TestCreateDialogSaysWhenNoFolderMatches(t *testing.T) {
	dl := createDialogFor("API", false)
	dl = typeRunes(dl, "Fix it")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyTab})
	dl = typeRunes(dl, "zzz")
	dl, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("enter on an empty match list -> %#v, want nothing", msgs)
	}
	if view := createView(dl); !strings.Contains(view, "no folder matches") {
		t.Errorf("view should say that nothing matches:\n%s", view)
	}
}

func TestCreateDialogWithAnEmptyTreeAsksForAFolder(t *testing.T) {
	dl, _ := newCreateDialog(nil, nil, "", false, 60)
	var d dialog = dl
	d = typeRunes(d, "Fix it")
	d, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, m := range collect(cmd) {
		if _, ok := m.(submitCreateMsg); ok {
			t.Errorf("enter with no folder to pick submitted %#v", m)
		}
	}
	if view := createView(d); !strings.Contains(view, "no folder") && !strings.Contains(view, "pick a folder") {
		t.Errorf("view should say there is no folder to pick:\n%s", view)
	}
}

// The preset is the sidebar's selected node, which can be gone from the tree by the time the dialog opens.
func TestCreateDialogIgnoresAPresetTheTreeLacks(t *testing.T) {
	dl, _ := newCreateDialog(testTree(), testCrumbs(), "GONE", false, 60)
	var d dialog = dl
	if view := createView(d); !strings.Contains(view, "pick a folder") {
		t.Errorf("a preset outside the tree should leave the folder unpicked:\n%s", view)
	}
	d = typeRunes(d, "Fix it")
	d, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, m := range collect(cmd) {
		if _, ok := m.(submitCreateMsg); ok {
			t.Errorf("enter submitted into a folder the tree does not hold: %#v", m)
		}
	}
	if view := createView(d); !strings.Contains(view, "iOS app") {
		t.Errorf("enter should open the tree instead:\n%s", view)
	}
}

func TestCreateDialogSubmitsOnce(t *testing.T) {
	dl := createDialogFor("API", false)
	dl = typeRunes(dl, "Fix it")
	dl, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 2 {
		t.Fatalf("first enter -> %#v", msgs)
	}
	// The close is still in the queue when a second enter arrives, it must not create the task twice.
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 0 {
		t.Errorf("second enter -> %#v, want nothing", msgs)
	}
}

func TestFolderCrumbsFollowTheTree(t *testing.T) {
	m := Model{sidebar: sidebarModel{nodes: testTree()}}
	if got := m.folderCrumbs(); !reflect.DeepEqual(got, testCrumbs()) {
		t.Errorf("crumbs = %v, want %v", got, testCrumbs())
	}
}
