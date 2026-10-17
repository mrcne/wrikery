package cli

import (
	"context"
	"encoding/json"
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

func TestTaskAssignSkipsGroupsLikeTheAssigneeBox(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	if err := env.Store.Contacts().ReplaceAll(context.Background(), []store.Contact{
		{ID: "U1", FirstName: "Marcin", LastName: "Tester", Me: true},
		{ID: "U2", FirstName: "Anna", LastName: "Nowak"},
		{ID: "G1", FirstName: "Design", Type: "Group"},
		{ID: "G2", FirstName: "Anna team", Type: "Group"},
	}); err != nil {
		t.Fatal(err)
	}
	env, _, form, _ := recordWrite(t, env, answer("TASK2", "Reach tasks outside the followed scopes", "Normal", []string{"U2"}, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2", "design"}); code != exitError {
		t.Errorf("group by name code = %d, want %d", code, exitError)
	}
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2", "G1"}); code != exitError {
		t.Errorf("group by id code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "no contact matching") {
		t.Errorf("stderr = %q", errOut.String())
	}
	// A group with Anna in its name does not make the one person ambiguous.
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK2", "anna"}); code != exitOK {
		t.Errorf("person next to a group code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if form.Get("addResponsibles") != `["U2"]` {
		t.Errorf("form = %v, want Anna alone", *form)
	}
}

func TestTaskUnassignTakesOffAPersonWhoLeftOrIsUnknown(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	if err := env.Store.Contacts().ReplaceAll(ctx, []store.Contact{
		{ID: "U1", FirstName: "Marcin", LastName: "Tester", Me: true},
		{ID: "U2", FirstName: "Anna", LastName: "Nowak"},
		{ID: "U3", FirstName: "Anna", LastName: "Kowalska"},
		{ID: "U9", FirstName: "Former", LastName: "Colleague", Deleted: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Tasks().Upsert(ctx, []store.Task{{ID: "TASK5", Title: "Left behind", Status: "Active", CustomStatusID: "ST_NEW",
		ResponsibleIDs: []string{"U2", "U9", "U_GONE"}, ParentIDs: []string{"PROJ1"}, Permalink: "https://www.wrike.com/open.htm?id=5"}}); err != nil {
		t.Fatal(err)
	}
	env, _, form, requests := recordWrite(t, env, answer("TASK5", "Left behind", "Normal", []string{"U2"}, []string{"PROJ1"}))
	if code := Run(ctx, env, []string{"task", "unassign", "TASK5", "former", "U_GONE"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 1 || form.Get("removeResponsibles") != `["U9","U_GONE"]` {
		t.Errorf("requests = %d, form = %v, want the deleted contact by name and the unknown id taken off", *requests, *form)
	}
	if !strings.Contains(out.String(), "was: Left behind  Anna Nowak, Former Colleague, U_GONE\n") {
		t.Errorf("out:\n%s", out.String())
	}
	out.Reset()
	// Two Annas in the cache, one on the task: no ambiguity for an unassign.
	if code := Run(ctx, env, []string{"task", "unassign", "TASK5", "anna"}); code != exitOK {
		t.Fatalf("one anna on the task code = %d\n%s", code, out.String())
	}
	if *requests != 2 || form.Get("removeResponsibles") != `["U2"]` {
		t.Errorf("requests = %d, form = %v, want the Anna on the task", *requests, *form)
	}
}

func TestTaskUnassignDropsAPersonNotOnTheTask(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env, _, _, requests := recordWrite(t, env, answer("TASK2", "Reach tasks outside the followed scopes", "Normal", nil, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "unassign", "TASK2", "me"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 0 || !strings.Contains(errOut.String(), "Marcin Tester is not on the task") || !strings.Contains(out.String(), "unchanged: Reach tasks outside the followed scopes  nobody\n") {
		t.Errorf("requests = %d, stderr %q, stdout %q", *requests, errOut.String(), out.String())
	}
}

func TestTaskAssignUnchangedJSONSaysNotSent(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "assign", "TASK1", "me", "--json"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["id"] != "TASK1" || got["sent"] != false || got["unchanged"] != true {
		t.Errorf("got = %v, want the task with sent false and unchanged true", got)
	}
}
