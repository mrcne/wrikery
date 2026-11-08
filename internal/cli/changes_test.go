package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

// seedChanges queues four writes the way a session does: two went out, one is waiting and one Wrike refused,
// from the interface and from a command, with the store clock moved so the rows have different times.
func seedChanges(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	at := func(hour, minute int) {
		st.Now = func() time.Time { return time.Date(2026, 10, 3, hour, minute, 0, 0, time.UTC) }
	}
	must := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	at(9, 50)
	st.Source = store.SourceTUI
	sent := must(st.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{CustomStatusID: "ST_PROG"}))
	at(10, 0)
	if err := st.Outbox().Complete(ctx, sent); err != nil {
		t.Fatal(err)
	}
	st.Source = store.SourceCLI
	later := must(st.Outbox().EnqueueComment(ctx, "TASK4", "U1", "Pushed the branch"))
	at(11, 0)
	if err := st.Outbox().Complete(ctx, later); err != nil {
		t.Fatal(err)
	}
	at(11, 30)
	must(st.Outbox().EnqueueComment(ctx, "TASK2", "U1", "Queued while offline"))
	at(11, 48)
	st.Source = store.SourceTUI
	failed := must(st.Outbox().EnqueueTaskUpdate(ctx, "TASK3", store.TaskUpdatePayload{Importance: "High"}))
	if err := st.Outbox().Fail(ctx, failed, "wrike: 403 not_allowed"); err != nil {
		t.Fatal(err)
	}
	at(12, 0)
}

