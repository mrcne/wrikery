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
	// glamour pads the description line to the width, the padding is not part of the contract.
	lines := strings.Split(out.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	golden.RequireEqual(t, []byte(strings.Join(lines, "\n")))
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

func TestTaskShowSaysWhatBecameOfAQueuedWrite(t *testing.T) {
	for name, tc := range map[string]struct {
		fail bool
		want string
	}{
		"pending": {false, "queued      a write is waiting to be sent\n"},
		"failed":  {true, "queued      a write was rejected, see the sync issues screen\n"},
	} {
		t.Run(name, func(t *testing.T) {
			env, out, _ := testEnv(t)
			seedBoard(t, env.Store)
			queueUpdate(t, env, tc.fail)
			if code := Run(context.Background(), env, []string{"task", "show", "TASK1"}); code != exitOK {
				t.Fatalf("code = %d", code)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("show lacks %q:\n%s", tc.want, out.String())
			}
		})
	}
}

// seedRelations makes TASK1 a subtask of TASK2 with one attachment, gives it a subtask of its own,
// and one edge each way: TASK4 before it, an uncached task after it.
func seedRelations(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	t1, err := st.Tasks().Get(ctx, "TASK1")
	if err != nil {
		t.Fatal(err)
	}
	t1.SuperTaskIDs, t1.AttachmentCount = []string{"TASK2"}, 1
	sub := store.Task{ID: "TASK9", Title: "Print JSON", Status: "Completed", CustomStatusID: "ST_DONE", SuperTaskIDs: []string{"TASK1"},
		CreatedDate: "2026-09-09T10:00:00Z", UpdatedDate: "2026-09-09T10:00:00Z"}
	if err := st.Tasks().Upsert(ctx, []store.Task{t1, sub}); err != nil {
		t.Fatal(err)
	}
	err = st.Dependencies().ReplaceForTask(ctx, "TASK1", []store.Dependency{
		{ID: "X", PredecessorID: "TASK4", SuccessorID: "TASK1", RelationType: "FinishToStart", LagMinutes: 480},
		{ID: "Y", PredecessorID: "TASK1", SuccessorID: "GONE", RelationType: "StartToStart"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTaskShowPrintsTheRelations(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedRelations(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show", "TASK1"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	got := out.String()
	for _, want := range []string{
		"subtask of  o Reach tasks outside the followed scopes\n",
		"attachments 1\n",
		"subtasks    v Print JSON\n",
		"predecessor v Add the licence file (finish to start, lag 1 day)\n",
		"successor   a task outside the followed spaces (start to start)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("show lacks %q:\n%s", want, got)
		}
	}
}

func TestTaskShowJSONCarriesTheRelations(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	seedRelations(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show", "TASK1", "--json"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var got struct {
		AttachmentCount int `json:"attachment_count"`
		SuperTasks      []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"super_tasks"`
		Subtasks []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Status      string `json:"status"`
			StatusGroup string `json:"status_group"`
		} `json:"subtasks"`
		Predecessors []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Relation   string `json:"relation"`
			LagMinutes int    `json:"lag_minutes"`
		} `json:"predecessors"`
		Successors []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"successors"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v:\n%s", err, out.String())
	}
	if got.AttachmentCount != 1 {
		t.Errorf("attachment_count = %d", got.AttachmentCount)
	}
	if len(got.SuperTasks) != 1 || got.SuperTasks[0].ID != "TASK2" || got.SuperTasks[0].Title != "Reach tasks outside the followed scopes" {
		t.Errorf("super_tasks = %+v", got.SuperTasks)
	}
	if len(got.Subtasks) != 1 || got.Subtasks[0].ID != "TASK9" || got.Subtasks[0].Status != "Completed" || got.Subtasks[0].StatusGroup != "Completed" {
		t.Errorf("subtasks = %+v", got.Subtasks)
	}
	if len(got.Predecessors) != 1 || got.Predecessors[0].ID != "TASK4" || got.Predecessors[0].Relation != "FinishToStart" || got.Predecessors[0].LagMinutes != 480 {
		t.Errorf("predecessors = %+v", got.Predecessors)
	}
	if len(got.Successors) != 1 || got.Successors[0].ID != "GONE" || got.Successors[0].Title != "" {
		t.Errorf("successors = %+v, an uncached end keeps its id and has no title", got.Successors)
	}
}

func TestTaskShowJSONHasEmptyRelationLists(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show", "TASK3", "--json"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	for _, key := range []string{`"super_tasks": []`, `"subtasks": []`, `"predecessors": []`, `"successors": []`, `"attachment_count": 0`} {
		if !strings.Contains(out.String(), key) {
			t.Errorf("json lacks %s, a slice is never null:\n%s", key, out.String())
		}
	}
}
