package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

var changesRef = refData{
	contacts: map[string]store.Contact{"U1": {FirstName: "Ada", LastName: "Nowak"}, "U2": {FirstName: "Bartek", LastName: "Lis"}},
	statuses: map[string]store.CustomStatus{"S1": {Name: "In Progress"}, "S2": {Name: "Done"}},
}

func TestDescribeWritesOldAndNew(t *testing.T) {
	folders := map[string]string{"F1": "Backlog", "F2": "v0.5"}
	cases := []struct {
		name    string
		row     store.OutboxRow
		summary string
		detail  string
	}{
		{"status", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"customStatusId":"S2"}`), Before: []byte(`{"customStatusId":"S1"}`)},
			"status  In Progress -> Done", "status  In Progress -> Done"},
		{"status unknown id", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"customStatusId":"X"}`), Before: []byte(`{"customStatusId":"S1"}`)},
			"status  In Progress -> X", "status  In Progress -> X"},
		{"status ghost", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"customStatusId":"S2"}`)},
			"status  -> Done", "status  -> Done"},
		{"assignee", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"addResponsibles":["U1"],"removeResponsibles":["U2"]}`)},
			"assignee  + Ada Nowak  - Bartek Lis", "assignee  + Ada Nowak  - Bartek Lis"},
		{"folder", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"addParents":["F2"],"removeParents":["F1","F9"]}`)},
			"folder  + v0.5  - Backlog  - F9", "folder  + v0.5  - Backlog  - F9"},
		{"title", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"title":"A much longer title than fits here"}`), Before: []byte(`{"title":"Short"}`)},
			"title  Short -> A much longer title t...", "title  Short -> A much longer title than fits here"},
		{"description", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"description":"<p>New <b>first</b> line</p><p>second</p>"}`), Before: []byte(`{"description":"<p>Old line</p>"}`)},
			"description changed", "description  Old line -> New first line second"},
		{"importance", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"importance":"High"}`), Before: []byte(`{"importance":"Normal"}`)},
			"importance  Normal -> High", "importance  Normal -> High"},
		// A command edits several fields in one write, every one of them is on the screen.
		{"several fields", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"title":"New","importance":"Low","description":"<p>Body</p>"}`), Before: []byte(`{"title":"Old","importance":"High","description":"<p>Was</p>"}`)},
			"title  Old -> New, importance  High -> Low, description changed", "title  Old -> New, importance  High -> Low, description  Was -> Body"},
		{"dates first", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"dates":{"type":"Planned","due":"2026-10-15"}}`), Before: []byte(`{"dates":{"type":"Backlog"}}`)},
			"dates  Backlog -> due 15 Oct", "dates  Backlog -> due 15 Oct"},
		{"dates range", store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"dates":{"type":"Planned","start":"2026-10-10","due":"2026-10-15"}}`), Before: []byte(`{"dates":{"type":"Milestone","due":"2026-10-12"}}`)},
			"dates  milestone 12 Oct -> 10 Oct to 15 Oct", "dates  milestone 12 Oct -> 10 Oct to 15 Oct"},
		{"comment", store.OutboxRow{Kind: store.KindCommentCreate, Payload: []byte(`{"text":"Queued while offline, quite a long comment indeed\nwith a second line"}`)},
			"comment: Queued while offline, quite a long comm...", "Queued while offline, quite a long comment indeed with a second line"},
		{"new task", store.OutboxRow{Kind: store.KindTaskCreate, Payload: []byte(`{"title":"Ship it"}`)}, "new task: Ship it", "Ship it"},
		{"time entry", store.OutboxRow{Kind: store.KindTimelogCreate, Payload: []byte(`{"hours":1,"trackedDate":"2026-08-26","comment":"pairing"}`)},
			"time entry 1h on 26 Aug", "time entry 1h on 26 Aug, pairing"},
		{"time entry hours", store.OutboxRow{Kind: store.KindTimelogUpdate, Payload: []byte(`{"hours":2,"trackedDate":"2026-08-27"}`), Before: []byte(`{"hours":1,"trackedDate":"2026-08-27"}`)},
			"time entry 1h -> 2h", "time entry 1h on 27 Aug -> 2h on 27 Aug"},
		{"time entry date", store.OutboxRow{Kind: store.KindTimelogUpdate, Payload: []byte(`{"hours":1,"trackedDate":"2026-08-28"}`), Before: []byte(`{"hours":1,"trackedDate":"2026-08-27"}`)},
			"time entry 27 Aug -> 28 Aug", "time entry 1h on 27 Aug -> 1h on 28 Aug"},
		{"time entry note", store.OutboxRow{Kind: store.KindTimelogUpdate, Payload: []byte(`{"hours":1,"trackedDate":"2026-08-27","comment":"fixed the totals"}`), Before: []byte(`{"hours":1,"trackedDate":"2026-08-27"}`)},
			"time entry note changed", "time entry 1h on 27 Aug -> 1h on 27 Aug, fixed the totals"},
		{"time entry ghost", store.OutboxRow{Kind: store.KindTimelogUpdate, Payload: []byte(`{"hours":2,"trackedDate":"2026-08-27"}`)},
			"time entry -> 2h", "time entry -> 2h on 27 Aug"},
		{"delete", store.OutboxRow{Kind: store.KindTimelogDelete, Before: []byte(`{"hours":1,"trackedDate":"2026-08-26"}`)},
			"deleted time entry 1h on 26 Aug", "deleted time entry 1h on 26 Aug"},
		{"delete ghost", store.OutboxRow{Kind: store.KindTimelogDelete}, "deleted time entry", ""},
	}
	for _, c := range cases {
		summary, detail := describe(c.row, changesRef, folders)
		if summary != c.summary || detail != c.detail {
			t.Errorf("%s: describe = %q, %q\n want %q, %q", c.name, summary, detail, c.summary, c.detail)
		}
	}
}

func TestStateText(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		row  store.OutboxRow
		want string
	}{
		{store.OutboxRow{State: store.StatePending, CreatedAt: "2026-10-15T11:58:00Z"}, "queued"},
		{store.OutboxRow{State: store.StateInflight, CreatedAt: "2026-10-15T11:58:00Z"}, "sending"},
		{store.OutboxRow{State: store.StatePending, LastError: "503", CreatedAt: "2026-10-15T11:58:00Z"}, "retrying 2 min ago"},
		{store.OutboxRow{State: store.StateFailed, LastError: "404", CreatedAt: "2026-10-14T11:58:00Z"}, "failed yesterday"},
		{store.OutboxRow{State: store.StateSent, DoneAt: "2026-10-15T10:00:00Z"}, "2 h ago"},
		{store.OutboxRow{State: store.StateDiscarded, DoneAt: "2026-10-01T10:00:00Z"}, "discarded 1 Oct"},
	}
	for _, c := range cases {
		if got := stateText(c.row, now, false); got != c.want {
			t.Errorf("stateText(%s) = %q, want %q", c.row.State, got, c.want)
		}
	}
}

// The second line under the cursor is the error on a row the engine still retries, and the values in full on every other row.
func TestChangesViewShowsTheErrorOrTheValuesUnderTheCursor(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	m := changesModel{height: 6}
	m.set([]changeRow{
		{row: store.OutboxRow{State: store.StateFailed, LastError: "wrike: 404 Task not found", CreatedAt: "2026-10-15T11:00:00Z"}, title: "Failed one", summary: "status  -> Done", detail: "status  -> Done"},
		{row: store.OutboxRow{State: store.StateSent, DoneAt: "2026-10-15T10:00:00Z", Source: store.SourceCLI}, title: "Sent one", summary: "title  Short -> A much longer title t...", detail: "title  Short -> A much longer title than fits here"},
	})
	out := m.View(th, now, 100, 6, false)
	if !strings.Contains(out, "404 Task not found") || !strings.Contains(out, "Sent one") || !strings.Contains(out, "2 h ago  cli") {
		t.Errorf("view with the cursor on the failed row:\n%s", out)
	}
	m.move(1)
	out = m.View(th, now, 100, 6, false)
	if !strings.Contains(out, "A much longer title than fits here") || strings.Contains(out, "404 Task not found") {
		t.Errorf("view with the cursor on the sent row:\n%s", out)
	}
}

// Retry and discard mean nothing on a row that went out or was dropped, the keys only say so.
func TestRetryAndDiscardOnAHistoryRowOnlyWriteTheStatusLine(t *testing.T) {
	m := changesModel{keys: defaultKeyMap(), height: 6}
	m.set([]changeRow{{row: store.OutboxRow{ID: 7, State: store.StateSent}}, {row: store.OutboxRow{ID: 8, State: store.StateDiscarded}}})
	for _, c := range []struct {
		key  string
		want string
	}{{"r", "already sent, nothing to retry"}, {"x", "already sent, nothing to discard"}} {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)})
		msg, ok := cmd().(toastMsg)
		if !ok || msg.text != c.want {
			t.Errorf("%s on a sent row = %#v, want the toast %q", c.key, cmd(), c.want)
		}
	}
	m.move(1)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if msg, ok := cmd().(toastMsg); !ok || msg.text != "already discarded, nothing to retry" {
		t.Errorf("r on a discarded row = %#v", cmd())
	}
}

func TestChangesKeysMoveByPageAndToTheEnds(t *testing.T) {
	m := changesModel{keys: defaultKeyMap(), height: 5}
	rows := make([]changeRow, 20)
	m.set(rows)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.cursor != 19 || m.offset != 16 {
		t.Errorf("after G: cursor %d offset %d, want 19 and 16", m.cursor, m.offset)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.cursor != 17 {
		t.Errorf("after ctrl+u: cursor %d, want 17, half of the four rows on screen", m.cursor)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("after g: cursor %d offset %d, want 0 and 0", m.cursor, m.offset)
	}
}

// TestLoadChangesFallbackTitles covers the rows loadChanges cannot resolve through the cache: a task update whose task is gone,
// a time entry delete whose entry was never cached, so no task is known at all, and a task that left the cache after the enqueue.
func TestLoadChangesFallbackTitles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "changes-fallback.db"))
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
	// A task that was in the cache at enqueue and left it since keeps the title the row took.
	if err := st.Tasks().Upsert(ctx, []store.Task{{ID: "WAS1", Title: "Was here", Status: "Active"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "WAS1", "U1", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Delete(ctx, "WAS1"); err != nil {
		t.Fatal(err)
	}

	m := New(Options{Store: st, Now: func() time.Time { return time.Now() }})
	msg := m.loadChanges()()
	loaded, ok := msg.(changesLoadedMsg)
	if !ok {
		t.Fatalf("loadChanges() = %#v, want changesLoadedMsg", msg)
	}
	if len(loaded.rows) != 3 {
		t.Fatalf("rows = %+v, want 3", loaded.rows)
	}
	for i, want := range []string{"(task GHOSTTASK1)", "(time entry GHOSTLOG1)", "Was here"} {
		if loaded.rows[i].title != want {
			t.Errorf("row %d title = %q, want %q", i, loaded.rows[i].title, want)
		}
		// Enter has nowhere to go for any of the three, the task is not in the cache.
		if loaded.rows[i].task.ID != "" {
			t.Errorf("row %d task = %q, want none, the cache has no such task", i, loaded.rows[i].task.ID)
		}
	}
}

// A row being sent right now belongs to the engine, the keys say so instead of handing a stale count to the status bar.
func TestRetryAndDiscardOnARowBeingSentOnlyWriteTheStatusLine(t *testing.T) {
	m := changesModel{keys: defaultKeyMap(), height: 6}
	m.set([]changeRow{{row: store.OutboxRow{ID: 7, State: store.StateInflight}}})
	for _, k := range []string{"r", "x"} {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		if msg, ok := cmd().(toastMsg); !ok || msg.text != "being sent right now" {
			t.Errorf("%s on a row being sent = %#v, want the toast", k, cmd())
		}
	}
}

func TestEnterOnARowWithoutATaskSaysSo(t *testing.T) {
	m := changesModel{keys: defaultKeyMap(), height: 6}
	m.set([]changeRow{{row: store.OutboxRow{ID: 7, State: store.StateDiscarded}, title: "(task local:3)"}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a row without a task did nothing, want a status line")
	}
	if msg, ok := cmd().(toastMsg); !ok || msg.text != "the task is not in the cache" {
		t.Errorf("enter = %#v, want the toast", cmd())
	}
}

// TestEnqueueWriteOpMapsNotFoundToAPlainToastWithTheCounts covers the case where the engine already took the
// row inflight, or somebody else cleared it, between the screen listing it and the key
// press acting on it. store.ErrNotFound then must not surface as a raw "store: not found" toast.
func TestEnqueueWriteOpMapsNotFoundToAPlainToastWithTheCounts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "write-op.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := st.Outbox().EnqueueTaskUpdate(context.Background(), "T1", store.TaskUpdatePayload{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Now: func() time.Time { return time.Now() }})
	cmd := m.enqueueWriteOp(func(ctx context.Context) error { return store.ErrNotFound }, "Retrying")
	got, ok := cmd().(writeQueuedMsg)
	if !ok {
		t.Fatalf("enqueueWriteOp on ErrNotFound = %#v, want writeQueuedMsg", cmd())
	}
	if got.toast != "already being sent, list refreshed" {
		t.Errorf("toast = %q, want a plain sentence rather than the raw store error", got.toast)
	}
	// The status bar copies the counts from this message, zeros would wipe the real ones until the next engine event.
	if got.pending != 1 {
		t.Errorf("pending = %d, want the queue's count read along with the toast", got.pending)
	}
}
