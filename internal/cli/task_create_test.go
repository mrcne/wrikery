package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func TestTaskCreateSendsTheTaskAndPrintsTheRealId(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	var gotPath, gotForm string
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		_ = r.ParseForm()
		gotForm = r.Form.Encode()
		_, _ = fmt.Fprintf(w, taskAnswer, "TASKNEW", "Try the new command", "Active", "ST_NEW")
	}))
	if code := Run(context.Background(), env, []string{"task", "create", "--folder", "later", "Try", "the", "new", "command"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if gotPath != "POST /folders/PROJ1/tasks" {
		t.Errorf("request = %q", gotPath)
	}
	if !strings.Contains(gotForm, "title=Try+the+new+command") || strings.Contains(gotForm, "responsibles") {
		t.Errorf("form = %q, want the title and nobody assigned", gotForm)
	}
	if !strings.Contains(out.String(), "created TASKNEW  Try the new command  in 4 Later") {
		t.Errorf("out:\n%s", out.String())
	}
	if _, err := env.Store.Tasks().Get(context.Background(), "TASKNEW"); err != nil {
		t.Errorf("the server row is not in the cache: %v", err)
	}
}

func TestTaskCreateOfflinePrintsTheLocalIdAndQueues(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "create", "--folder", "PROJ1", "Offline task"}); code != exitQueued {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "queued local:") || !strings.Contains(out.String(), "Offline task  in 4 Later") {
		t.Errorf("out:\n%s", out.String())
	}
	task, err := env.Store.Tasks().Get(context.Background(), "local:1")
	if err != nil || task.CustomStatusID != "ST_NEW" || task.Status != "Active" {
		t.Errorf("optimistic row = %+v, %v, want the first active status of the standard workflow", task, err)
	}
}

func TestTaskCreateJSONCarriesSentAndTheId(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, taskAnswer, "TASKNEW", "JSON task", "Active", "ST_NEW")
	}))
	if code := Run(context.Background(), env, []string{"task", "create", "--folder", "PROJ1", "--json", "JSON", "task"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["id"] != "TASKNEW" || got["sent"] != true {
		t.Errorf("got = %v", got)
	}
}

func TestTaskCreateNeedsAFolderAndATitle(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "create", "No folder"}); code != exitUsage {
		t.Errorf("no folder code = %d", code)
	}
	if code := Run(context.Background(), env, []string{"task", "create", "--folder", "PROJ1", "   "}); code != exitUsage {
		t.Errorf("blank title code = %d, stderr %q", code, errOut.String())
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("a usage error queued a row")
	}
}

func TestTaskStatusOnALocalIdLandsAfterItsCreate(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	if _, err := env.Store.Outbox().EnqueueTaskCreate(ctx, "PROJ1", store.TaskCreatePayload{Title: "Fresh"}, "ST_NEW"); err != nil {
		t.Fatal(err)
	}
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /folders/PROJ1/tasks":
			_, _ = fmt.Fprintf(w, taskAnswer, "TASKNEW", "Fresh", "Active", "ST_NEW")
		case "PUT /tasks/TASKNEW":
			_, _ = fmt.Fprintf(w, taskAnswer, "TASKNEW", "Fresh", "Active", "ST_PROG")
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	if code := Run(ctx, env, []string{"task", "status", "local:1", "In Progress"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "now: Fresh  [In Progress]") {
		t.Errorf("out:\n%s", out.String())
	}
	task, err := env.Store.Tasks().Get(ctx, "TASKNEW")
	if err != nil || task.CustomStatusID != "ST_PROG" {
		t.Errorf("task = %+v, %v, want ST_PROG", task, err)
	}
}
