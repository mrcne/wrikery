package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestSummarize(t *testing.T) {
	cases := []struct {
		row  store.OutboxRow
		want string
	}{
		{store.OutboxRow{Kind: store.KindCommentCreate, Payload: []byte(`{"text":"Queued while offline, quite a long comment indeed"}`)}, "comment: Queued while offline, quite a long comm..."},
		{store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"customStatusId":"X"}`)}, "status change"},
		{store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"addResponsibles":["A"]}`)}, "assignee change"},
		{store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"dates":{"type":"Planned"}}`)}, "dates change"},
		{store.OutboxRow{Kind: store.KindTimelogCreate, Payload: []byte(`{"hours":1,"trackedDate":"2026-08-26"}`)}, "time entry 1.0 h on 2026-08-26"},
		{store.OutboxRow{Kind: store.KindTimelogUpdate, Payload: []byte(`{"hours":2,"trackedDate":"2026-08-27"}`)}, "time entry 2.0 h on 2026-08-27"},
		{store.OutboxRow{Kind: store.KindTimelogUpdate, Payload: []byte(`{"comment":"fixed the totals"}`)}, "time entry change"},
		{store.OutboxRow{Kind: store.KindTimelogDelete}, "delete time entry"},
	}
	for _, c := range cases {
		if got := summarize(c.row); got != c.want {
			t.Errorf("summarize(%s) = %q, want %q", c.row.Kind, got, c.want)
		}
	}
}

// TestLoadIssuesFallbackTitles covers the two rows loadIssues cannot fully resolve: a task
// update whose task is gone from the cache, and a timelog edit whose timelog is gone too, so
// its task can no longer be looked up. Both fall back to a placeholder title built from the id.
func TestLoadIssuesFallbackTitles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "issues-fallback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	taskID, err := st.Outbox().EnqueueTaskUpdate(ctx, "GHOSTTASK1", store.TaskUpdatePayload{CustomStatusID: "X"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, taskID, "wrike: 404 Task not found"); err != nil {
		t.Fatal(err)
	}
	logID, err := st.Outbox().EnqueueTimelogDelete(ctx, "GHOSTLOG1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, logID, "wrike: 404 Timelog not found"); err != nil {
		t.Fatal(err)
	}

	m := New(Options{Store: st, Now: func() time.Time { return time.Now() }})
	msg := m.loadIssues()()
	loaded, ok := msg.(issuesLoadedMsg)
	if !ok {
		t.Fatalf("loadIssues() = %#v, want issuesLoadedMsg", msg)
	}
	if len(loaded.rows) != 2 {
		t.Fatalf("rows = %+v, want 2", loaded.rows)
	}
	if loaded.rows[0].title != "(task GHOSTTASK1)" {
		t.Errorf("task row title = %q, want (task GHOSTTASK1)", loaded.rows[0].title)
	}
	if loaded.rows[1].title != "(time entry GHOSTLOG1)" {
		t.Errorf("timelog row title = %q, want (time entry GHOSTLOG1)", loaded.rows[1].title)
	}
}

// TestIssuesViewKeepsTheCursorRowInBudget checks that the cursor's extra error line is counted
// against the height budget. Without that, scrolling the cursor to the last row a plain row
// count lets in pushes the output one line past height, and the box drawn around it trims that
// extra line, which happens to be the very error text the cursor is there to show.
func TestIssuesViewKeepsTheCursorRowInBudget(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	m := issuesModel{height: 2}
	m.set([]issueRow{
		{row: store.OutboxRow{LastError: "row0 error"}, title: "row0", summary: "s0"},
		{row: store.OutboxRow{LastError: "row1 error"}, title: "row1", summary: "s1"},
		{row: store.OutboxRow{LastError: "row2 error"}, title: "row2", summary: "s2"},
	})
	m.cursor = 2
	m.scroll() // the same call Down makes, scrolling the cursor's row into view

	out := m.View(th, time.Now(), 60, 2)
	lines := strings.Split(out, "\n")
	if len(lines) > 2 {
		t.Fatalf("View with height 2 produced %d lines, want at most 2:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "row2 error") {
		t.Errorf("the cursor's error line should fit the height budget:\n%s", out)
	}
}

// TestEnqueueIssueOpMapsNotFoundToAPlainToast covers the case where the engine already took the
// row inflight, or somebody else cleared it, between the issues screen listing it and the key
// press acting on it. store.ErrNotFound then must not surface as a raw "store: not found" toast.
func TestEnqueueIssueOpMapsNotFoundToAPlainToast(t *testing.T) {
	m := New(Options{Now: func() time.Time { return time.Now() }})
	cmd := m.enqueueIssueOp(func(ctx context.Context) error { return store.ErrNotFound }, "Retrying")
	got, ok := cmd().(writeQueuedMsg)
	if !ok {
		t.Fatalf("enqueueIssueOp on ErrNotFound = %#v, want writeQueuedMsg", cmd())
	}
	if got.toast != "already being sent, list refreshed" {
		t.Errorf("toast = %q, want a plain sentence rather than the raw store error", got.toast)
	}
}
