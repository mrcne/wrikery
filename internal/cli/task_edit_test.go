package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// answer is a task as Wrike returns it after a write, with the fields the edit, assign and move commands change.
func answer(id, title, importance string, responsibles, parents []string) string {
	if responsibles == nil {
		responsibles = []string{}
	}
	r, _ := json.Marshal(responsibles)
	p, _ := json.Marshal(parents)
	return fmt.Sprintf(`{"kind":"tasks","data":[{"id":%q,"title":%q,"status":"Active","customStatusId":"ST_NEW","importance":%q,
"responsibleIds":%s,"parentIds":%s,"createdDate":"2026-09-08T10:00:00Z","updatedDate":"2026-10-15T12:00:00Z",
"permalink":"https://www.wrike.com/open.htm?id=9"}]}`, id, title, importance, r, p)
}

// recordWrite answers every request with body and keeps the last request's method, path and form.
func recordWrite(t *testing.T, env Env, body string) (Env, *string, *url.Values, *int) {
	t.Helper()
	var path string
	var form url.Values
	requests := 0
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		path = r.Method + " " + r.URL.Path
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = fmt.Fprint(w, body)
	}))
	return env, &path, &form, &requests
}

func TestTaskEditSendsTheTitleAndTheImportanceInOneWrite(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, path, form, requests := recordWrite(t, env, answer("TASK1", "New title", "Low", []string{"U1"}, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "edit", "headless", "--title", "New title", "--importance", "low"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *requests != 1 || *path != "PUT /tasks/TASK1" {
		t.Errorf("requests = %d %q, want one PUT on the task", *requests, *path)
	}
	if form.Get("title") != "New title" || form.Get("importance") != "Low" || form.Has("description") {
		t.Errorf("form = %v, want the title and the importance as Wrike spells it, no description", *form)
	}
	if !strings.Contains(out.String(), "was: Headless commands for scripts  [High]\nnow: New title  [Low]\n") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskEditMergesTheDescriptionAndKeepsTheUntouchedBlock(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	// The first paragraph is description_markdown as task show prints it, the second is new.
	env.Stdin = strings.NewReader("Three flags, **no** subcommands.\n\nRelated: the other task\n")
	env, _, form, _ := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "edit", "TASK1", "--description", "-"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if d := form.Get("description"); d != "<p>Three flags, <b>no</b> subcommands.</p><p>Related: the other task</p>" {
		t.Errorf("description = %q, want the stored block kept byte for byte and the new paragraph converted", d)
	}
	if form.Has("title") || form.Has("importance") {
		t.Errorf("form = %v, want the description alone", *form)
	}
	if !strings.Contains(out.String(), "now: Headless commands for scripts  [High]  description updated\n") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskEditUnchangedDescriptionQueuesNothing(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env.Stdin = strings.NewReader("Three flags, **no** subcommands.\n")
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "edit", "TASK1", "--description", "-"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(errOut.String(), "description unchanged") || !strings.Contains(out.String(), "unchanged: Headless commands for scripts  [High]\n") {
		t.Errorf("stderr %q, stdout %q", errOut.String(), out.String())
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("an unchanged description queued a row")
	}
}

func TestTaskEditNeedsATaskAndAFlagAndRefusesAnEmptyTitle(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "edit", "TASK1"}); code != exitUsage {
		t.Errorf("no flag code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "--title, --importance or --description") {
		t.Errorf("stderr = %q, want the flags named", errOut.String())
	}
	if code := Run(context.Background(), env, []string{"task", "edit", "TASK1", "--title", "  "}); code != exitUsage {
		t.Errorf("blank title code = %d, want %d", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"task", "edit", "TASK1", "TASK2", "--importance", "High"}); code != exitUsage {
		t.Errorf("two tasks code = %d, want %d", code, exitUsage)
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("a usage error queued a row")
	}
}
