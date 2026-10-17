package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func TestTaskMoveSendsAddAndRemoveParents(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, path, form, requests := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"PROJ2"}))
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "3 Release", "--from", "later"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 1 || *path != "PUT /tasks/TASK1" || form.Get("addParents") != `["PROJ2"]` || form.Get("removeParents") != `["PROJ1"]` {
		t.Errorf("request = %q %v, want the new folder added and the old one removed", *path, *form)
	}
	if !strings.Contains(out.String(), "was: Headless commands for scripts  4 Later\nnow: Headless commands for scripts  3 Release\n") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskMoveToAloneKeepsTheOtherFolders(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, _, form, requests := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"PROJ1", "PROJ2"}))
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "PROJ2"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 1 || form.Get("addParents") != `["PROJ2"]` || form.Has("removeParents") {
		t.Errorf("requests = %d, form = %v, want one write with the folder added and nothing removed", *requests, *form)
	}
	if !strings.Contains(out.String(), "now: Headless commands for scripts  4 Later, 3 Release\n") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskMoveRefusesLeavingTheLastFolderAndNeedsAFlag(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--from", "PROJ1"}); code != exitError {
		t.Errorf("last folder code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "a task needs a folder") {
		t.Errorf("stderr = %q", errOut.String())
	}
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1"}); code != exitUsage {
		t.Errorf("no flag code = %d, want %d", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "PROJ2", "--from", "PROJ2"}); code != exitUsage {
		t.Errorf("same folder both ways code = %d, want %d", code, exitUsage)
	}
	errOut.Reset()
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "PROJ1"}); code != exitOK {
		t.Errorf("already in code = %d, want %d\n%s", code, exitOK, errOut.String())
	}
	if !strings.Contains(errOut.String(), "already in 4 Later") || !strings.Contains(out.String(), "unchanged: Headless commands for scripts  4 Later\n") {
		t.Errorf("stderr %q, stdout %q", errOut.String(), out.String())
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("a refused move queued a row")
	}
}

func TestTaskMoveUsageErrorsNeedNoToken(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	env.Token = func() (string, error) { return "", fmt.Errorf("keychain locked") }
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "PROJ2", "--from", "PROJ2"}); code != exitUsage {
		t.Errorf("code = %d, want %d, the usage error comes before the token", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "PROJ1"}); code != exitOK {
		t.Errorf("already in code = %d, want %d, a no-op needs no token", code, exitOK)
	}
}

func TestTaskMoveSaysWhenNoFollowedFolderIsLeft(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	if err := env.Store.Folders().ReplaceTree(ctx, []store.Folder{
		{ID: "SPACE1", Title: "Wrikery", Space: true, ChildIDs: []string{"PROJ1", "PROJ2"}},
		{ID: "PROJ1", Title: "4 Later", Project: &store.Project{Status: "Green"}},
		{ID: "PROJ2", Title: "3 Release", Project: &store.Project{Status: "Green"}},
		{ID: "OTHER", Title: "Other team", Space: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Scopes().Upsert(ctx, store.Scope{ID: "SPACE1", Kind: store.ScopeKindSpace, Title: "Wrikery", Followed: true}); err != nil {
		t.Fatal(err)
	}
	env, _, _, requests := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"OTHER"}))
	if code := Run(ctx, env, []string{"task", "move", "TASK1", "--to", "OTHER", "--from", "PROJ1"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 1 || !strings.Contains(errOut.String(), "no followed") {
		t.Errorf("requests = %d, stderr = %q, want the write sent with a note that the task leaves the cache", *requests, errOut.String())
	}
	errOut.Reset()
	if code := Run(ctx, env, []string{"task", "move", "TASK1", "--to", "OTHER"}); code != exitOK {
		t.Fatalf("keep code = %d\n%s", code, out.String())
	}
	if strings.Contains(errOut.String(), "no followed") {
		t.Errorf("stderr = %q, want no note while a followed folder stays", errOut.String())
	}
}
