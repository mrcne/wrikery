package syncer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// The account wide reads: the comments created and the time entries changed since the last cycle, for every cached task.
// They cost two requests per cycle whatever the size of the cache, which is what keeps tens of thousands of cached tasks current.
// What they cannot show is an edit or a deletion, those come with the per task read of a task that changed, see rereadStale.
const (
	// commentWindow is the cap of the comments read, "The date range must be 7 days or shorter"
	// (https://developers.wrike.com/api/v4/comments/), a longer range answers 400 (checked on the live account).
	commentWindow = 7 * 24 * time.Hour
	// pullOverlap is how far before its cursor a read starts, for the skew between Wrike's clock and this one.
	// The writes are idempotent, a row read twice counts as no change.
	pullOverlap = 10 * time.Minute
	// pullCatchUp caps a gap at this many windows.
	// A cache nobody used for a month is caught up by the per task reads of the tasks that get opened, not by a day of requests.
	pullCatchUp = 4
	// commentLimit is the documented default of the read, sent so the split rule compares against a known number.
	commentLimit = 1000
	// splitFloor is the window taken as it is even when it fills the limit, a thousand comments in one hour is not a real account.
	splitFloor = time.Hour
)

// pullStart is where an account wide read begins: ten minutes before its cursor, the thread window back on the first run,
// and never more than pullCatchUp windows back.
func pullStart(ctx context.Context, st *store.Store, key string, window time.Duration, now time.Time) (time.Time, error) {
	v, err := st.GetMeta(ctx, key)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return time.Time{}, err
	}
	from := now.Add(-window)
	if cursor, err := time.Parse(time.RFC3339, v); err == nil {
		from = cursor.Add(-pullOverlap)
	}
	if floor := now.Add(-time.Duration(pullCatchUp) * window); from.Before(floor) {
		from = floor
	}
	return from, nil
}

// pullComments reads the comments created since the last pull in steps the API takes and keeps those of cached tasks.
// A rejection Wrike will not take back, a 400 or a 403, must not fail every cycle: it is logged and the cursor still moves,
// so the next cycle asks a small fresh range instead of the same one again. Any other failure fails the cycle and leaves the cursor.
func pullComments(ctx context.Context, c Client, st *store.Store, log *slog.Logger, window time.Duration, now time.Time) (bool, error) {
	from, err := pullStart(ctx, st, store.MetaKeyCommentsPulledTo, window, now)
	if err != nil {
		return false, err
	}
	changed := 0
	for a := from; a.Before(now); {
		b := a.Add(commentWindow)
		if b.After(now) {
			b = now
		}
		n, err := pullCommentWindow(ctx, c, st, a, b)
		if err != nil {
			if classify(err) != failPermanent {
				return changed > 0, err
			}
			log.Warn("comments read rejected", "error", err)
			break
		}
		changed += n
		a = b
	}
	if err := st.SetMeta(ctx, store.MetaKeyCommentsPulledTo, rfc3339(now)); err != nil {
		return changed > 0, err
	}
	return changed > 0, nil
}

// pullCommentWindow reads one range of at most commentWindow.
// The read has no paging and no documented order, so a full answer may have left comments out: two halves read the range again.
func pullCommentWindow(ctx context.Context, c Client, st *store.Store, from, to time.Time) (int, error) {
	comments, err := c.Comments(ctx, wrike.CommentParams{CreatedFrom: from, CreatedTo: to, Limit: commentLimit})
	if err != nil {
		return 0, err
	}
	if len(comments) >= commentLimit && to.Sub(from) > splitFloor {
		mid := from.Add(to.Sub(from) / 2)
		first, err := pullCommentWindow(ctx, c, st, from, mid)
		if err != nil {
			return first, err
		}
		second, err := pullCommentWindow(ctx, c, st, mid, to)
		return first + second, err
	}
	// A comment without a task is a folder's, the client has no place for it.
	onTasks := comments[:0:0]
	for _, cm := range comments {
		if cm.TaskID != "" {
			onTasks = append(onTasks, cm)
		}
	}
	return st.Comments().UpsertCached(ctx, commentsFromWrike(onTasks))
}

// pullTimelogChanges reads the time entries changed since the last pull, everyone's, and keeps those of cached tasks.
// The range has no cap, so a gap is one paged read. The rejection rule is the one of pullComments.
func pullTimelogChanges(ctx context.Context, c Client, st *store.Store, log *slog.Logger, window time.Duration, now time.Time) (bool, error) {
	from, err := pullStart(ctx, st, store.MetaKeyTimelogChangesPulledTo, window, now)
	if err != nil {
		return false, err
	}
	changed := 0
	logs, err := walkTimelogs(ctx, c, wrike.TimelogParams{UpdatedFrom: from, UpdatedTo: now, PageSize: 1000})
	switch {
	case err == nil:
		if changed, err = st.Timelogs().UpsertCached(ctx, timelogsFromWrike(logs)); err != nil {
			return false, err
		}
	case classify(err) == failPermanent:
		log.Warn("time entry changes read rejected", "error", err)
	default:
		return false, err
	}
	if err := st.SetMeta(ctx, store.MetaKeyTimelogChangesPulledTo, rfc3339(now)); err != nil {
		return changed > 0, err
	}
	return changed > 0, nil
}

// walkTimelogs reads every page of a timelog query.
// An empty page ends the walk even when it carries a token.
// Wrike sends one with an empty answer (seen on an account with no entries) and refuses it on the next request.
// The reference (https://developers.wrike.com/api/v4/timelogs/) only says the token applies an offset to the next page.
func walkTimelogs(ctx context.Context, c Client, p wrike.TimelogParams) ([]wrike.Timelog, error) {
	var all []wrike.Timelog
	for {
		page, err := c.Timelogs(ctx, p)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Timelogs...)
		if page.NextPageToken == "" || len(page.Timelogs) == 0 {
			return all, nil
		}
		p.PageToken = page.NextPageToken
	}
}
