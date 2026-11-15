package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/store"
)

// seedInbox follows TASK2 and brings four comments in over three polls: one on TASK2 yesterday, which the user saw,
// then a mention on TASK3, a task they neither follow nor are on, and a second comment on TASK2, both unread.
// The user's own comment on TASK2 is in the cache too and never in the inbox.
func seedInbox(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	followed, err := st.Tasks().Get(ctx, "TASK2")
	must(err)
	followed.FollowedByMe = true
	must(st.Tasks().Upsert(ctx, []store.Task{followed}))
	arrive := func(day, hour int, comments ...store.Comment) {
		st.Now = func() time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC) }
		_, err := st.Comments().UpsertCached(ctx, comments)
		must(err)
	}
	arrive(2, 9,
		store.Comment{ID: "C1", TaskID: "TASK2", AuthorID: "U2", Text: "First line\nsecond line", CreatedDate: "2026-10-02T08:00:00Z"},
		store.Comment{ID: "C0", TaskID: "TASK2", AuthorID: "U1", Text: "My own answer", CreatedDate: "2026-10-02T08:30:00Z"})
	must(st.Comments().MarkInboxSeen(ctx, "2026-10-02T09:00:00Z"))
	arrive(3, 10, store.Comment{ID: "C2", TaskID: "TASK3", AuthorID: "U2", Text: "Can you look at this?", CreatedDate: "2026-10-03T09:30:00Z", Mentions: []string{"U1"}})
	arrive(3, 11, store.Comment{ID: "C3", TaskID: "TASK2", AuthorID: "U2", Text: "Done on my side", CreatedDate: "2026-10-03T11:00:00Z"})
	st.Now = func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }
}

func seenTo(t *testing.T, st *store.Store) string {
	t.Helper()
	v, err := st.GetMeta(context.Background(), store.MetaKeyInboxSeenTo)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	return v
}

