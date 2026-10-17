package cli

import (
	"context"
	"strings"
	"testing"
)

func TestTaskMoveSendsAddAndRemoveParents(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, path, form, _ := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"PROJ2"}))
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "3 Release", "--from", "later"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *path != "PUT /tasks/TASK1" || form.Get("addParents") != `["PROJ2"]` || form.Get("removeParents") != `["PROJ1"]` {
		t.Errorf("request = %q %v, want the new folder added and the old one removed", *path, *form)
	}
	if !strings.Contains(out.String(), "was: Headless commands for scripts  4 Later\nnow: Headless commands for scripts  3 Release\n") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskMoveToAloneKeepsTheOtherFolders(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, _, form, _ := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"PROJ1", "PROJ2"}))
	if code := Run(context.Background(), env, []string{"task", "move", "TASK1", "--to", "PROJ2"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if form.Get("addParents") != `["PROJ2"]` || form.Has("removeParents") {
		t.Errorf("form = %v, want the folder added and nothing removed", *form)
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
