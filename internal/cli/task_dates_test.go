package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

// The test clock is Saturday 2026-10-03, TASK1 runs from 2026-10-01 to 2026-10-03 and TASK2 has no dates.

func datesOf(t *testing.T, env Env, id string) store.TaskDates {
	t.Helper()
	task, err := env.Store.Tasks().Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Dates == nil {
		return store.TaskDates{}
	}
	return *task.Dates
}

func TestTaskDatesSendsTheNewDueAndKeepsTheStart(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, path, form, _ := recordWrite(t, env, answer("TASK1", "Headless commands for scripts", "High", []string{"U1"}, []string{"PROJ1"}))
	if code := Run(context.Background(), env, []string{"task", "dates", "TASK1", "--due", "+5d"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *path != "PUT /tasks/TASK1" {
		t.Errorf("request = %q", *path)
	}
	dates := form.Get("dates")
	for _, want := range []string{`"type":"Planned"`, `"start":"2026-10-01"`, `"due":"2026-10-08"`} {
		if !strings.Contains(dates, want) {
			t.Errorf("dates = %q, want %s in it", dates, want)
		}
	}
	if !strings.Contains(out.String(), "was: Headless commands for scripts  [2026-10-01 -> 2026-10-03]") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestTaskDatesReadsTheWordsOfTheDatesBox(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want store.TaskDates
		line string
	}{
		{"one day on a task without dates", []string{"TASK2", "--due", "fri"},
			store.TaskDates{Type: "Planned", Start: "2026-10-09", Due: "2026-10-09"}, "[2026-10-09 -> 2026-10-09]"},
		{"both", []string{"TASK2", "--start", "tomorrow", "--due", "12 oct"},
			store.TaskDates{Type: "Planned", Start: "2026-10-04", Due: "2026-10-12"}, "[2026-10-04 -> 2026-10-12]"},
		{"a start alone keeps the due date", []string{"TASK1", "--start", "2026-10-02"},
			store.TaskDates{Type: "Planned", Start: "2026-10-02", Due: "2026-10-03"}, "[2026-10-02 -> 2026-10-03]"},
		{"clear", []string{"TASK1", "--clear"}, store.TaskDates{Type: "Backlog"}, "[no dates]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, out, errOut := testEnv(t)
			seedBoard(t, env.Store)
			env = noNetwork(t, env)
			if code := Run(context.Background(), env, append([]string{"task", "dates"}, tc.args...)); code != exitQueued {
				t.Fatalf("code = %d, stderr %q", code, errOut.String())
			}
			got := datesOf(t, env, tc.args[0])
			got.Duration = 0
			if got != tc.want {
				t.Errorf("dates in the cache = %+v, want %+v", got, tc.want)
			}
			if !strings.Contains(out.String(), "  "+tc.line+"\n") {
				t.Errorf("out = %q, want %s", out.String(), tc.line)
			}
		})
	}
}

func TestTaskDatesToTheDaysTheTaskHasSendsNothing(t *testing.T) {
	for _, args := range [][]string{
		{"TASK1", "--due", "today"},
		{"TASK1", "--start", "2026-10-01", "--due", "2026-10-03"},
		{"TASK2", "--clear"},
	} {
		env, out, errOut := testEnv(t)
		seedBoard(t, env.Store)
		env, _, _, requests := recordWrite(t, env, "{}")
		if code := Run(context.Background(), env, append([]string{"task", "dates"}, args...)); code != exitOK {
			t.Fatalf("%v: code = %d, stderr %q", args, code, errOut.String())
		}
		if pending, _, _ := env.Store.Outbox().Counts(context.Background()); *requests != 0 || pending != 0 {
			t.Errorf("%v: %d requests, %d queued, want none", args, *requests, pending)
		}
		if !strings.HasPrefix(out.String(), "unchanged: ") || !strings.Contains(errOut.String(), "dates unchanged") {
			t.Errorf("%v: stdout %q, stderr %q", args, out.String(), errOut.String())
		}
	}
}

func TestTaskDatesRefusesADueDateBeforeTheStartTheTaskHas(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = noNetwork(t, env)
	if code := Run(context.Background(), env, []string{"task", "dates", "TASK1", "--due", "2026-09-30"}); code != exitError {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "due is before the start the task has, 2026-10-01, give --start as well") {
		t.Errorf("stderr = %q", errOut.String())
	}
	if code := Run(context.Background(), env, []string{"task", "dates", "TASK1", "--start", "2026-10-05"}); code != exitError {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "start is after the due date the task has, 2026-10-03, give --due as well") {
		t.Errorf("stderr = %q", errOut.String())
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("%d rows queued, want none", pending)
	}
}

func TestTaskDatesUsage(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"TASK1"}, "task dates needs --start, --due or --clear"},
		{[]string{"--due", "fri"}, "task dates takes one task"},
		{[]string{"TASK1", "--due", ""}, "the due date is empty"},
		{[]string{"TASK1", "--start", " "}, "the start is empty"},
		{[]string{"TASK1", "--clear", "--due", "fri"}, "--clear goes alone"},
		{[]string{"TASK1", "--due", "someday"}, "due: could not read the date"},
		{[]string{"TASK1", "--start", "mon", "--due", "today"}, "due is before start"},
		// A word that cannot be read is refused before the task is looked for.
		{[]string{"no such task", "--start", "+3"}, "start: expected +Nd or -Nd"},
	} {
		env, _, errOut := testEnv(t)
		seedBoard(t, env.Store)
		env = noNetwork(t, env)
		if code := Run(context.Background(), env, append([]string{"task", "dates"}, tc.args...)); code != exitUsage {
			t.Errorf("%v: code = %d, want %d, stderr %q", tc.args, code, exitUsage, errOut.String())
		}
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%v: stderr = %q, want %q", tc.args, errOut.String(), tc.want)
		}
	}
}

