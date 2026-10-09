package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// testPicker is the demo's shape: two spaces, one project under the first, Platform and Mobile followed.
func testPicker() ([]pickerItem, []store.Scope) {
	loaded := pickerLoadedMsg{
		spaces:   []store.Space{{ID: "MOB", Title: "Mobile"}, {ID: "PLT", Title: "Platform"}},
		projects: map[string][]store.Folder{"PLT": {{ID: "API", Title: "API"}}},
	}
	followed := []store.Scope{
		{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true},
		{ID: "MOB", Kind: store.ScopeKindSpace, Title: "Mobile", Followed: true},
		{ID: "PLT", Kind: store.ScopeKindSpace, Title: "Platform", Followed: true},
	}
	return pickerItems(loaded, followedSet(followed)), followed
}

func TestScopesDialogPlanCountsFollowsAndUnfollows(t *testing.T) {
	items, followed := testPicker()
	d, _ := newScopesDialog(items, followed)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	var dl dialog = d
	if view := dl.View(th, 60, 40); !strings.Contains(view, "nothing to change") || !strings.Contains(view, "[x] Mobile") || !strings.Contains(view, "[ ] API") {
		t.Errorf("the box should open on the followed set with nothing to change:\n%s", view)
	}
	dl = typeRunes(dl, "api")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	if view := dl.View(th, 60, 40); !strings.Contains(view, "enter follows 1") || strings.Contains(view, "unfollows") {
		t.Errorf("after ticking API the first line should say enter follows 1:\n%s", view)
	}
	// The filter keeps its query after a toggle, so Mobile needs a new query to show up again.
	for range 3 {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	dl = typeRunes(dl, "mob")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	if view := dl.View(th, 60, 40); !strings.Contains(view, "enter follows 1 and unfollows 1") {
		t.Errorf("the first line should count both directions:\n%s", view)
	}
	if view := dl.View(th, 60, 40); !strings.Contains(view, "Your own tasks are always included") {
		t.Errorf("the box should say My tasks needs no row:\n%s", view)
	}
}

func TestScopesDialogEnterSendsTheWholeFollowedSet(t *testing.T) {
	items, followed := testPicker()
	d, _ := newScopesDialog(items, followed)
	var dl dialog = d
	dl = typeRunes(dl, "api")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	want := submitScopesMsg{scopes: []store.Scope{
		{ID: "MOB", Kind: store.ScopeKindSpace, Title: "Mobile", Followed: true},
		{ID: "PLT", Kind: store.ScopeKindSpace, Title: "Platform", Followed: true},
		{ID: "API", Kind: store.ScopeKindProject, Title: "API", Followed: true},
	}, toast: "Following API"}
	if !reflect.DeepEqual(msgs, []tea.Msg{want, closeDialogMsg{}}) {
		t.Errorf("enter sent %#v", msgs)
	}

	// With nothing changed enter only closes the box, there is nothing to write and nothing to sync again.
	d, _ = newScopesDialog(items, followed)
	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{closeDialogMsg{}}) {
		t.Errorf("enter with no change sent %#v", msgs)
	}
}

func TestScopesToastNamesTheOneChange(t *testing.T) {
	titles := map[string]string{"API": "API", "MOB": "Mobile"}
	cases := map[string]struct {
		add, remove []string
		want        string
	}{
		"one follow":   {add: []string{"API"}, want: "Following API"},
		"one unfollow": {remove: []string{"MOB"}, want: "No longer following Mobile"},
		"mixed":        {add: []string{"API"}, remove: []string{"MOB"}, want: "Followed spaces and projects updated"},
	}
	for name, tc := range cases {
		if got := scopesToast(tc.add, tc.remove, titles); got != tc.want {
			t.Errorf("%s: toast = %q, want %q", name, got, tc.want)
		}
	}
}

// The picker lists spaces and the projects right under them, a followed project deeper down has no row there.
// It still has to be in the box, or the next apply drops it while the box counts only the listed rows.
func TestScopesDialogListsAFollowedScopeThePickerDoesNot(t *testing.T) {
	items, followed := testPicker()
	followed = append(followed, store.Scope{ID: "BIL", Kind: store.ScopeKindProject, Title: "Billing", Followed: true})
	d, _ := newScopesDialog(items, followed)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	var dl dialog = d
	if view := dl.View(th, 60, 40); !strings.Contains(view, "[x] Billing") || !strings.Contains(view, "nothing to change") {
		t.Fatalf("the box should list Billing ticked with nothing to change:\n%s", view)
	}
	dl = typeRunes(dl, "api")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	_, cmd := dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 2 {
		t.Fatalf("enter should submit and close, got %#v", msgs)
	}
	got, ok := msgs[0].(submitScopesMsg)
	var ids []string
	for _, sc := range got.scopes {
		ids = append(ids, sc.ID)
	}
	if !ok || !reflect.DeepEqual(ids, []string{"MOB", "PLT", "API", "BIL"}) || got.toast != "Following API" {
		t.Errorf("submitted %v with toast %q, want Billing kept", ids, got.toast)
	}
	d, _ = newScopesDialog(items, followed)
	dl = typeRunes(d, "bil")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	if view := dl.View(th, 60, 40); !strings.Contains(view, "enter unfollows 1") {
		t.Errorf("unticking Billing should count as an unfollow:\n%s", view)
	}
	_, cmd = dl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs = collect(cmd); len(msgs) != 2 || msgs[0].(submitScopesMsg).toast != "No longer following Billing" {
		t.Errorf("unticking Billing should submit its unfollow, got %#v", msgs)
	}
}

func TestScopesDialogSaysWhenTheCacheHasNoSpaces(t *testing.T) {
	followed := []store.Scope{{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true}}
	d, _ := newScopesDialog(pickerItems(pickerLoadedMsg{}, nil), followed)
	view := d.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40)
	if !strings.Contains(view, "No spaces in the cache yet") || strings.Contains(view, "nothing to change") {
		t.Errorf("an empty picker should say the spaces are still to come:\n%s", view)
	}
}

// The spaces can land while the box is open, after a cache reset for example, and the rebuilt box keeps what was ticked.
func TestScopesDialogReloadKeepsTheTicks(t *testing.T) {
	items, followed := testPicker()
	d, _ := newScopesDialog(items, followed)
	var dl dialog = d
	dl = typeRunes(dl, "api")
	dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeySpace})
	for range 3 {
		dl, _ = dl.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	d = dl.(scopesDialog)
	d, _ = d.reload(pickerLoadedMsg{
		spaces:   []store.Space{{ID: "MOB", Title: "Mobile"}, {ID: "NEW", Title: "Newcomer"}, {ID: "PLT", Title: "Platform"}},
		projects: map[string][]store.Folder{"PLT": {{ID: "API", Title: "API"}}},
		followed: followed,
	})
	view := d.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 60, 40)
	for _, want := range []string{"[ ] Newcomer", "[x] API", "[x] Mobile", "enter follows 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("after the reload the box lacks %q:\n%s", want, view)
		}
	}
}