func TestChangesListsTheQueueThenTheHistory(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedChanges(t, env.Store)
	if code := Run(context.Background(), env, []string{"changes"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	want := []string{
		"queued             cli  Reach tasks outside the followed scopes  comment: Queued while offline",
		"failed 12 min ago  tui  Custom fields                            importance  Low -> High",
		"    wrike: 403 not_allowed",
		"1 h ago            cli  Add the licence file                     comment: Pushed the branch",
		"2 h ago            tui  Headless commands for scripts            status  New -> In Progress",
	}
	if got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestChangesSecondLineCarriesTheValuesTheSummaryCuts(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	if _, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, "TASK1", store.TaskUpdatePayload{Title: "A much longer title than the summary shows"}); err != nil {
		t.Fatal(err)
	}
	if code := Run(ctx, env, []string{"changes"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "title  Headless commands for... -> A much longer title t...") {
		t.Fatalf("lines = %q, want the cut summary and a second line", lines)
	}
	if lines[1] != "    title  Headless commands for scripts -> A much longer title than the summary shows" {
		t.Errorf("second line = %q, want the whole titles", lines[1])
	}
}

func TestChangesJSONCarriesTheStoredValuesNextToTheText(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedChanges(t, env.Store)
	if code := Run(context.Background(), env, []string{"changes", "--json"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("%v:\n%s", err, out.String())
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4:\n%s", len(rows), out.String())
	}
	first, last := rows[0], rows[3]
	for key, want := range map[string]any{
		"kind": string(store.KindCommentCreate), "state": string(store.StatePending), "source": store.SourceCLI,
		"task_id": "TASK2", "task_title": "Reach tasks outside the followed scopes",
		"summary": "comment: Queued while offline", "detail": "Queued while offline", "error": "", "created": "2026-10-03T11:30:00Z", "done": "",
	} {
		if first[key] != want {
			t.Errorf("first row %s = %#v, want %#v", key, first[key], want)
		}
	}
	if first["before"] != nil {
		t.Errorf("a comment replaced nothing, before = %#v", first["before"])
	}
	if payload, _ := first["payload"].(map[string]any); payload["text"] != "Queued while offline" {
		t.Errorf("first row payload = %#v", first["payload"])
	}
	for key, want := range map[string]any{
		"kind": string(store.KindTaskUpdate), "state": string(store.StateSent), "source": store.SourceTUI, "task_id": "TASK1",
		"summary": "status  New -> In Progress", "done": "2026-10-03T10:00:00Z", "id": float64(1),
	} {
		if last[key] != want {
			t.Errorf("last row %s = %#v, want %#v", key, last[key], want)
		}
	}
	before, _ := last["before"].(map[string]any)
	payload, _ := last["payload"].(map[string]any)
	if before["customStatusId"] != "ST_NEW" || payload["customStatusId"] != "ST_PROG" {
		t.Errorf("last row before = %#v, payload = %#v, want the status ids", last["before"], last["payload"])
	}
	if rows[1]["error"] != "wrike: 403 not_allowed" || rows[1]["state"] != string(store.StateFailed) {
		t.Errorf("failed row = %#v", rows[1])
	}
}

func TestChangesOnAnEmptyStorePrintsNothingInAPipe(t *testing.T) {
	env, out, errOut := testEnv(t)
	if code := Run(context.Background(), env, []string{"changes"}); code != exitOK || out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("code %d stdout %q stderr %q, want 0 and nothing", code, out.String(), errOut.String())
	}
	if code := Run(context.Background(), env, []string{"changes", "--json"}); code != exitOK || out.String() != "[]\n" {
		t.Errorf("json: code %d stdout %q, want an empty array", code, out.String())
	}
}

func TestChangesOnATerminalFramesTheRows(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedChanges(t, env.Store)
	env.Width = 120
	if code := Run(context.Background(), env, []string{"changes"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if !strings.HasPrefix(lines[0], "WHEN") || !strings.Contains(lines[0], "SOURCE") || !strings.Contains(lines[0], "TASK") || !strings.Contains(lines[0], "CHANGE") {
		t.Errorf("header = %q", lines[0])
	}
	if last := lines[len(lines)-1]; last != "4 changes, 1 queued, 1 failed" {
		t.Errorf("count line = %q", last)
	}
	out.Reset()
	env.Store.Now = func() time.Time { return time.Date(2026, 12, 3, 12, 0, 0, 0, time.UTC) }
	if _, err := env.Store.Outbox().EnqueueComment(context.Background(), "TASK1", "U1", "sweeps the history"); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), env, []string{"changes"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out.String(), "3 changes, 2 queued, 1 failed") {
		t.Errorf("two months later the history is gone and the queue stays:\n%s", out.String())
	}
}

func TestChangesTakesNoArguments(t *testing.T) {
	env, _, errOut := testEnv(t)
	if code := Run(context.Background(), env, []string{"changes", "TASK1"}); code != exitUsage || !strings.Contains(errOut.String(), "usage: wrikery changes") {
		t.Errorf("code %d stderr %q, want a usage error", code, errOut.String())
	}
}

// --source keeps the rows of one front end or agent word, --limit cuts the list after the first N rows, the queue first.
func TestChangesSourceAndLimitNarrowTheList(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedChanges(t, env.Store)
	ids := func(args ...string) string {
		t.Helper()
		out.Reset()
		if code := Run(context.Background(), env, append([]string{"changes", "--json"}, args...)); code != exitOK {
			t.Fatalf("%v: exit %d", args, code)
		}
		var rows []changeJSON
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.TaskID)
		}
		return strings.Join(got, " ")
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--source", "cli"}, "TASK2 TASK4"},
		{[]string{"--limit", "2"}, "TASK2 TASK3"},
		{[]string{"--source", "tui", "--limit", "1"}, "TASK3"},
		{[]string{"--source", "codex"}, ""},
	} {
		if got := ids(tc.args...); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.args, got, tc.want)
		}
	}
	env2, _, errOut := testEnv(t)
	if code := Run(context.Background(), env2, []string{"changes", "--limit", "-1"}); code != exitUsage || !strings.Contains(errOut.String(), "usage: wrikery changes") {
		t.Errorf("--limit -1: exit %d, stderr:\n%s", code, errOut.String())
	}
}

// A command retries nothing itself, so a queued row whose last send failed reads queued with the error under it, never retrying.
func TestChangesCallsAQueuedRowQueuedWhateverItsLastTry(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env.Store.Source = store.SourceCLI
	ctx := context.Background()
	id, err := env.Store.Outbox().EnqueueComment(ctx, "TASK1", "U1", "Soon")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Outbox().Reschedule(ctx, id, "wrike: 429 rate limit", "2026-10-03T12:05:00Z"); err != nil {
		t.Fatal(err)
	}
	if code := Run(ctx, env, []string{"changes"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(out.String(), "queued  cli  ") || !strings.Contains(out.String(), "\n    wrike: 429 rate limit\n") || strings.Contains(out.String(), "retrying") {
		t.Errorf("changes printed:\n%s", out.String())
	}
}

// On a terminal a line is cut to the width, a pipe gets the whole line. The test theme is ASCII, so bytes are cells.
func TestChangesOnATerminalCutsLinesToTheWidth(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedChanges(t, env.Store)
	env.Width = 50
	if code := Run(context.Background(), env, []string{"changes"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if len(line) > 50 {
			t.Errorf("%d cells wide: %q", len(line), line)
		}
	}
	if !strings.Contains(out.String(), "4 changes") {
		t.Errorf("changes printed:\n%s", out.String())
	}
}
