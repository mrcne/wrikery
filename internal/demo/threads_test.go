package demo

import (
	"context"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

func TestExtendThreadsAddsLongDiscussionsSpreadOverYears(t *testing.T) {
	st, now := seededForExtend(t)
	ctx := context.Background()
	if err := Extend(ctx, st, now, 200); err != nil {
		t.Fatal(err)
	}
	since := now.AddDate(0, 0, -store.InboxDays).UTC().Format(time.RFC3339)
	marker, err := st.GetMeta(ctx, store.MetaKeyInboxSeenTo)
	if err != nil {
		t.Fatal(err)
	}
	clockBefore := st.Now()

	if err := ExtendThreads(ctx, st, now, 200, 3); err != nil {
		t.Fatal(err)
	}

	// Task 1 has no comments of Extend's, task 0 has two and is the long thread, a hundred times the others.
	if got, err := st.Comments().ListForTask(ctx, "IEAAGEN000001"); err != nil || len(got) != 3 {
		t.Errorf("an ordinary task has %d comments, %v, want 3", len(got), err)
	}
	long, err := st.Comments().ListForTask(ctx, LongThread)
	if err != nil || len(long) != 300+2 {
		t.Fatalf("the long thread has %d comments, %v, want 302", len(long), err)
	}
	oldest, latestArrival := long[0].CreatedDate, ""
	for _, c := range long {
		if c.ArrivedAt < c.CreatedDate {
			t.Errorf("%s arrived %s before it was written %s", c.ID, c.ArrivedAt, c.CreatedDate)
		}
		oldest, latestArrival = min(oldest, c.CreatedDate), max(latestArrival, c.ArrivedAt)
	}
	if oldest > now.AddDate(-2, 0, 0).Format(time.RFC3339) {
		t.Errorf("the oldest comment is from %s, want a history of more than two years", oldest)
	}
	if latestArrival > marker {
		t.Errorf("a comment arrived %s, after the seen marker %s", latestArrival, marker)
	}
	if unread, err := st.Comments().InboxUnread(ctx, MeID, since, marker); err != nil || unread != 2 {
		t.Errorf("unread = %d, %v, want the seed's 2", unread, err)
	}
	if !st.Now().Equal(clockBefore) {
		t.Errorf("the store clock was not put back, it reads %v", st.Now())
	}
}

func TestExtendThreadsWithZeroAddsNothing(t *testing.T) {
	st, now := seededForExtend(t)
	ctx := context.Background()
	if err := Extend(ctx, st, now, 20); err != nil {
		t.Fatal(err)
	}
	if err := ExtendThreads(ctx, st, now, 20, 0); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Comments().ListForTask(ctx, "IEAAGEN000001"); err != nil || len(got) != 0 {
		t.Errorf("comments = %d, %v, want none", len(got), err)
	}
}
