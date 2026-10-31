package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

func inboxRows(n int) []inboxRow {
	rows := make([]inboxRow, n)
	for i := range rows {
		rows[i] = inboxRow{row: store.InboxRow{Comment: store.Comment{ID: "C" + string(rune('A'+i)), TaskID: "T1", Text: "row"}, TaskTitle: "Task"},
			task: store.Task{ID: "T1", Title: "Task"}, author: "Bartek Lis"}
	}
	return rows
}

// An unread row starts with a marker, a mention says so after the author, a seen row starts blank.
func TestInboxViewMarksUnreadRowsAndMentions(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	m := inboxModel{height: 10}
	// C1 was written first but entered the cache last, after the marker, so it is the unread one with C3.
	m.set([]inboxRow{
		{row: store.InboxRow{Comment: store.Comment{ID: "C3", TaskID: "T1", Text: "can you check the retry counts?", CreatedDate: "2026-09-03T11:00:00Z", ArrivedAt: "2026-09-03T11:01:00Z"}, TaskTitle: "Fix auth retry loop", Mentioned: true}, author: "Bartek Lis"},
		{row: store.InboxRow{Comment: store.Comment{ID: "C2", TaskID: "T1", Text: "logs attached", CreatedDate: "2026-09-03T10:00:00Z", ArrivedAt: "2026-09-03T10:01:00Z"}, TaskTitle: "Fix auth retry loop"}, author: "Bartek Lis"},
		{row: store.InboxRow{Comment: store.Comment{ID: "C1", TaskID: "T2", Text: "first\nsecond", CreatedDate: "2026-09-03T09:00:00Z", ArrivedAt: "2026-09-03T11:30:00Z"}, TaskTitle: "Add the backoff"}, author: "Celina Wrona"},
	}, "2026-09-03T10:30:00Z")
	lines := strings.Split(ansi.Strip(m.View(th, now, 100, 10)), "\n")
	if len(lines) != 4 {
		t.Fatalf("view has %d lines, want three rows and the cursor's text line:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[0], "> * Fix auth retry loop  Bartek Lis mentioned you: can you check the retry counts?") || !strings.HasSuffix(strings.TrimRight(lines[0], " "), "1 h ago") {
		t.Errorf("mention row = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "    can you check the retry counts?") {
		t.Errorf("cursor text line = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "    Fix auth retry loop  Bartek Lis: logs attached") {
		t.Errorf("seen row = %q, want no marker", lines[2])
	}
	if !strings.HasPrefix(lines[3], "  * Add the backoff  Celina Wrona: first second") {
		t.Errorf("late row = %q, want the marker by its arrival and the line break as a space", lines[3])
	}
}

func TestInboxViewWrapsTheCursorRowToThreeLines(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	m := inboxModel{height: 10}
	m.set([]inboxRow{{row: store.InboxRow{Comment: store.Comment{ID: "C1", TaskID: "T1", Text: strings.Repeat("x", 400), CreatedDate: "2026-09-03T09:00:00Z"}, TaskTitle: "Task"}, author: "Bartek Lis"}}, "")
	lines := strings.Split(ansi.Strip(m.View(th, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 60, 10)), "\n")
	if len(lines) != 4 {
		t.Fatalf("view has %d lines, want the row and three lines of text:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if ansi.StringWidth(line) > 60 {
			t.Errorf("line %d is %d wide: %q", i, ansi.StringWidth(line), line)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(lines[3], " "), "...") {
		t.Errorf("third text line = %q, want it cut with ...", lines[3])
	}
}

func TestInboxViewSaysWhenEmpty(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	m := inboxModel{height: 10}
	if out := m.View(th, time.Now(), 80, 10); !strings.Contains(out, "No comments for you in the last 30 days.") {
		t.Errorf("empty view = %q", out)
	}
}

func TestInboxKeysMoveAndEnterOpensTheTask(t *testing.T) {
	m := inboxModel{keys: defaultKeyMap(), height: 8}
	m.set(inboxRows(10), "")
	press := func(k tea.KeyMsg) {
		m, _ = m.Update(k)
	}
	for range 5 {
		press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if m.cursor != 5 || m.offset != 1 {
		t.Errorf("after five downs: cursor %d offset %d, want 5 and 1 with a capacity of height-3", m.cursor, m.offset)
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.cursor != 9 || m.offset != 5 {
		t.Errorf("after G: cursor %d offset %d, want 9 and 5", m.cursor, m.offset)
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("after g: cursor %d offset %d, want 0 and 0", m.cursor, m.offset)
	}
	press(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.cursor != 2 || m.offset != 0 {
		t.Errorf("after ctrl+d: cursor %d offset %d, want 2 and 0", m.cursor, m.offset)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter sent %#v, want one open", msgs)
	}
	if open, ok := msgs[0].(openTaskMsg); !ok || open.task.ID != "T1" {
		t.Errorf("enter sent %#v, want openTaskMsg with the row's task", msgs[0])
	}
}

// The marker is the newest arrival among the rows that were on screen, not the first row of the list:
// a row that arrived while the list was scrolled past its place was never seen.
func TestInboxSeenFollowsTheRowsDrawn(t *testing.T) {
	m := inboxModel{keys: defaultKeyMap(), height: 6}
	rows := inboxRows(10)
	for i := range rows {
		// Newest by creation first, but the arrivals run the other way: the last row entered the cache last.
		rows[i].row.ArrivedAt = fmt.Sprintf("2026-09-03T%02d:00:00Z", 1+i)
	}
	m.set(rows, "")
	if m.seen != "2026-09-03T03:00:00Z" {
		t.Errorf("seen after the load = %q, want the newest arrival of the three rows the pane holds", m.seen)
	}
	m.move(9)
	if m.seen != "2026-09-03T10:00:00Z" {
		t.Errorf("seen after scrolling to the end = %q, want the newest arrival of all", m.seen)
	}
	m.move(-9)
	if m.seen != "2026-09-03T10:00:00Z" {
		t.Errorf("seen after scrolling back = %q, want what was seen kept", m.seen)
	}
	var empty inboxModel
	if empty.seen != "" {
		t.Error("an empty inbox has seen nothing")
	}
}

// A reload lands a new comment at the top, the cursor stays on the comment it was on and not on its index.
func TestInboxSetKeepsTheCursorOnItsRow(t *testing.T) {
	m := inboxModel{keys: defaultKeyMap(), height: 10}
	rows := inboxRows(5)
	m.set(rows, "")
	m.move(2)
	newer := append([]inboxRow{{row: store.InboxRow{Comment: store.Comment{ID: "CNEW", TaskID: "T1"}}, author: "x"}}, rows...)
	m.set(newer, "")
	if m.cursor != 3 || m.rows[m.cursor].row.ID != "CC" {
		t.Errorf("cursor after a row landed above = %d on %q, want 3 on CC", m.cursor, m.rows[m.cursor].row.ID)
	}
	// The row under the cursor went, the cursor clamps as before.
	m.set(rows[:2], "")
	if m.cursor != 1 {
		t.Errorf("cursor after its row went = %d, want the last row", m.cursor)
	}
}

// A full list keeps its last line inside the box even when the cursor's comment takes three lines.
func TestInboxViewKeepsTheCutLineInTheBox(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	m := inboxModel{keys: defaultKeyMap(), height: 6}
	rows := inboxRows(inboxLimit)
	rows[inboxLimit-1].row.Text = strings.Repeat("x", 300)
	m.set(rows, "")
	m.move(inboxLimit)
	out := ansi.Strip(m.View(th, time.Now(), 80, 6))
	lines := strings.Split(out, "\n")
	if len(lines) > 6 || !strings.Contains(out, "stops at 200 rows") {
		t.Errorf("view at the end of a full list has %d lines for a height of 6, cut line present %v:\n%s", len(lines), strings.Contains(out, "stops at"), out)
	}
}

// An author drawn as separate emoji after the joiner is dropped must not push the time off the row.
func TestInboxRowKeepsTheTimeWithAnEmojiAuthor(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	m := inboxModel{height: 10}
	family := "Ada \U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466 Nowak"
	m.set([]inboxRow{{row: store.InboxRow{Comment: store.Comment{ID: "C1", TaskID: "T1", Text: strings.Repeat("word ", 30), CreatedDate: "2026-09-03T11:00:00Z"}, TaskTitle: "Task"}, author: family}}, "")
	line := strings.Split(ansi.Strip(m.View(th, now, 60, 10)), "\n")[0]
	if !strings.HasSuffix(strings.TrimRight(line, " "), "1 h ago") {
		t.Errorf("row = %q, want the time at its end", line)
	}
}

// The demo seeds two comments after its seen marker, so the bar starts at two and the screen opens on them.
func TestLeavingTheInboxWritesTheMarkerAndClearsTheCount(t *testing.T) {
	m := demoModel(t)
	if m.status.unread != 2 {
		t.Fatalf("unread at start = %d, want the demo's two", m.status.unread)
	}
	m = pump(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@")})
	if m.screen != screenInbox || len(m.inbox.rows) == 0 {
		t.Fatalf("@ gave screen %v with %d rows", m.screen, len(m.inbox.rows))
	}
	first := m.inbox.rows[0].row
	if first.ID != "IEAACMTI1" || !first.Mentioned || len(m.inbox.rows[0].task.ParentIDs) == 0 {
		t.Errorf("first row = %+v with task %+v, want the demo's mention of today with its task's folders for the jump", first, m.inbox.rows[0].task)
	}
	m = pump(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != screenMain {
		t.Fatalf("esc left the screen at %v", m.screen)
	}
	if got, _ := m.opts.Store.GetMeta(context.Background(), store.MetaKeyInboxSeenTo); got != first.ArrivedAt {
		t.Errorf("marker = %q, want the newest arrival on screen %q", got, first.ArrivedAt)
	}
	if m.status.unread != 0 {
		t.Errorf("unread after leaving = %d, want 0", m.status.unread)
	}
	m = pump(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@")}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenMain || m.selectedTaskID != first.TaskID {
		t.Errorf("enter gave screen %v on task %q, want the main screen on %q", m.screen, m.selectedTaskID, first.TaskID)
	}
}

func TestAStoreChangeReloadsTheInboxAndTheCount(t *testing.T) {
	m := demoModel(t)
	m = pump(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@")})
	first := m.inbox.rows[0].row
	ctx := context.Background()
	if _, err := m.opts.Store.Comments().UpsertCached(ctx, []store.Comment{{ID: "IEAACMTNEW", TaskID: first.TaskID, AuthorID: "KUAAAAB1", Text: "just now", CreatedDate: "2026-09-03T11:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	m = pump(m, StoreChangedMsg{Entities: []string{"comments"}})
	if now := m.inbox.rows[0].row; now.ID != "IEAACMTNEW" {
		t.Errorf("first row after the change = %+v, want the new comment", now)
	}
	if m.status.unread != 3 {
		t.Errorf("unread after the change = %d, want 3", m.status.unread)
	}
}

func TestInboxLoadsNothingWithoutAMeID(t *testing.T) {
	m := New(rootTestOptions(t))
	if msg, ok := m.loadInbox()().(inboxLoadedMsg); !ok || len(msg.rows) != 0 {
		t.Errorf("loadInbox without a me id = %#v, want no rows", msg)
	}
	if msg, ok := m.loadUnread()().(inboxUnreadMsg); !ok || msg.count != 0 {
		t.Errorf("loadUnread without a me id = %#v, want zero", msg)
	}
}

// q and a command's jump leave the screen like esc does, so what was read is not unread again at the next start.
func TestQuittingFromTheInboxWritesTheMarker(t *testing.T) {
	for _, quit := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("q")}, {Type: tea.KeyCtrlC}} {
		m := demoModel(t)
		m = pump(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@")})
		first := m.inbox.rows[0].row
		next, cmd := m.Update(quit)
		if cmd == nil {
			t.Fatalf("%v gave no command", quit)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%v from the inbox = %#v, want the quit", quit, cmd())
		}
		if got, _ := next.(Model).opts.Store.GetMeta(context.Background(), store.MetaKeyInboxSeenTo); got != first.ArrivedAt {
			t.Errorf("marker after %v = %q, want the newest arrival on screen %q", quit, got, first.ArrivedAt)
		}
	}
}

func TestControlFolderFromTheInboxWritesTheMarker(t *testing.T) {
	m := demoModel(t)
	m = pump(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@")})
	first := m.inbox.rows[0].row
	m = pump(m, ControlMsg{Op: OpFolder, FolderID: demo.ProjectAPI})
	if m.screen != screenMain {
		t.Errorf("folder gave screen %v", m.screen)
	}
	if got, _ := m.opts.Store.GetMeta(context.Background(), store.MetaKeyInboxSeenTo); got != first.ArrivedAt {
		t.Errorf("marker after the command = %q, want %q", got, first.ArrivedAt)
	}
}

func TestControlSelectFromTheInboxWritesTheMarker(t *testing.T) {
	m := demoModel(t)
	m = pump(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@")})
	first := m.inbox.rows[0].row
	m = pump(m, ControlMsg{Op: OpSelect, TaskID: first.TaskID})
	if m.screen != screenMain || m.selectedTaskID != first.TaskID {
		t.Errorf("select gave screen %v on %q", m.screen, m.selectedTaskID)
	}
	if got, _ := m.opts.Store.GetMeta(context.Background(), store.MetaKeyInboxSeenTo); got != first.ArrivedAt {
		t.Errorf("marker after the command = %q, want %q", got, first.ArrivedAt)
	}
	if m.status.unread != 0 {
		t.Errorf("unread after the command = %d, want 0", m.status.unread)
	}
}

func TestInboxViewSaysWhenTheListIsCut(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	m := inboxModel{height: 300}
	m.set(inboxRows(inboxLimit), "")
	out := ansi.Strip(m.View(th, time.Now(), 80, 300))
	if !strings.Contains(out, "stops at 200 rows") {
		t.Errorf("a full list says nothing about the cut:\n%s", out[len(out)-200:])
	}
	m.set(inboxRows(3), "")
	if out := ansi.Strip(m.View(th, time.Now(), 80, 300)); strings.Contains(out, "stops at") {
		t.Errorf("a short list talks about a cut:\n%s", out)
	}
}
