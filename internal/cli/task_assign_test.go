package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func TestTaskAssignSendsAddResponsiblesAndNamesThePeople(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, path, form, _ := recordWrite(t, env, answer("TASK2", "Reach tasks outside the followed scopes", "Normal", []string{"U1", "U2"}, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2", "me", "nowak"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *path != "PUT /tasks/TASK2" || form.Get("addResponsibles") != `["U1","U2"]` || form.Has("removeResponsibles") {
		t.Errorf("request = %q %v, want both ids added and nobody removed", *path, *form)
	}
	if !strings.Contains(out.String(), "was: Reach tasks outside the followed scopes  nobody\nnow: Reach tasks outside the followed scopes  Marcin Tester, Anna Nowak\n") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskAssignDropsAPersonAlreadyOnTheTask(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env, _, form, requests := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1", "U2"}, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK1", "me"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 0 {
		t.Errorf("requests = %d, want none when nobody is left to add", *requests)
	}
	if !strings.Contains(errOut.String(), "Marcin Tester is already on the task") || !strings.Contains(out.String(), "unchanged: Headless commands for scripts  Marcin Tester\n") {
		t.Errorf("stderr %q, stdout %q", errOut.String(), out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK1", "me", "Anna"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 1 || form.Get("addResponsibles") != `["U2"]` {
		t.Errorf("requests = %d, form = %v, want Anna alone added", *requests, *form)
	}
	if !strings.Contains(errOut.String(), "already on the task") || !strings.Contains(out.String(), "now: Headless commands for scripts  Marcin Tester, Anna Nowak\n") {
		t.Errorf("stderr %q, stdout %q", errOut.String(), out.String())
	}
}

func TestTaskUnassignSendsRemoveResponsibles(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, _, form, _ := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", nil, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "unassign", "TASK1", "Marcin Tester"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if form.Get("removeResponsibles") != `["U1"]` || form.Has("addResponsibles") {
		t.Errorf("form = %v, want the one id removed", *form)
	}
	if !strings.Contains(out.String(), "was: Headless commands for scripts  Marcin Tester\nnow: Headless commands for scripts  nobody\n") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskAssignRefusesAnUnknownOrAmbiguousPerson(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	if err := env.Store.Contacts().ReplaceAll(context.Background(), []store.Contact{
		{ID: "U1", FirstName: "Marcin", LastName: "Tester", Me: true},
		{ID: "U2", FirstName: "Anna", LastName: "Nowak"},
		{ID: "U3", FirstName: "Anna", LastName: "Kowalska"},
		{ID: "U4", FirstName: "Gone", LastName: "Person", Deleted: true},
	}); err != nil {
		t.Fatal(err)
	}
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2", "nobody-known"}); code != exitError {
		t.Errorf("unknown code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), `no contact matching "nobody-known"`) {
		t.Errorf("stderr = %q", errOut.String())
	}
	errOut.Reset()
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2", "anna"}); code != exitError {
		t.Errorf("ambiguous code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "matches 2 contacts") || !strings.Contains(errOut.String(), "Anna Kowalska") {
		t.Errorf("stderr = %q, want both Annas listed", errOut.String())
	}
	errOut.Reset()
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2", "gone"}); code != exitError {
		t.Errorf("deleted contact code = %d, want %d, a deleted contact is no match", code, exitError)
	}
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2"}); code != exitUsage {
		t.Errorf("nobody named code = %d, want %d", code, exitUsage)
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("a refused assign queued a row")
	}
}
