package ui_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

func TestFirstRunFlow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	verified := make(chan string, 1)
	opts := testOptions(st)
	opts.Demo = false
	opts.FirstRun = true
	opts.Hooks.VerifyToken = func(_ context.Context, token string) (string, error) {
		verified <- token
		return "Ada Nowak", nil
	}
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Paste a permanent access token")
	press(tm, "abc", "enter")
	if got := <-verified; got != "abc" {
		t.Fatalf("verified %q", got)
	}
	waitFor(t, tm, "Hello Ada Nowak")

	// The engine would have pulled these by now. Seed and send the hint the bridge would send.
	if err := st.Spaces().ReplaceAll(ctx, []store.Space{{ID: demo.SpacePlatform, Title: "Platform"}, {ID: demo.SpaceMobile, Title: "Mobile"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Folders().ReplaceTree(ctx, []store.Folder{
		{ID: demo.SpacePlatform, Title: "Platform", Space: true, ChildIDs: []string{demo.ProjectAPI}},
		{ID: demo.ProjectAPI, Title: "API", Project: &store.Project{Status: "Green"}},
		{ID: demo.SpaceMobile, Title: "Mobile", Space: true},
	}); err != nil {
		t.Fatal(err)
	}
	tm.Send(ui.StoreChangedMsg{Entities: []string{"spaces", "folders"}})
	waitFor(t, tm, "Platform")
	// The store lists spaces by title, so the rows are My tasks (locked), Mobile, Platform and the API project under it.
	press(tm, "j", "j", "space", "enter")

	waitFor(t, tm, "Syncing")
	scopes, err := st.Scopes().Followed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, sc := range scopes {
		ids = append(ids, sc.ID)
	}
	if !slices.Contains(ids, demo.SpacePlatform) || slices.Contains(ids, demo.SpaceMobile) {
		t.Fatalf("followed = %v", ids)
	}

	if err := st.Tasks().ApplyPage(ctx, demo.SpacePlatform, nil, "2026-09-03T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	tm.Send(ui.StoreChangedMsg{Entities: []string{"tasks"}})
	waitFor(t, tm, "v Platform") // ASCII completed glyph marks a synced scope
	press(tm, "enter")
	waitFor(t, tm, "Tasks")
	_ = finalView(t, tm)
}

// The box clips each body line, so an instruction wider than the inner width silently loses its tail.
func TestFirstRunKeepsALongTokenWhole(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	// A permanent token is a JWT well over 300 characters, this one is shaped like a real one.
	token := "eyJ0dCI6InAiLCJhbGciOiJIUzI1NiIsInR2IjoiMiJ9." + strings.Repeat("ab", 140) + "." + strings.Repeat("c", 43)
	verified := make(chan string, 1)
	opts := testOptions(st)
	opts.Demo = false
	opts.FirstRun = true
	opts.Hooks.VerifyToken = func(_ context.Context, got string) (string, error) {
		verified <- got
		return "Ada Nowak", nil
	}
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Paste a permanent access token")
	press(tm, token, "enter")
	if got := <-verified; got != token {
		t.Fatalf("verified %d characters, want %d: the input cut the token", len(got), len(token))
	}
	waitFor(t, tm, "Hello Ada Nowak")
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

func TestFirstRunShowsTheTokenLink(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "link.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	opts := testOptions(st)
	opts.Demo = false
	opts.FirstRun = true
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "appconsole.htm?#/api")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestAuthRequiredReturnsToTokenStep(t *testing.T) {
	st := seededStore(t)
	opts := testOptions(st)
	opts.Hooks.VerifyToken = func(context.Context, string) (string, error) { return "Ada Nowak", nil }
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Tasks")
	tm.Send(ui.SyncStateMsg{State: "auth_required"})
	waitFor(t, tm, "Wrike did not accept the stored token")

	// The pane titles were on screen before the box covered them, so only the frames drawn from here on prove the main screen is back.
	from := mark(t, tm)
	press(tm, "newtoken", "enter")
	waitAfter(t, tm, from, "Tasks")
	view := finalView(t, tm)
	if strings.Contains(view, "Follow") {
		t.Errorf("a second token walked into the scope picker:\n%s", view)
	}
}
