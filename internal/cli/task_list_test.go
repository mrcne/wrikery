package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func TestTaskListDefaultsToMyOpenTasks(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "list"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "TASK1") {
		t.Errorf("lines = %q, want TASK1 alone, TASK4 is done", lines)
	}
	if !strings.Contains(lines[0], "New") || !strings.Contains(lines[0], "Headless commands for scripts") {
		t.Errorf("row = %q", lines[0])
	}
}

func TestTaskListAllIncludesDoneTasks(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "list", "--all"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out.String(), "TASK4") {
		t.Errorf("out = %q, want TASK4", out.String())
	}
}

func TestTaskListFolderTakesAFragmentAndRecursesFromASpace(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "list", "--folder", "later"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"TASK1", "TASK2", "TASK3"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("4 Later lacks %s:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "TASK4") {
		t.Errorf("4 Later shows TASK4 from another project")
	}
	out.Reset()
	if code := Run(context.Background(), env, []string{"task", "list", "--folder", "SPACE1", "--all"}); code != exitOK {
		t.Fatalf("space code = %d", code)
	}
	for _, want := range []string{"TASK1", "TASK2", "TASK3", "TASK4"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the space lacks %s:\n%s", want, out.String())
		}
	}
}

func TestTaskListJSONCarriesNamesAndNeverNull(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "list", "--folder", "4 Later", "--json"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	first := rows[0]
	if first["status"] != "New" || first["status_group"] != "Active" || first["pending"] != false {
		t.Errorf("first = %v", first)
	}
	resp := first["responsibles"].([]any)[0].(map[string]any)
	if resp["name"] != "Marcin Tester" {
		t.Errorf("responsible = %v", resp)
	}
	fold := first["folders"].([]any)[0].(map[string]any)
	if fold["title"] != "4 Later" {
		t.Errorf("folder = %v", fold)
	}
	if _, ok := first["description_text"]; ok {
		t.Errorf("a list row carries the description")
	}
	if rows[1]["responsibles"] == nil || rows[1]["dates"] != nil {
		t.Errorf("second row = %v, want an empty responsibles array and null dates", rows[1])
	}
	out.Reset()
	if code := Run(context.Background(), env, []string{"task", "list", "--folder", "3 Release", "--json"}); code != exitOK {
		t.Fatalf("empty code = %d", code)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Errorf("empty list = %q, want []", out.String())
	}
}

func TestTaskListOnATerminalAddsAHeaderAndACount(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env.Width = 100
	if code := Run(context.Background(), env, []string{"task", "list", "--folder", "later"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	got := out.String()
	if !strings.HasPrefix(got, "ID") || !strings.Contains(got, "STATUS") || !strings.Contains(got, "TITLE") {
		t.Errorf("no header:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "3 tasks") {
		t.Errorf("no count line:\n%s", got)
	}
	out.Reset()
	if code := Run(context.Background(), env, []string{"task", "list", "--folder", "SPACE1"}); code != exitOK {
		t.Fatalf("space code = %d", code)
	}
	if !strings.Contains(out.String(), "3 tasks, 1 done hidden") {
		t.Errorf("count line:\n%s", out.String())
	}
}

func TestTaskListRefusesTwoSelectorsAndAnUnknownMe(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "list", "--folder", "later", "--me"}); code != exitUsage {
		t.Errorf("two selectors code = %d", code)
	}
	env2, _, errOut2 := testEnv(t)
	if code := Run(context.Background(), env2, []string{"task", "list"}); code != exitError {
		t.Errorf("no me code = %d", code)
	}
	if !strings.Contains(errOut2.String(), "run wrikery sync first") {
		t.Errorf("stderr = %q", errOut2.String())
	}
}

func TestTaskListPendingMarkFollowsAQueuedWrite(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	if _, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, "TASK2", store.TaskUpdatePayload{Importance: "High"}); err != nil {
		t.Fatal(err)
	}
	if code := Run(ctx, env, []string{"task", "list", "--folder", "later"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	seen := false
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "TASK2") {
			seen = true
			if !strings.Contains(line, "TASK2 "+env.Theme.Glyphs.Pending) {
				t.Errorf("TASK2 row has no pending mark: %q", line)
			}
		}
	}
	if !seen {
		t.Errorf("no TASK2 row in:\n%s", out.String())
	}
	out.Reset()
	if code := Run(ctx, env, []string{"task", "list", "--folder", "later", "--json"}); code != exitOK {
		t.Fatalf("json code = %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	pending := map[any]any{}
	for _, row := range rows {
		pending[row["id"]] = row["pending"]
	}
	if pending["TASK2"] != true || pending["TASK1"] != false {
		t.Errorf("pending by id = %v, want TASK2 true and TASK1 false", pending)
	}
}

func TestTaskListAlignsTheStatusColumnForIdsOfDifferentLength(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	if _, err := env.Store.Outbox().EnqueueTaskCreate(ctx, "PROJ1", store.TaskCreatePayload{Title: "Fresh"}); err != nil {
		t.Fatal(err)
	}
	if code := Run(ctx, env, []string{"task", "list", "--folder", "later"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 4 || !strings.Contains(out.String(), "local:1") {
		t.Fatalf("want the local row next to the TASK rows:\n%s", out.String())
	}
	col := -1
	for _, line := range lines {
		// The first run of spaces ends the id cell, the status glyph is the next character.
		gap := strings.Index(line, "  ")
		if gap <= 0 {
			t.Fatalf("no status cell in %q", line)
		}
		i := gap + len(line[gap:]) - len(strings.TrimLeft(line[gap:], " "))
		if col >= 0 && i != col {
			t.Errorf("status column at %d, want %d in %q", i, col, line)
		}
		col = i
	}
}

func TestTaskListShowsARejectedWriteAsFailedNotPending(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	queueUpdate(t, env, true)
	if code := Run(context.Background(), env, []string{"task", "list"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.HasPrefix(out.String(), "TASK1 "+env.Theme.Glyphs.Failed) {
		t.Errorf("row = %q, want the failed glyph after the id", out.String())
	}
	out.Reset()
	if code := Run(context.Background(), env, []string{"task", "list", "--json"}); code != exitOK {
		t.Fatalf("json code = %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["failed"] != true || rows[0]["pending"] != false {
		t.Errorf("row = %v, want failed true and pending false", rows[0])
	}
}

func TestTaskListShowsAQueuedWriteAsPending(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	queueUpdate(t, env, false)
	if code := Run(context.Background(), env, []string{"task", "list", "--json"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["failed"] != false || rows[0]["pending"] != true {
		t.Errorf("row = %v, want pending true and failed false", rows[0])
	}
}