// A milestone has a due date and no start. Moving its day must not make a one day task of it, a start asks for a range.
func TestTaskDatesKeepsAMilestoneAndAStartMakesItPlanned(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want store.TaskDates
	}{
		{"the due date moves", []string{"--due", "2026-10-20"}, exitQueued, store.TaskDates{Type: "Milestone", Due: "2026-10-20"}},
		{"a start", []string{"--start", "2026-10-05"}, exitQueued, store.TaskDates{Type: "Planned", Start: "2026-10-05", Due: "2026-10-09"}},
		// The cache holds the day with Wrike's working hour, the same day typed bare is no change.
		{"the day it has", []string{"--due", "2026-10-09"}, exitOK, store.TaskDates{Type: "Milestone", Due: "2026-10-09T17:00:00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _, errOut := testEnv(t)
			seedBoard(t, env.Store)
			if err := env.Store.Tasks().Upsert(context.Background(), []store.Task{{ID: "MILE1", Title: "Release day", Status: "Active", CustomStatusID: "ST_NEW",
				ParentIDs: []string{"PROJ1"}, Dates: &store.TaskDates{Type: "Milestone", Due: "2026-10-09T17:00:00"}}}); err != nil {
				t.Fatal(err)
			}
			env = noNetwork(t, env)
			if code := Run(context.Background(), env, append([]string{"task", "dates", "MILE1"}, tc.args...)); code != tc.code {
				t.Fatalf("code = %d, want %d, stderr %q", code, tc.code, errOut.String())
			}
			if got := datesOf(t, env, "MILE1"); got != tc.want {
				t.Errorf("dates in the cache = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTaskDatesJSONCarriesTheDatesAndSent(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = noNetwork(t, env)
	if code := Run(context.Background(), env, []string{"task", "dates", "TASK2", "--due", "fri", "--json"}); code != exitQueued {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	var got struct {
		ID    string
		Sent  bool
		Dates struct{ Type, Start, Due string }
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got.ID != "TASK2" || got.Sent || got.Dates.Type != "Planned" || got.Dates.Start != "2026-10-09" || got.Dates.Due != "2026-10-09" {
		t.Errorf("json = %+v, want the queued task with its one day", got)
	}
}
