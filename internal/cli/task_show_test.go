package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/mrcne/wrikery/internal/store"
)

func seedThread(t *testing.T, st *store.Store) {
	t.Helper()
	err := st.Comments().ReplaceForTask(context.Background(), "TASK1", []store.Comment{
		{ID: "C1", TaskID: "TASK1", AuthorID: "U2", Text: "Can the list print JSON?", CreatedDate: "2026-10-01T09:30:00Z"},
		{ID: "C2", TaskID: "TASK1", AuthorID: "U1", Text: "Yes, with --json.", CreatedDate: "2026-10-02T08:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTaskShowInAPipePrintsPlainTextAndTheThread(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedThread(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show", "headless"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	got := out.String()
	for _, want := range []string{
		"Headless commands for scripts\n",
		"id          TASK1",
		"status      o New (Default Workflow)",
		"importance  High",
		"assignees   Marcin Tester",
		"dates       2026-10-01 -> 2026-10-03",
		"folders     4 Later",
		"link        https://www.wrike.com/open.htm?id=1",
		"Three flags, no subcommands.",
		"Anna Nowak",
		"Can the list print JSON?",
		"Yes, with --json.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("show lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<p>") {
		t.Errorf("a pipe got HTML:\n%s", got)
	}
}

func TestTaskShowLeavesEmptyPartsOut(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show", "TASK3"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	got := out.String()
	for _, absent := range []string{"assignees", "dates", "comments"} {
		if strings.Contains(got, absent) {
			t.Errorf("show of a bare task prints %q:\n%s", absent, got)
		}
	}
}

func TestTaskShowOnATerminalRendersTheDescription(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedThread(t, env.Store)
	env.Width = 80
	if code := Run(context.Background(), env, []string{"task", "show", "TASK1"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	golden.RequireEqual(t, out.Bytes())
}

func TestTaskShowJSONHasTheDescriptionAndComments(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedThread(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show", "TASK1", "--json"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["description_html"] != "<p>Three flags, <b>no</b> subcommands.</p>" || got["description_text"] != "Three flags, no subcommands." {
		t.Errorf("description = %q / %q", got["description_html"], got["description_text"])
	}
	comments := got["comments"].([]any)
	if len(comments) != 2 || comments[0].(map[string]any)["author"] != "Anna Nowak" {
		t.Errorf("comments = %v", comments)
	}
	out.Reset()
	if code := Run(context.Background(), env, []string{"task", "show", "TASK3", "--json"}); code != exitOK {
		t.Fatalf("bare code = %d", code)
	}
	if !strings.Contains(out.String(), `"comments": []`) {
		t.Errorf("bare task comments are not an empty array:\n%s", out.String())
	}
}

func TestTaskShowNeedsExactlyOneTask(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show"}); code != exitUsage {
		t.Errorf("no argument code = %d", code)
	}
	if code := Run(context.Background(), env, []string{"task", "show", "zzz"}); code != exitError {
		t.Errorf("unknown task code = %d", code)
	}
}
