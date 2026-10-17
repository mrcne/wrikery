package ui_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/mrcne/wrikery/internal/config"
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

// browserOptions is a first run whose config names an API app, with a fake sign in hook that hands the test every call.
func browserOptions(t *testing.T, st *store.Store, secret string) (ui.Options, chan ui.SignInResult, chan string, chan struct{}) {
	t.Helper()
	opts := testOptions(st)
	opts.Demo = false
	opts.FirstRun = true
	opts.Config.OAuth = config.OAuthConfig{ClientID: "id1", ClientSecret: secret, RedirectURI: "http://localhost:8731/callback"}
	result := make(chan ui.SignInResult, 1)
	pasted := make(chan string, 1)
	cancelled := make(chan struct{}, 1)
	opts.Hooks.SignIn = func(string) (ui.SignIn, error) {
		return ui.SignIn{
			URL:       "https://login.wrike.com/oauth2/authorize/v4?client_id=id1&state=s",
			Listening: true,
			Result:    result,
			Paste: func(address string) error {
				if !strings.Contains(address, "state=") {
					return errors.New("paste the whole address the browser landed on")
				}
				pasted <- address
				return nil
			},
			Cancel: func() { cancelled <- struct{}{} },
		}, nil
	}
	return opts, result, pasted, cancelled
}

func TestFirstRunPasteErrorKeepsTheAttempt(t *testing.T) {
	opts, result, pasted, _ := browserOptions(t, freshStore(t), "s3cret")
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Sign in through the browser")
	press(tm, "enter")
	waitFor(t, tm, "login.wrike.com")
	press(tm, "c0de", "enter")
	waitFor(t, tm, "paste the whole address")
	// The attempt is still there, a right paste after the wrong one completes it.
	press(tm, "http://localhost:8731/callback?code=c0de&state=s", "enter")
	select {
	case <-pasted:
	case <-time.After(3 * time.Second):
		t.Fatal("the second paste never reached the hook")
	}
	result <- ui.SignInResult{Name: "Ada Nowak", App: "id1"}
	waitFor(t, tm, "Hello Ada Nowak")
}

func TestFirstRunSaysWhenNoListenerRuns(t *testing.T) {
	opts, _, _, _ := browserOptions(t, freshStore(t), "s3cret")
	inner := opts.Hooks.SignIn
	opts.Hooks.SignIn = func(secret string) (ui.SignIn, error) {
		si, err := inner(secret)
		si.Listening = false
		return si, err
	}
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Sign in through the browser")
	press(tm, "enter")
	waitFor(t, tm, "paste the address the browser lands on, nothing listens on the port")
}

func TestFirstRunTabIsIgnoredWhileTheSignInStarts(t *testing.T) {
	opts, _, _, _ := browserOptions(t, freshStore(t), "s3cret")
	inner := opts.Hooks.SignIn
	release := make(chan struct{})
	opts.Hooks.SignIn = func(secret string) (ui.SignIn, error) {
		<-release
		return inner(secret)
	}
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Sign in through the browser")
	press(tm, "enter")
	waitFor(t, tm, "starting the sign in")
	press(tm, "tab")
	time.Sleep(100 * time.Millisecond)
	close(release)
	waitFor(t, tm, "waiting for the browser")
	if seen := seenOutput(t, tm); strings.Contains(seen.String(), "Paste a permanent access token") {
		t.Fatal("tab switched to the token field while the sign in was starting")
	}
}

func TestReauthNoticeForATokenWithTheBrowserBox(t *testing.T) {
	st := seededStore(t)
	opts, _, _, _ := browserOptions(t, st, "s3cret")
	opts.FirstRun = false
	opts.SignedInWith = ui.SignedInWithToken
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Tasks")
	tm.Send(ui.SyncStateMsg{State: "auth_required"})
	waitFor(t, tm, "Wrike did not accept the stored token. Sign in again to continue.")
	if seen := seenOutput(t, tm); strings.Contains(seen.String(), "Paste it again") {
		t.Fatal("the notice asks for a paste above the browser box")
	}
}

func freshStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestFirstRunBrowserShowsTheAddressAndTakesAPaste(t *testing.T) {
	opts, result, pasted, _ := browserOptions(t, freshStore(t), "s3cret")
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Sign in through the browser")
	press(tm, "enter")
	waitFor(t, tm, "login.wrike.com")
	address := "http://localhost:8731/callback?code=c0de&state=s"
	press(tm, address, "enter")
	select {
	case got := <-pasted:
		if got != address {
			t.Fatalf("pasted %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the pasted address never reached the hook")
	}
	result <- ui.SignInResult{Name: "Ada Nowak"}
	waitFor(t, tm, "Hello Ada Nowak")
}

func TestFirstRunBrowserNeedsTheSecret(t *testing.T) {
	opts, _, _, _ := browserOptions(t, freshStore(t), "")
	opts.Hooks.SignIn = func(string) (ui.SignIn, error) {
		t.Error("the hook ran without a secret")
		return ui.SignIn{}, nil
	}
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Client secret")
	press(tm, "enter")
	waitFor(t, tm, "the client secret is needed")
}

func TestFirstRunTabSwitchesToTheTokenField(t *testing.T) {
	opts, _, _, _ := browserOptions(t, freshStore(t), "s3cret")
	verified := make(chan string, 1)
	opts.Hooks.VerifyToken = func(_ context.Context, token string) (string, error) {
		verified <- token
		return "Ada Nowak", nil
	}
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Sign in through the browser")
	press(tm, "tab")
	waitFor(t, tm, "Paste a permanent access token")
	press(tm, "abc", "enter")
	if got := <-verified; got != "abc" {
		t.Fatalf("verified %q", got)
	}
	waitFor(t, tm, "Hello Ada Nowak")
}

func TestFirstRunEscCancelsTheAttempt(t *testing.T) {
	opts, result, _, cancelled := browserOptions(t, freshStore(t), "s3cret")
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Sign in through the browser")
	press(tm, "enter")
	waitFor(t, tm, "waiting for the browser")
	press(tm, "esc")
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("esc did not cancel the attempt")
	}
	// The cancelled attempt still answers, late, and that answer must not sign anybody in.
	result <- ui.SignInResult{Name: "Late"}
	time.Sleep(100 * time.Millisecond)
	press(tm, "tab")
	waitFor(t, tm, "Paste a permanent access token")
	if seen := seenOutput(t, tm); strings.Contains(seen.String(), "Hello Late") {
		t.Fatal("the late answer of a cancelled attempt was taken")
	}
}

func TestReauthNoticeNamesTheSession(t *testing.T) {
	st := seededStore(t)
	opts, _, _, _ := browserOptions(t, st, "s3cret")
	opts.FirstRun = false
	opts.SignedInWith = ui.SignedInWithBrowser
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Tasks")
	tm.Send(ui.SyncStateMsg{State: "auth_required"})
	waitFor(t, tm, "Wrike no longer accepts the session")
	waitFor(t, tm, "sign in rejected")
}

func TestFirstRunPickerStartsFromTheFollowedSet(t *testing.T) {
	st := freshStore(t)
	ctx := context.Background()
	// A sign out and a new sign in run the first run over a cache that still follows spaces, the picker must show that set.
	if err := st.Spaces().ReplaceAll(ctx, []store.Space{{ID: demo.SpacePlatform, Title: "Platform"}, {ID: demo.SpaceMobile, Title: "Mobile"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Folders().ReplaceTree(ctx, []store.Folder{
		{ID: demo.SpacePlatform, Title: "Platform", Space: true},
		{ID: demo.SpaceMobile, Title: "Mobile", Space: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Scopes().SetFollowed(ctx, []store.Scope{{ID: demo.SpacePlatform, Kind: store.ScopeKindSpace, Title: "Platform", Followed: true}}); err != nil {
		t.Fatal(err)
	}
	opts := testOptions(st)
	opts.Demo = false
	opts.FirstRun = true
	opts.Hooks.VerifyToken = func(context.Context, string) (string, error) { return "Ada Nowak", nil }
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Paste a permanent access token")
	press(tm, "abc", "enter")
	waitFor(t, tm, "[x] Platform")
	if seen := seenOutput(t, tm); !strings.Contains(seen.String(), "[ ] Mobile") {
		t.Fatal("an unfollowed space shows as ticked")
	}
}

func TestFirstRunCtrlYCopiesTheAddress(t *testing.T) {
	opts, _, _, _ := browserOptions(t, freshStore(t), "s3cret")
	var copied []string
	opts.Hooks.Copy = func(text string) error {
		copied = append(copied, text)
		return nil
	}
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Sign in through the browser")
	press(tm, "enter")
	waitFor(t, tm, "waiting for the browser")
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlY})
	waitFor(t, tm, "address copied")
	if len(copied) != 1 || copied[0] != "https://login.wrike.com/oauth2/authorize/v4?client_id=id1&state=s" {
		t.Fatalf("copied %q", copied)
	}
}

func TestFirstRunShowsNoKeyHints(t *testing.T) {
	opts := testOptions(freshStore(t))
	opts.Demo = false
	opts.FirstRun = true
	tm := teatest.NewTestModel(t, ui.New(opts), teatest.WithInitialTermSize(120, 30))
	waitFor(t, tm, "Paste a permanent access token")
	// None of the main screen keys work under the box, and q is swallowed by the field.
	if seen := seenOutput(t, tm); strings.Contains(seen.String(), "ctrl+f search") || strings.Contains(seen.String(), "q quit") {
		t.Fatal("the first run frame carries the main screen key hints")
	}
}
