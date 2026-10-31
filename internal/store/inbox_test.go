package store

import (
	"context"
	"testing"
	"time"
)

// The set: comments by other people on the tasks the user follows, and the ones that mention the user,
// by id or through @assignees on a task the user is responsible for, the last thirty days, newest first.
func TestInboxListsTheCommentsAddressedToTheUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	followed, mine, theirs, followedToo := makeTask("T1", "followed"), makeTask("T2", "mine"), makeTask("T3", "theirs"), makeTask("T4", "followed too")
	followed.FollowedByMe, followedToo.FollowedByMe = true, true
	mine.ResponsibleIDs, theirs.ResponsibleIDs = []string{"U1"}, []string{"U2"}
	if err := st.Tasks().Upsert(ctx, []Task{followed, mine, theirs, followedToo}); err != nil {
		t.Fatal(err)
	}
	comments := []Comment{
		{ID: "C1", TaskID: "T1", AuthorID: "U2", Text: "on a followed task", CreatedDate: "2026-09-09T10:00:00Z"},
		{ID: "C2", TaskID: "T2", AuthorID: "U2", Text: "mentions me", CreatedDate: "2026-09-09T11:00:00Z", Mentions: []string{"U1"}},
		{ID: "C3", TaskID: "T2", AuthorID: "U2", Text: "to the assignees, me", CreatedDate: "2026-09-09T12:00:00Z", Mentions: []string{"@assignees"}},
		{ID: "C4", TaskID: "T3", AuthorID: "U2", Text: "to the assignees of someone else's task", CreatedDate: "2026-09-09T13:00:00Z", Mentions: []string{"@assignees"}},
		{ID: "C5", TaskID: "T3", AuthorID: "U2", Text: "on a task I neither follow nor am on", CreatedDate: "2026-09-09T14:00:00Z"},
		{ID: "C6", TaskID: "T1", AuthorID: "U1", Text: "my own", CreatedDate: "2026-09-09T15:00:00Z", Mentions: []string{"U1"}},
		{ID: "C7", TaskID: "T4", AuthorID: "U2", Text: "older than the window", CreatedDate: "2026-08-01T10:00:00Z"},
		{ID: "C8", TaskID: "T1", AuthorID: "U3", Text: "mentions me on a followed task", CreatedDate: "2026-09-09T16:00:00Z", Mentions: []string{"U4", "U1"}},
		{ID: "local:7", TaskID: "T1", AuthorID: "U1", Text: "an optimistic row of the user's own", CreatedDate: "2026-09-09T17:00:00Z"},
	}
	if _, err := st.Comments().UpsertCached(ctx, comments); err != nil {
		t.Fatal(err)
	}
	since := "2026-08-10T00:00:00Z"

	rows, err := st.Comments().Inbox(ctx, "U1", since, 200)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if want := []string{"C8", "C3", "C2", "C1"}; len(ids) != 4 || ids[0] != "C8" || ids[1] != "C3" || ids[2] != "C2" || ids[3] != "C1" {
		t.Fatalf("inbox = %v, want %v", ids, want)
	}
	if !rows[0].Mentioned || !rows[1].Mentioned || !rows[2].Mentioned || rows[3].Mentioned {
		t.Errorf("mentioned = %v %v %v %v, want the three mentions marked and the followed task's comment not", rows[0].Mentioned, rows[1].Mentioned, rows[2].Mentioned, rows[3].Mentioned)
	}
	if rows[0].TaskTitle != "followed" || rows[1].TaskTitle != "mine" || rows[0].Text != "mentions me on a followed task" || rows[0].AuthorID != "U3" {
		t.Errorf("rows = %+v, want the task title and the comment on each", rows[:2])
	}
	if short, _ := st.Comments().Inbox(ctx, "U1", since, 2); len(short) != 2 || short[0].ID != "C8" || short[1].ID != "C3" {
		t.Errorf("limit 2 = %+v, want the two newest", short)
	}

	if n, err := st.Comments().InboxUnread(ctx, "U1", since, ""); err != nil || n != 4 {
		t.Errorf("unread with no marker = %d, %v, want every row", n, err)
	}
}

// A row is unread by when it entered the cache, not by when it was written on Wrike:
// a task cached late brings older comments in after newer ones were seen.
func TestInboxUnreadCountsTheArrivalsAfterTheMarker(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	followed := makeTask("T1", "followed")
	followed.FollowedByMe = true
	if err := st.Tasks().Upsert(ctx, []Task{followed}); err != nil {
		t.Fatal(err)
	}
	clock := func(s string) { st.Now = func() time.Time { tm, _ := time.Parse(time.RFC3339, s); return tm } }
	clock("2026-09-09T12:00:00Z")
	if _, err := st.Comments().UpsertCached(ctx, []Comment{{ID: "C1", TaskID: "T1", AuthorID: "U2", Text: "newer, seen first", CreatedDate: "2026-09-09T11:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	clock("2026-09-09T13:00:00Z")
	if _, err := st.Comments().UpsertCached(ctx, []Comment{{ID: "C2", TaskID: "T1", AuthorID: "U2", Text: "older, arrived later", CreatedDate: "2026-09-09T10:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	since := "2026-08-10T00:00:00Z"
	rows, err := st.Comments().Inbox(ctx, "U1", since, 200)
	if err != nil || len(rows) != 2 || rows[0].ID != "C1" || rows[0].ArrivedAt != "2026-09-09T12:00:00Z" || rows[1].ArrivedAt != "2026-09-09T13:00:00Z" {
		t.Fatalf("inbox = %+v, %v, want the rows by creation date with their arrivals", rows, err)
	}
	if err := st.Comments().MarkInboxSeen(ctx, "2026-09-09T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	seen, _ := st.GetMeta(ctx, MetaKeyInboxSeenTo)
	if n, err := st.Comments().InboxUnread(ctx, "U1", since, seen); err != nil || n != 1 {
		t.Errorf("unread after the first arrival = %d, %v, want the late one", n, err)
	}
	// The marker never moves back, a second interface with an older list cannot star rows again.
	if err := st.Comments().MarkInboxSeen(ctx, "2026-09-09T13:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Comments().MarkInboxSeen(ctx, "2026-09-09T12:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if seen, _ := st.GetMeta(ctx, MetaKeyInboxSeenTo); seen != "2026-09-09T13:00:00Z" {
		t.Errorf("marker = %q, want the later one kept", seen)
	}
}

// Without a user nothing is addressed to anyone, the mention match must not fall through to an empty id.
func TestInboxAnswersNothingWithoutAUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	followed := makeTask("T1", "followed")
	followed.FollowedByMe = true
	if err := st.Tasks().Upsert(ctx, []Task{followed}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Comments().UpsertCached(ctx, []Comment{{ID: "C1", TaskID: "T1", AuthorID: "U2", Text: "hi", CreatedDate: "2026-09-09T10:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	if rows, err := st.Comments().Inbox(ctx, "", "2026-08-10T00:00:00Z", 200); err != nil || len(rows) != 0 {
		t.Errorf("Inbox without a user = %+v, %v, want nothing", rows, err)
	}
	if n, err := st.Comments().InboxUnread(ctx, "", "2026-08-10T00:00:00Z", ""); err != nil || n != 0 {
		t.Errorf("InboxUnread without a user = %d, %v, want 0", n, err)
	}
}
