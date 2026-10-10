package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// taskAnswer is what Wrike sends back for a task update or create, the envelope the client decodes.
const taskAnswer = `{"kind":"tasks","data":[{"id":"%s","title":"%s","status":"%s","customStatusId":"%s","importance":"Normal",
"responsibleIds":[],"parentIds":["PROJ1"],"createdDate":"2026-09-08T10:00:00Z","updatedDate":"2026-10-03T12:00:00Z",
"permalink":"https://www.wrike.com/open.htm?id=9"}]}`

// withNetwork gives env a token, a host and a client that talks to handler. A nil handler is a closed port.
func withNetwork(t *testing.T, env Env, handler http.Handler) Env {
	t.Helper()
	srv := httptest.NewServer(handler)
	if handler == nil {
		srv.Close()
	} else {
		t.Cleanup(srv.Close)
	}
	env.Token = func() (string, error) { return "test-token", nil }
	env.Host = func(ctx context.Context, token string) (string, error) { return "stub", nil }
	env.Client = func(token, host string) *wrike.Client { return wrike.New(token, wrike.WithBaseURL(srv.URL)) }
	env.LockFile = filepath.Join(t.TempDir(), "sync.lock")
	return env
}

// queueUpdate leaves a status change for TASK1 in the outbox, failed when asked.
func queueUpdate(t *testing.T, env Env, fail bool) {
	t.Helper()
	ctx := context.Background()
	id, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{CustomStatusID: "ST_HOLD", Status: "Deferred"})
	if err != nil {
		t.Fatal(err)
	}
	if fail {
		if err := env.Store.Outbox().Fail(ctx, id, "boom"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadBackKeepsTheFatesWordsWhenTheStoreCannotBeRead(t *testing.T) {
	for _, tc := range []struct {
		out         outcome
		wantPending bool
	}{{sent, false}, {queued, true}} {
		t.Run(tc.out.String(), func(t *testing.T) {
			env, _, errOut := testEnv(t)
			if err := env.Store.Close(); err != nil {
				t.Fatal(err)
			}
			before := store.Task{ID: "TASK1", Title: "Before"}
			ref := refData{pending: map[string]store.OutboxState{"TASK1": store.StateFailed}}
			got, ok := readBack(context.Background(), env, tc.out, "TASK1", before, &ref, true)
			if ok {
				t.Error("ok = true for a read that failed")
			}
			if got.Title != "Before" {
				t.Errorf("task = %+v, want the one from before the write", got)
			}
			if want := "wrikery: the change is " + tc.out.String() + ", reading the task back:"; !strings.Contains(errOut.String(), want) {
				t.Errorf("stderr = %q, want %q", errOut.String(), want)
			}
			if _, ok := ref.pending["TASK1"]; ok != tc.wantPending || ref.pending["TASK1"] == store.StateFailed {
				t.Errorf("marks = %v, want pending %v", ref.pending, tc.wantPending)
			}
		})
	}
}

func TestReportWriteWithoutReadBackPrintsOnlyTheFate(t *testing.T) {
	for _, tc := range []struct {
		out      outcome
		wantText string
		wantSent bool
		wantCode int
	}{
		{sent, "sent local:7\n", true, exitOK},
		{queued, "queued local:7\n", false, exitQueued},
	} {
		t.Run(tc.out.String(), func(t *testing.T) {
			env, out, _ := testEnv(t)
			fallback := store.Task{ID: "local:7", Title: "Before", Status: "Active"}
			ref := refData{}
			if code := reportWrite(context.Background(), env, tc.out, "slow", false, false, fallback, &ref, "Before  [Active]"); code != tc.wantCode {
				t.Errorf("text code = %d, want %d", code, tc.wantCode)
			}
			if out.String() != tc.wantText {
				t.Errorf("text = %q, want %q", out.String(), tc.wantText)
			}
			out.Reset()
			if code := reportWrite(context.Background(), env, tc.out, "slow", true, false, fallback, &ref, ""); code != tc.wantCode {
				t.Errorf("json code = %d, want %d", code, tc.wantCode)
			}
			var got map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 || got["id"] != "local:7" || got["sent"] != tc.wantSent || got["read_back"] != false {
				t.Errorf("json = %v, want only id, sent and read_back false", got)
			}
		})
	}
}

// The drain stops at the first write Wrike could not take. When that write is not the command's own,
// the reason has to say so, or the command prints another task's failure as its own.
func TestSendNamesTheEarlierWriteTheDrainStoppedAt(t *testing.T) {
	unavailable := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("no healthy upstream"))
	})
	ctx := context.Background()
	t.Run("behind another write", func(t *testing.T) {
		env, _, _ := testEnv(t)
		env = withNetwork(t, env, unavailable)
		if _, err := env.Store.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "Earlier"}); err != nil {
			t.Fatal(err)
		}
		second, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Title: "Later"})
		if err != nil {
			t.Fatal(err)
		}
		out, reason, err := send(ctx, env, "test-token", "stub", second)
		want := `waiting behind an earlier write that failed, a new task "Earlier": wrike: POST /folders/F1/tasks: http 503, Wrike is unavailable: no healthy upstream`
		if err != nil || out != queued || reason != want {
			t.Errorf("send = %v, %q, %v\nwant queued with %q", out, reason, err, want)
		}
	})
	t.Run("own write", func(t *testing.T) {
		env, _, _ := testEnv(t)
		env = withNetwork(t, env, unavailable)
		first, err := env.Store.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "Mine"})
		if err != nil {
			t.Fatal(err)
		}
		out, reason, err := send(ctx, env, "test-token", "stub", first)
		want := "wrike: POST /folders/F1/tasks: http 503, Wrike is unavailable: no healthy upstream"
		if err != nil || out != queued || reason != want {
			t.Errorf("send = %v, %q, %v\nwant queued with %q", out, reason, err, want)
		}
	})
}

