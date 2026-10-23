package syncer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func outsideTask(id, title string) wrike.Task {
	return wrike.Task{ID: id, Title: title, Status: "Active", Permalink: "https://www.wrike.com/open.htm?id=4552825749",
		CreatedDate: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC), UpdatedDate: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)}
}

func stampedTask(t *testing.T, st *store.Store, id, title string) {
	t.Helper()
	err := st.Tasks().UpsertReached(context.Background(), []store.Task{{
		ID: id, Title: title, Status: "Active",
		CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z",
	}}, "2026-10-22T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
}

func TestReachFetchesAnIdWithTheCountFieldOnlyAndStampsIt(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	var gotFields []string
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		gotFields = fields
		return []wrike.Task{outsideTask("MAAAAAEPXpuV", "Outside")}, nil
	}}
	e := New(fc, st, Config{}, nil)
	task, err := e.Reach(ctx, store.TaskRef{Kind: store.RefID, Value: "MAAAAAEPXpuV"})
	if err != nil || task.ID != "MAAAAAEPXpuV" || task.Title != "Outside" {
		t.Fatalf("Reach = %+v, %v", task, err)
	}
	if log := fc.callLog(); !reflect.DeepEqual(log, []string{"TasksByIDs MAAAAAEPXpuV"}) {
		t.Errorf("calls = %v", log)
	}
	if !reflect.DeepEqual(gotFields, []string{"attachmentCount"}) {
		t.Errorf("fields = %v, want the count alone, the by id call refuses the search's fields", gotFields)
	}
	if ids, err := st.Tasks().ReachedIDs(ctx, ""); err != nil || !reflect.DeepEqual(ids, []string{"MAAAAAEPXpuV"}) {
		t.Errorf("ReachedIDs = %v, %v", ids, err)
	}
	select {
	case ev := <-e.Events():
		if ev.Kind != EventStoreChanged || !reflect.DeepEqual(ev.Entities, []EntityKind{KindTasks}) {
			t.Errorf("event = %+v, want a tasks store change", ev)
		}
	default:
		t.Error("no event after the reach")
	}
}

func TestReachResolvesANumberThroughThePermalinkFilter(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		// A subtask without a folder of its own is listed only with subTasks, a pasted link is often one.
		if p.Permalink != "https://www.wrike.com/open.htm?id=4552825749" || p.FolderID != "" || p.SpaceID != "" || !p.SubTasks || !reflect.DeepEqual(p.Fields, pullFields) {
			t.Errorf("params = %+v, want the www link, the plain endpoint, subtasks and the pull fields", p)
		}
		return wrike.TasksPage{Tasks: []wrike.Task{outsideTask("MAAAAAEPXpuV", "Outside")}}, nil
	}}
	e := New(fc, st, Config{}, nil)
	task, err := e.Reach(ctx, store.TaskRef{Kind: store.RefNumber, Value: "4552825749"})
	if err != nil || task.ID != "MAAAAAEPXpuV" {
		t.Errorf("Reach = %+v, %v", task, err)
	}
	if n := strings.Count(strings.Join(fc.callLog(), "\n"), "Tasks "); n != 1 {
		t.Errorf("calls = %v, want one permalink search", fc.callLog())
	}
}

func TestReachReportsNoSuchTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	answers := map[string]error{
		"MAAAAAEPXXXX": &wrike.APIError{StatusCode: http.StatusNotFound, Code: "resource_not_found"},
		"MAAAAAEPYYYY": &wrike.APIError{StatusCode: http.StatusBadRequest, Code: "invalid_request"},
		"MAAAAAEPZZZZ": nil,
	}
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		if ids[0] == "MAAAAAEPWWWW" {
			deleted := outsideTask("MAAAAAEPWWWW", "In the bin")
			deleted.Scope = "RbTask"
			return []wrike.Task{deleted}, nil
		}
		return nil, answers[ids[0]]
	}}
	e := New(fc, st, Config{}, nil)
	for _, id := range []string{"MAAAAAEPXXXX", "MAAAAAEPYYYY", "MAAAAAEPZZZZ", "MAAAAAEPWWWW"} {
		if _, err := e.Reach(ctx, store.TaskRef{Kind: store.RefID, Value: id}); !errors.Is(err, ErrNoSuchTask) {
			t.Errorf("Reach(%s) = %v, want ErrNoSuchTask", id, err)
		}
	}
	if _, err := st.Tasks().Get(ctx, "MAAAAAEPWWWW"); err == nil {
		t.Error("a task in the Recycle Bin was cached")
	}
	// A 400 about the request itself, a field the call refuses for example, is not about the task and must travel up.
	fc.tasksByIDs = func(ids, fields []string) ([]wrike.Task, error) {
		return nil, &wrike.APIError{StatusCode: http.StatusBadRequest, Code: "invalid_parameter"}
	}
	if _, err := e.Reach(ctx, store.TaskRef{Kind: store.RefID, Value: "MAAAAAEPXpuV"}); err == nil || errors.Is(err, ErrNoSuchTask) {
		t.Errorf("a 400 invalid_parameter gave %v, want another error", err)
	}
	fc.tasksByIDs = func(ids, fields []string) ([]wrike.Task, error) {
		return nil, &wrike.APIError{StatusCode: http.StatusServiceUnavailable}
	}
	if _, err := e.Reach(ctx, store.TaskRef{Kind: store.RefID, Value: "MAAAAAEPXpuV"}); err == nil || errors.Is(err, ErrNoSuchTask) {
		t.Errorf("a 503 gave %v, want another error", err)
	}
}

