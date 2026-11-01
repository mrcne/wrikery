package demo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

const (
	// LongThread is a generated task that ExtendThreads gives a long discussion, the first of every thousand.
	LongThread = "IEAAGEN000000"
	// threadDays is how far back the generated comments go, three years of discussion.
	threadDays = 3 * 365
	// threadSlice is the stretch of comments stored with one clock, a month, so a comment arrives within a month after it was written.
	threadSlice = 30
)

// ExtendThreads adds perTask comments to each of the first n tasks Extend generated, spread over three years,
// and a hundred times as many to every thousandth one, the long discussions of a large account.
// They go in month by month with the store clock at the end of each month, the way a sync stores them as they come,
// and never later than two days ago, below the seed's seen marker, so the inbox count stays the seed's.
// It is for a store Extend just filled with at least n tasks, and once.
func ExtendThreads(ctx context.Context, st *store.Store, now time.Time, n, perTask int) error {
	if n < 1 || perTask < 1 {
		return nil
	}
	_, stamp := clocks(now)
	count := func(i int) int {
		if i%1000 == 0 {
			return 100 * perTask
		}
		return perTask
	}
	// The comment j of task i is written this many days ago, an even spread without a random source,
	// and three days back at the least, so it is older than the latest arrival below.
	daysAgo := func(i, j int) int { return 3 + (i*7+j*13)%(threadDays-3) }
	prevNow := st.Now
	defer func() { st.Now = prevNow }()
	latest := now.AddDate(0, 0, -2)
	var batch []store.Comment
	flush := func() error {
		_, err := st.Comments().UpsertCached(ctx, batch)
		batch = batch[:0]
		return err
	}
	for end := threadDays - threadSlice; end > -threadSlice; end -= threadSlice {
		// The slice holds the comments written from end+threadSlice days ago up to end days ago,
		// they arrive at the midnight after the newest of those days.
		d := now.AddDate(0, 0, -end+1)
		arrived := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		if arrived.After(latest) {
			arrived = latest
		}
		st.Now = func() time.Time { return arrived }
		for i := range n {
			for j := range count(i) {
				if d := daysAgo(i, j); d >= end+threadSlice || d < end {
					continue
				}
				author := fmt.Sprintf("KUGEN%03d", (i+j)%extendPeople)
				if j%17 == 0 {
					author = MeID
				}
				var mentions []string
				switch {
				case (i+j)%100 == 0:
					mentions = []string{MeID}
				case (i+j)%500 == 1:
					mentions = []string{"@assignees"}
				}
				batch = append(batch, store.Comment{ID: fmt.Sprintf("IEAATHR%06d%05d", i, j), TaskID: fmt.Sprintf("IEAAGEN%06d", i),
					AuthorID: author, Text: threadText(i, j), CreatedDate: stamp(-daysAgo(i, j), 8+j%10), Mentions: mentions})
				if len(batch) == 5000 {
					if err := flush(); err != nil {
						return err
					}
				}
			}
		}
		if err := flush(); err != nil {
			return err
		}
	}
	return nil
}

// threadText is one to four sentences, the length of a comment in a discussion.
func threadText(i, j int) string {
	sentence := "Checked the change on staging, the numbers look right but the export still rounds the hours. "
	return fmt.Sprintf("Comment %d on this task. %s", j, strings.TrimSpace(strings.Repeat(sentence, 1+(i+j)%4)))
}