// A time entry row holds the timelog id, which nobody can place, so the message names the task the entry is on,
// and a title is quoted as it is, %q would print the parts of a joined emoji with an escape between them.
func TestSubjectNamesTheWriteByItsKindAndItsTask(t *testing.T) {
	ctx := context.Background()
	env, _, _ := testEnv(t)
	family := "\U0001F468\u200d\U0001F469\u200d\U0001F467"
	if err := env.Store.Tasks().Upsert(ctx, []store.Task{{ID: "T9", Title: "Plan " + family + " trip", Status: "Active"}}); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Timelogs().Upsert(ctx, []store.Timelog{{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: "2026-10-01", Hours: 1}}); err != nil {
		t.Fatal(err)
	}
	title := `"Plan ` + family + ` trip"`
	cases := []struct {
		row  store.OutboxRow
		want string
	}{
		{store.OutboxRow{Kind: store.KindTaskUpdate, EntityID: "T9"}, "a change to " + title},
		{store.OutboxRow{Kind: store.KindCommentCreate, EntityID: "T9"}, "a comment on " + title},
		{store.OutboxRow{Kind: store.KindTimelogCreate, EntityID: "T9"}, "a time entry on " + title},
		{store.OutboxRow{Kind: store.KindTimelogUpdate, EntityID: "L1"}, "a time entry on " + title},
		{store.OutboxRow{Kind: store.KindTimelogDelete, EntityID: "L1"}, "a time entry delete"},
		{store.OutboxRow{Kind: store.KindTaskCreate, ID: 7}, "a new task local:7"},
		{store.OutboxRow{Kind: store.KindTaskUpdate, EntityID: "T0"}, "a change to T0"},
	}
	for _, c := range cases {
		if got := subject(ctx, env, c.row); got != c.want {
			t.Errorf("subject(%s %s) = %q, want %q", c.row.Kind, c.row.EntityID, got, c.want)
		}
	}
}

// Only a store failure makes the read back fail, Run has no way to inject one, so the helper is tried on its own.
func TestReadBackKeepsTheFateAndWarnsWhenTheStoreFails(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	before, err := env.Store.Tasks().Get(context.Background(), "TASK1")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Close(); err != nil {
		t.Fatal(err)
	}
	task, ok := readBack(context.Background(), env, sent, "TASK1", before, &refData{}, false)
	if ok || task.ID != "TASK1" {
		t.Errorf("read back = %+v ok %v, want the fallback and ok false", task, ok)
	}
	if !strings.Contains(errOut.String(), "the change is sent, reading the task back:") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
}

// The pass may stop at a failure on another task while this change is held behind a backed off write on its own task.
// That write keeps holding the change after the other task's goes through, so it is the one the reason names.
func TestSendNamesTheWriteOnItsOwnTaskOverTheOneThePassStoppedAt(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	// A comment create is not retried by the client, so the pass stops at it well inside the deadline.
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("no healthy upstream"))
	}))
	ctx := context.Background()
	older, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Importance: "High"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Outbox().Reschedule(ctx, older, "wrike: 503 earlier", "2999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Store.Outbox().EnqueueComment(ctx, "TASK2", "U1", "unrelated"); err != nil {
		t.Fatal(err)
	}
	change, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Importance: "Low"})
	if err != nil {
		t.Fatal(err)
	}
	token, host, code := preflight(ctx, env)
	if code != exitOK {
		t.Fatalf("preflight = %d", code)
	}
	out, reason, err := send(ctx, env, token, host, change)
	want := `waiting behind an earlier write that failed, a change to "Headless commands for scripts": wrike: 503 earlier`
	if err != nil || out != queued || reason != want {
		t.Errorf("send = %v, %q, %v\nwant queued with %q", out, reason, err, want)
	}
}

// A change to a task whose create is still queued waits for the create, which names the folder and not the task.
// The reason names the create and its failure instead of the bare wait.
func TestSendNamesTheCreateAChangeToANewTaskWaitsFor(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	ctx := context.Background()
	create, err := env.Store.Outbox().EnqueueTaskCreate(ctx, "PROJ1", store.TaskCreatePayload{Title: "Fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Outbox().Reschedule(ctx, create, "wrike: 503 no healthy upstream", "2999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	change, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, store.LocalID(create), store.TaskUpdatePayload{Importance: "High"})
	if err != nil {
		t.Fatal(err)
	}
	token, host, code := preflight(ctx, env)
	if code != exitOK {
		t.Fatalf("preflight = %d", code)
	}
	out, reason, err := send(ctx, env, token, host, change)
	want := `waiting behind an earlier write that failed, a new task "Fresh": wrike: 503 no healthy upstream`
	if err != nil || out != queued || reason != want {
		t.Errorf("send = %v, %q, %v\nwant queued with %q", out, reason, err, want)
	}
}
