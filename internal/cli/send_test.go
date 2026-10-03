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