func TestReachReferencedFetchesOneHopAndSkipsTheUnanswered(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Timelogs().Upsert(ctx, []store.Timelog{{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: "2026-10-20", Hours: 1}}); err != nil {
		t.Fatal(err)
	}
	seedTaskWithDependency(t, st, "T1", "D1")
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []store.Dependency{{ID: "D1", PredecessorID: "T1", SuccessorID: "T8"}}); err != nil {
		t.Fatal(err)
	}
	stampedTask(t, st, "T2", "reached")
	if err := st.Dependencies().ReplaceForTask(ctx, "T2", []store.Dependency{{ID: "D2", PredecessorID: "T6", SuccessorID: "T2"}}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		return []wrike.Task{outsideTask("T8", "the other end")}, nil
	}}
	skip := map[string]bool{}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	changed, err := reachReferenced(ctx, fc, st, "U1", "2026-08-24", skip, slog.Default(), now)
	if err != nil || !changed {
		t.Fatalf("reachReferenced = %v, %v, want a change", changed, err)
	}
	if ids, err := st.Tasks().ReachedIDs(ctx, ""); err != nil || !reflect.DeepEqual(ids, []string{"T2", "T8"}) {
		t.Errorf("ReachedIDs = %v, %v, want T2 and T8, never T6 behind the reached T2", ids, err)
	}
	if changed, err := reachReferenced(ctx, fc, st, "U1", "2026-08-24", skip, slog.Default(), now); err != nil || changed {
		t.Errorf("second pass = %v, %v, want nothing, T9 is skipped", changed, err)
	}
	if log := fc.callLog(); !reflect.DeepEqual(log, []string{"TasksByIDs T8,T9"}) {
		t.Errorf("calls = %v, want one batch", log)
	}
	delete(skip, "T9")
	if _, err := reachReferenced(ctx, fc, st, "U1", "2026-08-24", skip, slog.Default(), now); err != nil {
		t.Fatal(err)
	}
	if log := fc.callLog(); len(log) != 2 || log[1] != "TasksByIDs T9" {
		t.Errorf("calls = %v, want T9 asked again once forgotten", log)
	}
}

func TestCycleRereadsStampedTasksAndDropsTheMissing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	stampedTask(t, st, "T5", "old title")
	stampedTask(t, st, "T6", "gone on Wrike")
	stampedTask(t, st, "T7", "in the bin")
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		deleted := outsideTask("T7", "in the bin")
		deleted.Scope = "RbTask"
		return []wrike.Task{outsideTask("T5", "new title"), deleted}, nil
	}}
	e := New(fc, st, Config{}, nil)
	if _, err := e.Once(ctx, true); err != nil {
		t.Fatal(err)
	}
	if task, err := st.Tasks().Get(ctx, "T5"); err != nil || task.Title != "new title" {
		t.Errorf("T5 = %+v, %v, want the re-read title", task, err)
	}
	if _, err := st.Tasks().Get(ctx, "T6"); err == nil {
		t.Error("T6 survived a re-read that did not answer it")
	}
	if _, err := st.Tasks().Get(ctx, "T7"); err == nil {
		t.Error("T7 survived a re-read that answered it from the Recycle Bin")
	}
}

func TestPollRereadsOnlyTheOpenedStampedTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	stampedTask(t, st, "T5", "opened")
	stampedTask(t, st, "T6", "never opened")
	if err := st.Tasks().MarkOpened(ctx, "T5", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		return []wrike.Task{outsideTask("T5", "opened")}, nil
	}}
	e := New(fc, st, Config{}, nil)
	if _, err := e.Once(ctx, false); err != nil {
		t.Fatal(err)
	}
	var byIDs []string
	for _, c := range fc.callLog() {
		if strings.HasPrefix(c, "TasksByIDs ") {
			byIDs = append(byIDs, c)
		}
	}
	if !reflect.DeepEqual(byIDs, []string{"TasksByIDs T5"}) {
		t.Errorf("by id calls = %v, want T5 alone", byIDs)
	}
	if _, err := st.Tasks().Get(ctx, "T6"); err != nil {
		t.Errorf("T6 was dropped by a poll that never asked for it: %v", err)
	}
}

