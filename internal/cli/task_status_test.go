package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

func TestTaskStatusSendsTheChangeAndPrintsBeforeAndAfter(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	var gotForm string
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/tasks/TASK1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		gotForm = r.Form.Encode()
		_, _ = fmt.Fprintf(w, taskAnswer, "TASK1", "Headless commands for scripts", "Active", "ST_PROG")
	}))
	if code := Run(context.Background(), env, []string{"task", "status", "headless", "in progress"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(gotForm, "customStatus=ST_PROG") || strings.Contains(gotForm, "status=") {
		t.Errorf("form = %q, want the custom status id and no group", gotForm)
	}
	got := out.String()
	if !strings.Contains(got, "was: Headless commands for scripts  [New]") || !strings.Contains(got, "now: Headless commands for scripts  [In Progress]") {
		t.Errorf("out:\n%s", got)
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("%d rows still pending", pending)
	}
}

func TestTaskStatusLeavesTheWriteQueuedWhenWrikeIsUnreachable(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	start := time.Now()
	code := Run(context.Background(), env, []string{"task", "status", "TASK1", "On Hold"})
	if code != exitQueued {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the command took %s, the deadline is 300ms", time.Since(start))
	}
	if !strings.Contains(out.String(), "queued: Headless commands for scripts  [On Hold]") {
		t.Errorf("out:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "not on Wrike yet") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
	task, _ := env.Store.Tasks().Get(context.Background(), "TASK1")
	if task.CustomStatusID != "ST_HOLD" || task.Status != "Deferred" {
		t.Errorf("cache row = %+v, want the optimistic status", task)
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 1 {
		t.Errorf("%d rows pending, want the queued one", pending)
	}
}

func TestTaskStatusReportsARejectedWrite(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"errorDescription":"Parameter 'customStatus' value is invalid","error":"invalid_parameter"}`)
	}))
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "Completed"}); code != exitError {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(errOut.String(), "invalid_parameter") || !strings.Contains(errOut.String(), "sync issues") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
	if _, failed, _ := env.Store.Outbox().Counts(context.Background()); failed != 1 {
		t.Errorf("%d rows failed, want 1", failed)
	}
}

func TestTaskStatusRefusesAnUnknownNameAndListsTheWorkflow(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "Done"}); code != exitError {
		t.Fatalf("code = %d", code)
	}
	msg := errOut.String()
	for _, want := range []string{`no status "Done"`, "Default Workflow", "New, In Progress, On Hold, Completed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("stderr lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Old") {
		t.Errorf("a hidden status is offered:\n%s", msg)
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("a refused name queued a row")
	}
}

func TestTaskStatusWithoutATokenQueuesNothing(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "On Hold"}); code != exitError {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), "no token stored") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("a row was queued without a token")
	}
}

func TestTaskStatusOnALocalIdWaitsForTheCreate(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	// The create itself is blocked by a closed port, so the dependent status change has to stay queued too.
	env = withNetwork(t, env, nil)
	id, err := env.Store.Outbox().EnqueueTaskCreate(ctx, "PROJ1", store.TaskCreatePayload{Title: "Fresh"}, "ST_NEW")
	if err != nil {
		t.Fatal(err)
	}
	if code := Run(ctx, env, []string{"task", "status", store.LocalID(id), "In Progress"}); code != exitQueued {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "queued: Fresh  [In Progress]") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskStatusReportsAnotherSenderWhenTheLockIsHeld(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, taskAnswer, "TASK1", "Headless commands for scripts", "Active", "ST_PROG")
	}))
	// Another process holding the sync lock, taken on a descriptor of our own: flock contends per open file.
	f, err := os.OpenFile(env.LockFile, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "In Progress"}); code != exitQueued {
		t.Fatalf("code = %d\n%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "another wrikery is sending") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
	if !strings.Contains(out.String(), "queued: ") {
		t.Errorf("stdout:\n%s", out.String())
	}
}

func TestTaskStatusJSONReportsSent(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, taskAnswer, "TASK1", "Headless commands for scripts", "Active", "ST_PROG")
	}))
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "In Progress", "--json"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["sent"] != true || got["status"] != "In Progress" || got["pending"] != false {
		t.Errorf("got = %v", got)
	}
}

func TestTaskStatusInterruptedMidDrainStaysQueuedNotAnError(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server notices a dropped client only once the body is read.
		_ = r.ParseForm()
		cancel()
		<-r.Context().Done()
	}))
	env.Deadline = 5 * time.Second
	if code := Run(ctx, env, []string{"task", "status", "TASK1", "On Hold"}); code != exitQueued {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "queued: ") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskStatusJSONReportsQueued(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "status", "TASK1", "On Hold", "--json"}); code != exitQueued {
		t.Fatalf("code = %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["sent"] != false || got["pending"] != true {
		t.Errorf("got = %v", got)
	}
}
