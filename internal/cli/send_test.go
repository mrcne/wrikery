package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// The board's tasks are on that Wrike as the cache holds them now: a read of one by id, the check before an edit,
// answers its values from before the command's write, and the board's user and account answer the account check,
// the rest goes to handler.
func withNetwork(t *testing.T, env Env, handler http.Handler) Env {
	t.Helper()
	if handler != nil {
		handler = unchangedOnWrike(t, env.Store, handler)
	}
	return withServer(t, env, handler)
}

// withServer is withNetwork with handler alone, for a test that answers the account check itself.
func withServer(t *testing.T, env Env, handler http.Handler) Env {
	t.Helper()
	srv := httptest.NewServer(handler)
	if handler == nil {
		srv.Close()
	} else {
		t.Cleanup(srv.Close)
	}
	env.SignedIn = func() (bool, error) { return true, nil }
	env.Host = func(context.Context) (string, error) { return "www.wrike.com", nil } // the host the board's task links name
	env.Client = func(string) *wrike.Client { return wrike.New("test-token", wrike.WithBaseURL(srv.URL)) }
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
		out, reason, err := send(ctx, env, "www.wrike.com", second)
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
		out, reason, err := send(ctx, env, "www.wrike.com", first)
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
		{store.OutboxRow{Kind: store.KindTimelogUpdate, EntityID: "L1", TaskID: "T9"}, "a time entry on " + title},
		{store.OutboxRow{Kind: store.KindTimelogDelete, EntityID: "L1", TaskID: "T9"}, "a time entry on " + title},
		{store.OutboxRow{Kind: store.KindTimelogDelete, EntityID: "L0"}, "a time entry delete"},
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
	host, code, unchecked := preflight(ctx, env)
	if code != exitOK || unchecked != nil {
		t.Fatalf("preflight = %d, %v", code, unchecked)
	}
	out, reason, err := send(ctx, env, host, change)
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
	// The account was compared before, the closed port is for the send alone.
	if err := env.Store.AdoptHome(ctx, store.Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U1"}); err != nil {
		t.Fatal(err)
	}
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
	host, code, unchecked := preflight(ctx, env)
	if code != exitOK || unchecked != nil {
		t.Fatalf("preflight = %d, %v", code, unchecked)
	}
	out, reason, err := send(ctx, env, host, change)
	want := `waiting behind an earlier write that failed, a new task "Fresh": wrike: 503 no healthy upstream`
	if err != nil || out != queued || reason != want {
		t.Errorf("send = %v, %q, %v\nwant queued with %q", out, reason, err, want)
	}
}

// A row that went out during the command's own drain stays in the outbox as sent, the command reads the state and not the absence.
func TestSendReadsSentFromTheRowState(t *testing.T) {
	env, _, _ := testEnv(t)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, taskAnswer, "TASK1", "One", "Deferred", "ST_HOLD")
	}))
	queueUpdate(t, env, false)
	row, err := env.Store.Outbox().NextDue(context.Background(), "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	out, reason, err := send(context.Background(), env, "www.wrike.com", row.ID)
	if err != nil || out != sent || reason != "" {
		t.Fatalf("send = %v, %q, %v, want sent", out, reason, err)
	}
	if got, err := env.Store.Outbox().Get(context.Background(), row.ID); err != nil || got.State != store.StateSent {
		t.Errorf("row after send = %+v, %v, want kept as sent", got, err)
	}
}