// A 404 is the ordinary answer for one id the user cannot see, it fills the skip set and earns no warning.
// Any other rejection for good is logged, and the cycle goes on either way.
func TestReachReferencedTakesA404AsNoAnswerWithoutAWarning(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Timelogs().Upsert(ctx, []store.Timelog{{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: "2026-10-20", Hours: 1}}); err != nil {
		t.Fatal(err)
	}
	answer := &wrike.APIError{StatusCode: http.StatusNotFound, Code: "resource_not_found"}
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) { return nil, answer }}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	skip := map[string]bool{}
	if changed, err := reachReferenced(ctx, fc, st, "U1", "2026-08-24", skip, log, time.Now()); err != nil || changed {
		t.Fatalf("reachReferenced = %v, %v", changed, err)
	}
	if !skip["T9"] || strings.Contains(buf.String(), "reach rejected") {
		t.Errorf("after a 404 skip = %v, log = %q, want T9 skipped and no warning", skip, buf.String())
	}
	answer = &wrike.APIError{StatusCode: http.StatusForbidden, Code: "access_forbidden"}
	delete(skip, "T9")
	if _, err := reachReferenced(ctx, fc, st, "U1", "2026-08-24", skip, log, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !skip["T9"] || !strings.Contains(buf.String(), "reach rejected") {
		t.Errorf("after a 403 skip = %v, log = %q, want T9 skipped and a warning", skip, buf.String())
	}
}

// The stamped set does not change by itself, so a re-read Wrike rejects for good must not fail every cycle from then on.
func TestCycleSurvivesAPermanentErrorOnTheReread(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	stampedTask(t, st, "T5", "stays")
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		return nil, &wrike.APIError{StatusCode: http.StatusBadRequest, Code: "invalid_request"}
	}}
	var buf bytes.Buffer
	e := New(fc, st, Config{}, slog.New(slog.NewTextHandler(&buf, nil)))
	state, err := e.Once(ctx, true)
	if err != nil || state != StateIdle {
		t.Fatalf("Once = %q, %v, want idle and no error", state, err)
	}
	if _, err := st.Tasks().Get(ctx, "T5"); err != nil {
		t.Errorf("T5 was dropped by a rejected re-read: %v", err)
	}
	if !strings.Contains(buf.String(), "reread rejected") {
		t.Errorf("log = %q, want the rejection noted", buf.String())
	}
}

// A read by id answers a deleted task with the Recycle Bin scope, that is no answer for the reach.
func TestReachReferencedTreatsARecycleBinTaskAsUnanswered(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Timelogs().Upsert(ctx, []store.Timelog{{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: "2026-10-20", Hours: 1}}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
		deleted := outsideTask("T9", "deleted")
		deleted.Scope = "RbTask"
		return []wrike.Task{deleted}, nil
	}}
	skip := map[string]bool{}
	if changed, err := reachReferenced(ctx, fc, st, "U1", "2026-08-24", skip, slog.Default(), time.Now()); err != nil || changed {
		t.Fatalf("reachReferenced = %v, %v, want nothing written", changed, err)
	}
	if _, err := st.Tasks().Get(ctx, "T9"); err == nil {
		t.Error("the deleted task was cached")
	}
	if !skip["T9"] {
		t.Error("the deleted task is not skipped")
	}
}

// The skip set is applied inside the window the query reads, so the ids behind a batch of skipped ones are still reached.
func TestReachReferencedMakesProgressPastASkippedBatch(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	var logs []store.Timelog
	skip := map[string]bool{}
	for i := 0; i <= reachBatch; i++ {
		id := fmt.Sprintf("T%04d", i)
		logs = append(logs, store.Timelog{ID: "L" + id, TaskID: id, UserID: "U1", TrackedDate: "2026-10-20", Hours: 1})
		if i < reachBatch {
			skip[id] = true
		}
	}
	if err := st.Timelogs().Upsert(ctx, logs); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	if _, err := reachReferenced(ctx, fc, st, "U1", "2026-08-24", skip, slog.Default(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if log := fc.callLog(); !reflect.DeepEqual(log, []string{fmt.Sprintf("TasksByIDs T%04d", reachBatch)}) {
		t.Errorf("calls = %v, want the one id behind the skipped batch", log)
	}
}

// A manual cycle lists the stamped tasks before it reaches for the referenced ones, so what it just fetched is not read a second time.
func TestManualCycleReadsAJustReachedTaskOnce(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	today := time.Now().UTC().Format("2006-01-02")
	fc := &fakeClient{
		timelogs: func(p wrike.TimelogParams) (wrike.TimelogsPage, error) {
			return wrike.TimelogsPage{Timelogs: []wrike.Timelog{{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: today, Hours: 1}}}, nil
		},
		tasksByIDs: func(ids, fields []string) ([]wrike.Task, error) {
			return []wrike.Task{outsideTask("T9", "behind a time entry")}, nil
		},
	}
	e := New(fc, st, Config{}, nil)
	if _, err := e.Once(ctx, true); err != nil {
		t.Fatal(err)
	}
	var byIDs []string
	for _, c := range fc.callLog() {
		if strings.HasPrefix(c, "TasksByIDs ") {
			byIDs = append(byIDs, c)
		}
	}
	if !reflect.DeepEqual(byIDs, []string{"TasksByIDs T9"}) {
		t.Errorf("by id calls = %v, want T9 once", byIDs)
	}
}
