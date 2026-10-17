package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/store"
)

// uiStub listens like an interface on a socket in a short temp dir, records every request and answers each with reply.
// A t.TempDir() path can run past the length a unix socket path may have on macOS.
func uiStub(t *testing.T, env Env, reply control.Reply) (Env, *[]control.Request) {
	t.Helper()
	dir, err := os.MkdirTemp("", "wk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var got []control.Request
	l, err := control.Listen(filepath.Join(dir, "ui.sock"), func(req control.Request) control.Reply {
		got = append(got, req)
		return reply
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	env.Socket = filepath.Join(dir, "ui.sock")
	return env, &got
}

func TestUIFilterSendsTheLevelsPersonAndStatuses(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "showing High, Low | Anna Nowak | In Progress"})
	if code := Run(context.Background(), env, []string{"ui", "filter", "--importance", "high", "--importance", "Low", "--person", "nowak", "--status", "in progress"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if len(*got) != 1 {
		t.Fatalf("requests = %d, want one", len(*got))
	}
	req := (*got)[0]
	if req.Op != control.OpFilter || strings.Join(req.Importance, ",") != "High,Low" || req.Person != "U2" || strings.Join(req.Statuses, ",") != "in progress" {
		t.Errorf("request = %+v, want the levels as Wrike spells them, the contact id and the status as typed", req)
	}
	if out.String() != "showing High, Low | Anna Nowak | In Progress\n" {
		t.Errorf("out = %q, want the interface's line", out.String())
	}
}

func TestUIFilterClearAndUsage(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "filter cleared"})
	if code := Run(context.Background(), env, []string{"ui", "filter", "--clear"}); code != exitOK {
		t.Fatalf("clear code = %d, stderr %q", code, errOut.String())
	}
	if len(*got) != 1 || (*got)[0].Op != control.OpFilter || len((*got)[0].Importance)+len((*got)[0].Statuses) != 0 || (*got)[0].Person != "" {
		t.Errorf("clear sent %+v, want an empty filter", *got)
	}
	if code := Run(context.Background(), env, []string{"ui", "filter"}); code != exitUsage {
		t.Errorf("no flag code = %d, want %d", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"ui", "filter", "--clear", "--person", "me"}); code != exitUsage {
		t.Errorf("clear with a flag code = %d, want %d", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"ui", "filter", "--importance", "urgent"}); code != exitUsage {
		t.Errorf("bad level code = %d, want %d", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"ui", "filter", "--person", " "}); code != exitUsage {
		t.Errorf("blank person code = %d, want %d, a blank must not clear the filter", code, exitUsage)
	}
	if len(*got) != 1 {
		t.Errorf("a usage error reached the interface: %+v", *got)
	}
}

func TestUIFilterChecksTheStatusNamesAndThePerson(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "showing New"})
	if code := Run(context.Background(), env, []string{"ui", "filter", "--status", "Nope"}); code != exitError {
		t.Errorf("unknown status code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), `no status named "Nope"`) {
		t.Errorf("stderr = %q", errOut.String())
	}
	if code := Run(context.Background(), env, []string{"ui", "filter", "--person", "stranger"}); code != exitError {
		t.Errorf("unknown person code = %d, want %d", code, exitError)
	}
	if len(*got) != 0 {
		t.Errorf("a refused filter reached the interface: %+v", *got)
	}
	if code := Run(context.Background(), env, []string{"ui", "filter", "--status", "NEW", "--person", "me"}); code != exitOK {
		t.Errorf("case code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if code := Run(context.Background(), env, []string{"ui", "filter", "--person", "unassigned"}); code != exitOK {
		t.Errorf("unassigned code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if len(*got) != 2 || (*got)[0].Person != "me" || (*got)[0].Statuses[0] != "NEW" || (*got)[1].Person != "unassigned" {
		t.Errorf("requests = %+v, want me and unassigned passed as words", *got)
	}
}

func TestUIFilterFindsAPersonWhoLeft(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	if err := env.Store.Contacts().ReplaceAll(context.Background(), []store.Contact{
		{ID: "U1", FirstName: "Marcin", LastName: "Tester", Me: true},
		{ID: "U9", FirstName: "Former", LastName: "Colleague", Deleted: true},
		{ID: "G1", FirstName: "Design", Type: "Group"},
	}); err != nil {
		t.Fatal(err)
	}
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "showing Former Colleague"})
	// The filter box lists whoever is on a task in view, a former colleague included, so the command finds them too.
	if code := Run(context.Background(), env, []string{"ui", "filter", "--person", "former"}); code != exitOK {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if len(*got) != 1 || (*got)[0].Person != "U9" {
		t.Errorf("request = %+v, want the deleted contact's id", *got)
	}
	if code := Run(context.Background(), env, []string{"ui", "filter", "--person", "design"}); code != exitError {
		t.Errorf("group code = %d, want %d, a group is no person", code, exitError)
	}
}

func TestUISelectSendsTheTaskItsFoldersAndItsPeople(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "selected"})
	if code := Run(context.Background(), env, []string{"ui", "select", "headless"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if len(*got) != 1 || (*got)[0].Op != control.OpSelect || (*got)[0].TaskID != "TASK1" ||
		strings.Join((*got)[0].ParentIDs, ",") != "PROJ1" || strings.Join((*got)[0].ResponsibleIDs, ",") != "U1" {
		t.Errorf("request = %+v, want the task, its folders and its people", *got)
	}
	if out.String() != "selected Headless commands for scripts\n" {
		t.Errorf("out = %q", out.String())
	}
	if code := Run(context.Background(), env, []string{"ui", "select"}); code != exitUsage {
		t.Errorf("no task code = %d, want %d", code, exitUsage)
	}
}

func TestUIFolderSendsMeOrTheFolder(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "opened it"})
	if code := Run(context.Background(), env, []string{"ui", "folder", "me"}); code != exitOK {
		t.Fatalf("me code = %d\n%s", code, out.String())
	}
	if code := Run(context.Background(), env, []string{"ui", "folder", "later"}); code != exitOK {
		t.Fatalf("folder code = %d\n%s", code, out.String())
	}
	if len(*got) != 2 || (*got)[0].Op != control.OpFolder || (*got)[0].FolderID != "me" || (*got)[1].FolderID != "PROJ1" {
		t.Errorf("requests = %+v, want me as a word and the folder's id", *got)
	}
	if !strings.HasPrefix(out.String(), "opened it\n") {
		t.Errorf("out = %q, want the interface's line", out.String())
	}
}