// A row the user discards in the interface while the command waits for the lock must not be reported as queued,
// a script would sync and wait for a write that never goes out.
func TestSendReportsARowDiscardedMeanwhile(t *testing.T) {
	env, _, errOut := testEnv(t)
	env = withNetwork(t, env, nil)
	queueUpdate(t, env, false)
	row, err := env.Store.Outbox().NextDue(context.Background(), "2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Outbox().Discard(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	out, _, err := send(context.Background(), env, "www.wrike.com", row.ID)
	if err != nil || out != discarded {
		t.Fatalf("send = %v, %v, want discarded", out, err)
	}
	if code := reportWrite(context.Background(), env, out, "", false, false, store.Task{}, nil, ""); code != exitError {
		t.Errorf("code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "discarded") {
		t.Errorf("stderr = %q, want it to say the change was discarded", errOut.String())
	}
}

// The board's user and account, the answers of the account check, U1 is the board's me_contact_id.
const (
	boardMe      = `{"kind":"contacts","data":[{"id":"U1","firstName":"Marcin","lastName":"Tester","type":"Person","me":true}]}`
	boardAccount = `{"kind":"accounts","data":[{"id":"ACC1","name":"Acme"}]}`
)

// unchangedOnWrike answers GET /tasks/{id} for a task of the board with the values the cache holds when it is called.
func unchangedOnWrike(t *testing.T, st *store.Store, next http.Handler) http.Handler {
	t.Helper()
	answers := map[string][]byte{}
	for _, id := range boardTaskIDs {
		// Get and not ByIDs, the list read leaves the description out.
		task, err := st.Tasks().Get(context.Background(), id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		wt := map[string]any{"id": task.ID, "title": task.Title, "status": task.Status, "customStatusId": task.CustomStatusID,
			"importance": task.Importance, "description": task.Description, "permalink": task.Permalink,
			"parentIds": task.ParentIDs, "responsibleIds": task.ResponsibleIDs,
			"createdDate": task.CreatedDate, "updatedDate": task.UpdatedDate}
		if task.Dates != nil {
			wt["dates"] = map[string]any{"type": task.Dates.Type, "duration": task.Dates.Duration, "start": task.Dates.Start, "due": task.Dates.Due}
		}
		raw, err := json.Marshal(map[string]any{"kind": "tasks", "data": []any{wt}})
		if err != nil {
			t.Fatal(err)
		}
		answers["/tasks/"+task.ID] = raw
	}
	answers["/account"] = []byte(boardAccount)
	answers["/contacts"] = []byte(boardMe)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/contacts" && r.URL.Query().Get("me") != "true" {
			next.ServeHTTP(w, r)
			return
		}
		if raw, ok := answers[r.URL.Path]; ok && r.Method == http.MethodGet {
			_, _ = w.Write(raw)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// otherAccount is a Wrike where the token belongs to another person of another account.
func otherAccount() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contacts":
			_, _ = fmt.Fprint(w, `{"kind":"contacts","data":[{"id":"U2","firstName":"Anna","lastName":"Nowak","type":"Person","me":true}]}`)
		case "/account":
			_, _ = fmt.Fprint(w, `{"kind":"accounts","data":[{"id":"ACC2","name":"Other"}]}`)
		default:
			http.Error(w, "the write must not reach Wrike", http.StatusTeapot)
		}
	})
}

func TestAnEnvTokenOfAnotherAccountIsRefusedBeforeItIsQueued(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	if err := env.Store.AdoptHome(context.Background(), store.Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U1"}); err != nil {
		t.Fatal(err)
	}
	env = withServer(t, env, otherAccount())
	env.TokenFromEnv = true
	if code := Run(context.Background(), env, []string{"task", "comment", "TASK1", "hello"}); code != exitError {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	want := "wrikery: this sign in is for Other on www.wrike.com, the cache belongs to Acme on www.wrike.com, open wrikery to decide"
	if !strings.Contains(errOut.String(), want) {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
	if pending, failed, _ := env.Store.Outbox().Counts(context.Background()); pending+failed != 0 {
		t.Errorf("%d rows queued, want none", pending+failed)
	}
}

func TestALegacyCacheRefusesAnotherPerson(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withServer(t, env, otherAccount())
	if code := Run(context.Background(), env, []string{"task", "comment", "TASK1", "hello"}); code != exitError {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "open wrikery to decide") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestALegacyCacheAdoptsItsAccountOnTheFirstWrite(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"kind":"comments","data":[{"id":"C1","authorId":"U1","text":"hello","taskId":"TASK1","createdDate":"2026-10-23T10:00:00Z"}]}`)
	}))
	if code := Run(context.Background(), env, []string{"task", "comment", "TASK1", "hello"}); code != exitOK {
		t.Fatalf("code = %d\n%s\n%s", code, out.String(), errOut.String())
	}
	h, err := env.Store.Home(context.Background())
	if err != nil || h.AccountID != "ACC1" || h.Host != "www.wrike.com" || h.ContactID != "U1" {
		t.Errorf("record = %+v, %v, want the board's account adopted", h, err)
	}
}

// A row the check after a move held is not a rejection by Wrike, the command says it was held and how to send it anyway.
func TestAWriteHeldAfterTheMoveSaysSo(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	if err := env.Store.MoveHome(context.Background(), store.Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U1"}); err != nil {
		t.Fatal(err)
	}
	env = withServer(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/tasks/TASK1" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":"resource_not_found","errorDescription":"Task not found or access denied"}`)
			return
		}
		t.Errorf("%s %s, the held write must not be sent", r.Method, r.URL.Path)
	}))
	if code := Run(context.Background(), env, []string{"task", "comment", "TASK1", "hello"}); code != exitError {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	want := "wrikery: not sent, held after the move, not found in Acme on www.wrike.com, r on the changes screen in wrikery sends it anyway"
	if !strings.Contains(errOut.String(), want) {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
}
