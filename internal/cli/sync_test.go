package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

// stubAccount answers every path a cycle requests with an envelope the client decodes.
// Only the contacts and the task a queued write touches carry data, the rest is empty lists.
func stubAccount() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/contacts"):
			_, _ = fmt.Fprint(w, `{"kind":"contacts","data":[{"id":"U1","firstName":"Marcin","lastName":"Tester","type":"Person","me":true}]}`)
		case r.URL.Path == "/tasks/TASK1":
			_, _ = fmt.Fprintf(w, taskAnswer, "TASK1", "Headless commands for scripts", "Active", "ST_PROG")
		default:
			_, _ = fmt.Fprint(w, `{"kind":"folders","data":[]}`)
		}
	})
}

func TestSyncRunsOneCycleAndPrintsTheCounts(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, stubAccount())
	if _, err := env.Store.Outbox().EnqueueTaskUpdate(context.Background(), "TASK1", store.TaskUpdatePayload{CustomStatusID: "ST_PROG", Status: "Active"}); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), env, []string{"sync"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "sync idle") || !strings.Contains(out.String(), "0 pending") || !strings.Contains(out.String(), "0 failed") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestSyncFullRunsTheSweep(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	var taskRequests int
	stub := stubAccount()
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tasks" {
			taskRequests++
		}
		stub.ServeHTTP(w, r)
	}))
	ctx := context.Background()
	// The first run pulls the me scope from scratch and gives it a cursor, so a sweep has to request it again.
	if code := Run(ctx, env, []string{"sync"}); code != exitOK {
		t.Fatalf("first sync: code = %d\n%s", code, out.String())
	}
	taskRequests = 0
	if code := Run(ctx, env, []string{"sync"}); code != exitOK {
		t.Fatalf("plain sync: code = %d\n%s", code, out.String())
	}
	if taskRequests != 1 {
		t.Errorf("plain sync made %d task requests, want the one pull", taskRequests)
	}
	taskRequests = 0
	if code := Run(ctx, env, []string{"sync", "--full"}); code != exitOK {
		t.Fatalf("full sync: code = %d\n%s", code, out.String())
	}
	if taskRequests != 2 {
		t.Errorf("sync --full made %d task requests, want the pull and the sweep", taskRequests)
	}
}

func TestSyncInterruptedSaysSo(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, stubAccount())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := Run(ctx, env, []string{"sync"}); code != exitError {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "sync interrupted") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestSyncInterruptedJSONSaysSo(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, stubAccount())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := Run(ctx, env, []string{"sync", "--json"}); code != exitError {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["state"] != "interrupted" {
		t.Errorf("got = %v", got)
	}
}

func TestSyncOfflineIsAnError(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	// sync has no deadline of its own, the client would retry the closed port with backoff, so the context bounds the test.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if code := Run(ctx, env, []string{"sync"}); code != exitError {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(errOut.String(), "wrikery:") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
}

func TestSyncJSON(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, stubAccount())
	if code := Run(context.Background(), env, []string{"sync", "--json"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["state"] != "idle" || got["pending"] != float64(0) || got["failed"] != float64(0) {
		t.Errorf("got = %v", got)
	}
}

func TestSyncTakesNoArguments(t *testing.T) {
	env, _, errOut := testEnv(t)
	if code := Run(context.Background(), env, []string{"sync", "extra"}); code != exitUsage {
		t.Fatalf("code = %d\n%s", code, errOut.String())
	}
}