func TestInboxListsTheCommentsNewestFirstWithTheStarAndTheMention(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	seedInbox(t, env.Store)
	if code := Run(context.Background(), env, []string{"inbox"}); code != exitOK {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	want := []string{
		"* 1 h ago    Reach tasks outside the followed scopes  Anna Nowak: Done on my side",
		"* 2 h ago    Custom fields                            Anna Nowak mentioned you: Can you look at this?",
		"  yesterday  Reach tasks outside the followed scopes  Anna Nowak: First line second line",
	}
	if got := strings.TrimRight(out.String(), "\n"); got != strings.Join(want, "\n") {
		t.Errorf("rows:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

func TestInboxJSONCarriesTheTaskTheAuthorAndTheMarks(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	seedInbox(t, env.Store)
	if code := Run(context.Background(), env, []string{"inbox", "--json"}); code != exitOK {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	var got []inboxJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(got) != 3 || got[0].ID != "C3" || got[1].ID != "C2" || got[2].ID != "C1" {
		t.Fatalf("rows = %+v, want C3, C2, C1", got)
	}
	if want := (inboxJSON{ID: "C2", TaskID: "TASK3", TaskTitle: "Custom fields", AuthorID: "U2", Author: "Anna Nowak",
		Created: "2026-10-03T09:30:00Z", Mentioned: true, Unread: true, Text: "Can you look at this?"}); got[1] != want {
		t.Errorf("the mention = %+v, want %+v", got[1], want)
	}
	if got[0].Mentioned || !got[0].Unread || got[2].Unread || got[2].Text != "First line\nsecond line" {
		t.Errorf("rows = %+v, want the followed task's comments unmarked as mentions, the seen one not unread, the text with its line break", got)
	}
	// An empty inbox is an empty list, never null.
	empty, emptyOut, _ := testEnv(t)
	seedBoard(t, empty.Store)
	if code := Run(context.Background(), empty, []string{"inbox", "--json"}); code != exitOK || strings.TrimSpace(emptyOut.String()) != "[]" {
		t.Errorf("empty inbox: code %d, out %q, want []", code, emptyOut.String())
	}
}

func TestInboxUnreadAndLimitNarrowTheRows(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--unread"}, "C3,C2"},
		{[]string{"--limit", "1"}, "C3"},
		{[]string{"--limit", "2"}, "C3,C2"},
		{[]string{"--unread", "--limit", "5"}, "C3,C2"},
	} {
		env, out, errOut := testEnv(t)
		seedBoard(t, env.Store)
		seedInbox(t, env.Store)
		if code := Run(context.Background(), env, append([]string{"inbox", "--json"}, tc.args...)); code != exitOK {
			t.Fatalf("%v: code = %d, stderr %q", tc.args, code, errOut.String())
		}
		var got []inboxJSON
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("%v: %v\n%s", tc.args, err, out.String())
		}
		var ids []string
		for _, r := range got {
			ids = append(ids, r.ID)
		}
		if strings.Join(ids, ",") != tc.want {
			t.Errorf("%v: rows = %v, want %s", tc.args, ids, tc.want)
		}
	}
}

// A read by a script must not take the star off a comment the person has not looked at, only --seen moves the marker.
func TestInboxMovesTheMarkerOnlyWithSeen(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	seedInbox(t, env.Store)
	env, got := uiStub(t, env, control.Reply{OK: true})
	ctx := context.Background()
	for _, args := range [][]string{{"inbox"}, {"inbox", "--unread", "--json"}, {"inbox", "--limit", "1"}} {
		if code := Run(ctx, env, args); code != exitOK {
			t.Fatalf("%v: code = %d, stderr %q", args, code, errOut.String())
		}
	}
	if at := seenTo(t, env.Store); at != "2026-10-02T09:00:00Z" || len(*got) != 0 {
		t.Fatalf("after three reads the marker is %q and the interface got %+v, want the marker where it was and nothing sent", at, *got)
	}
	out.Reset()
	if code := Run(ctx, env, []string{"inbox", "--unread", "--seen"}); code != exitOK {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "* 1 h ago") || strings.Count(out.String(), "\n") != 2 {
		t.Errorf("out = %q, want the two unread rows with their stars, as they were before the marker moved", out.String())
	}
	if at := seenTo(t, env.Store); at != "2026-10-03T11:00:00Z" {
		t.Errorf("marker = %q, want the arrival of the newest row printed", at)
	}
	if n, err := env.Store.Comments().InboxUnread(ctx, "U1", "2026-09-03T12:00:00Z", seenTo(t, env.Store)); err != nil || n != 0 {
		t.Errorf("unread = %d, %v, want none left", n, err)
	}
	if len(*got) != 1 || (*got)[0].Op != control.OpReload || strings.Join((*got)[0].Entities, ",") != "comments" {
		t.Errorf("the interface got %+v, want one reload of the comments so its count drops", *got)
	}
	// With nothing unread the marker has nowhere to go and nobody is told.
	if code := Run(ctx, env, []string{"inbox", "--seen"}); code != exitOK || len(*got) != 1 {
		t.Errorf("a second --seen: code %d, the interface got %d requests, want 0 and still one", code, len(*got))
	}
}

func TestInboxOnATerminalFramesTheRowsAndCutsThemToTheWidth(t *testing.T) {
	env, out, errOut := testEnv(t)
	seedBoard(t, env.Store)
	seedInbox(t, env.Store)
	env.Width = 70
	if code := Run(context.Background(), env, []string{"inbox", "--seen"}); code != exitOK {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	want := []string{
		"  WHEN       TASK                                     COMMENT",
		"* 1 h ago    Reach tasks outside the followed scopes  Anna Nowak: Done",
		"* 2 h ago    Custom fields                            Anna Nowak menti",
		"  yesterday  Reach tasks outside the followed scopes  Anna Nowak: Firs",
		"3 comments, 2 unread",
		"marked as seen",
	}
	if got := strings.TrimRight(out.String(), "\n"); got != strings.Join(want, "\n") {
		t.Errorf("out:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	out.Reset()
	if code := Run(context.Background(), env, []string{"inbox", "--unread"}); code != exitOK || out.String() != "No unread comments.\n" {
		t.Errorf("nothing unread: code %d, out %q", code, out.String())
	}
}

func TestInboxEmptySaysSoOnATerminalAndNothingInAPipe(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	if code := Run(context.Background(), env, []string{"inbox"}); code != exitOK || out.String() != "" {
		t.Errorf("in a pipe: code %d, out %q, want nothing", code, out.String())
	}
	env.Width = 80
	if code := Run(context.Background(), env, []string{"inbox"}); code != exitOK || out.String() != "No comments for you in the last 30 days.\n" {
		t.Errorf("on a terminal: code %d, out %q", code, out.String())
	}
}

func TestInboxUsage(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"TASK1"}, "inbox takes flags only"},
		{[]string{"--limit", "-1"}, "--limit takes a count"},
		{[]string{"--seen", "--limit", "2"}, "it does not go with --limit"},
	} {
		env, _, errOut := testEnv(t)
		seedBoard(t, env.Store)
		seedInbox(t, env.Store)
		if code := Run(context.Background(), env, append([]string{"inbox"}, tc.args...)); code != exitUsage {
			t.Errorf("%v: code = %d, want %d", tc.args, code, exitUsage)
		}
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%v: stderr = %q, want %q", tc.args, errOut.String(), tc.want)
		}
		if at := seenTo(t, env.Store); at != "2026-10-02T09:00:00Z" {
			t.Errorf("%v: the marker moved to %q on a refused command", tc.args, at)
		}
	}
}

func TestInboxBeforeTheFirstSyncSaysToSync(t *testing.T) {
	env, _, errOut := testEnv(t)
	if code := Run(context.Background(), env, []string{"inbox"}); code != exitError || !strings.Contains(errOut.String(), "run wrikery sync first") {
		t.Errorf("code = %d, stderr %q", code, errOut.String())
	}
}