func TestUIShapeOpenAndRefresh(t *testing.T) {
	env, _, _ := testEnv(t)
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "done"})
	for _, args := range [][]string{{"ui", "board"}, {"ui", "list"}, {"ui", "open", "timesheet"}, {"ui", "refresh"}} {
		if code := Run(context.Background(), env, args); code != exitOK {
			t.Errorf("%v code = %d", args, code)
		}
	}
	if code := Run(context.Background(), env, []string{"ui", "open", "garden"}); code != exitUsage {
		t.Errorf("bad screen code = %d, want %d", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"ui", "dance"}); code != exitUsage {
		t.Errorf("unknown ui command code = %d, want %d", code, exitUsage)
	}
	if len(*got) != 4 {
		t.Fatalf("requests = %+v, want four", *got)
	}
	if (*got)[0].Op != control.OpShape || (*got)[0].Shape != "board" || (*got)[1].Shape != "list" ||
		(*got)[2].Op != control.OpScreen || (*got)[2].Screen != "timesheet" || (*got)[3].Op != control.OpRefresh {
		t.Errorf("requests = %+v", *got)
	}
}

func TestUIWithoutAnInterfaceExitsOne(t *testing.T) {
	env, _, errOut := testEnv(t)
	env.Socket = filepath.Join(t.TempDir(), "none.sock")
	if code := Run(context.Background(), env, []string{"ui", "refresh"}); code != exitError {
		t.Errorf("code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "no wrikery interface is running") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestUIPrintsARefusal(t *testing.T) {
	env, out, errOut := testEnv(t)
	env, _ = uiStub(t, env, control.Reply{Error: "a box is open in the interface"})
	if code := Run(context.Background(), env, []string{"ui", "refresh"}); code != exitError {
		t.Errorf("code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "a box is open in the interface") || out.Len() != 0 {
		t.Errorf("stderr %q, stdout %q", errOut.String(), out.String())
	}
}

func TestUIJSON(t *testing.T) {
	env, out, _ := testEnv(t)
	env, _ = uiStub(t, env, control.Reply{OK: true, Text: "refreshing"})
	if code := Run(context.Background(), env, []string{"ui", "refresh", "--json"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["ok"] != true || got["text"] != "refreshing" {
		t.Errorf("got = %v", got)
	}
}

func TestUIWithoutAStoreSendsValuesAsGiven(t *testing.T) {
	env, out, _ := testEnv(t)
	env, got := uiStub(t, env, control.Reply{OK: true, Text: "ok"})
	// The demo: no cache a command could read, every value goes as typed.
	env.Store = nil
	for _, args := range [][]string{
		{"ui", "select", "IEAATASK03"},
		{"ui", "folder", "MOB"},
		{"ui", "filter", "--status", "In Progress", "--person", "me", "--importance", "high"},
	} {
		if code := Run(context.Background(), env, args); code != exitOK {
			t.Errorf("%v code = %d\n%s", args, code, out.String())
		}
	}
	if len(*got) != 3 || (*got)[0].TaskID != "IEAATASK03" || len((*got)[0].ParentIDs) != 0 || (*got)[1].FolderID != "MOB" ||
		(*got)[2].Statuses[0] != "In Progress" || (*got)[2].Person != "me" || (*got)[2].Importance[0] != "High" {
		t.Errorf("requests = %+v", *got)
	}
	if !strings.HasPrefix(out.String(), "selected IEAATASK03\n") {
		t.Errorf("out = %q, want the id where no title is known", out.String())
	}
}

func TestWriteCommandsAndSyncNudgeTheInterface(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, _, _, _ = recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"PROJ1"}))
	env, got := uiStub(t, env, control.Reply{OK: true})
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "In Progress"}); code != exitOK {
		t.Fatalf("status code = %d\n%s", code, out.String())
	}
	if len(*got) != 1 || (*got)[0].Op != control.OpReload || strings.Join((*got)[0].Entities, ",") != "tasks,comments,timelogs" {
		t.Errorf("after a write the interface got %+v, want one reload of what a write can touch", *got)
	}
	// The server's answer put the task back on New, so the same change goes out again, this time with nobody listening.
	socket := env.Socket
	env.Socket = filepath.Join(t.TempDir(), "none.sock")
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "In Progress"}); code != exitOK {
		t.Errorf("no interface must not change a write's outcome, code = %d\n%s", code, out.String())
	}
	env.Socket = socket
	// An offline sync still changed what it could, the counts, so it nudges too.
	env = withNetwork(t, env, nil)
	env.Socket = socket
	_ = Run(context.Background(), env, []string{"sync"})
	if len(*got) != 2 || (*got)[1].Op != control.OpReload || len((*got)[1].Entities) != 0 {
		t.Errorf("after a sync the interface got %+v, want a second reload of every kind", *got)
	}
}
