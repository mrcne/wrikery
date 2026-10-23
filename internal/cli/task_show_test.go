package cli

import (
	"context"
	"encoding/json"
	"net/http"
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
	// The markdown the editor opens on, so an edit from a script starts from the same text a person sees.
	if got["description_markdown"] != "Three flags, **no** subcommands.\n" {
		t.Errorf("description_markdown = %q", got["description_markdown"])
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
		"failed":  {true, "queued      a write was rejected, see the changes screen\n"},
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

const outsideTaskJSON = `{"kind":"tasks","data":[{"id":"MAAAAAEPXpuV","title":"Outside the scopes","status":"Active","importance":"Normal",
"permalink":"https://www.wrike.com/open.htm?id=9900000","createdDate":"2026-10-01T10:00:00Z","updatedDate":"2026-10-01T10:00:00Z"}]}`

// outsideWrike answers the fetch of one task by id and nothing else, the way the live account does.
func outsideWrike(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/MAAAAAEPXpuV" || r.URL.Query().Get("fields") != `["attachmentCount"]` {
			t.Errorf("request = %s %s, want the task by id with the count field alone", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(outsideTaskJSON))
	})
}

func TestTaskShowFetchesAnUncachedTask(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, outsideWrike(t))
	if code := Run(context.Background(), env, []string{"task", "show", "MAAAAAEPXpuV"}); code != exitOK {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Outside the scopes") {
		t.Errorf("out:\n%s", out.String())
	}
	if ids, err := env.Store.Tasks().ReachedIDs(context.Background(), ""); err != nil || len(ids) != 1 || ids[0] != "MAAAAAEPXpuV" {
		t.Errorf("ReachedIDs = %v, %v, want the fetched task stamped", ids, err)
	}
}

func TestTaskShowOfAnUnknownIdSaysSo(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorDescription":"Task not found or access denied","error":"resource_not_found"}`))
	}))
	if code := Run(context.Background(), env, []string{"task", "show", "MAAAAAEPXXXX"}); code != exitError {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), `no task "MAAAAAEPXXXX" on Wrike, or no access`) {
		t.Errorf("stderr:\n%s", errOut.String())
	}
}

func TestTaskShowOfAnUncachedIdNeedsTheSignIn(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"task", "show", "MAAAAAEPXpuV"}); code != exitError {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), "not signed in") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
}

func TestTaskShowOfAnUncachedIdWithARejectedTokenSaysSignIn(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorDescription":"Token is invalid","error":"not_authorized"}`))
	}))
	if code := Run(context.Background(), env, []string{"task", "show", "MAAAAAEPXpuV"}); code != exitError {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), "sign in rejected, run wrikery to sign in again") || strings.Contains(errOut.String(), "/tasks/") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
}

func TestTaskShowOfAnUncachedIdSaysWhenWrikeDoesNotAnswer(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	if code := Run(context.Background(), env, []string{"task", "show", "MAAAAAEPXpuV"}); code != exitError {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), "Wrike did not answer in time") {
		t.Errorf("stderr:\n%s", errOut.String())
	}
}

// A task titled with a number is still found by more of its title, the message keeps saying so.
func TestTaskShowOfAnUnknownNumberKeepsTheTitleHint(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorDescription":"Task not found or access denied","error":"resource_not_found"}`))
	}))
	if code := Run(context.Background(), env, []string{"task", "show", "2026"}); code != exitError {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), `no task "2026" on Wrike, or no access, a title that is a number needs more of it`) {
		t.Errorf("stderr:\n%s", errOut.String())
	}
}
