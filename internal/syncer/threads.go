package syncer

import (
	"context"
	"errors"
	"log/slog"
	"slices"
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
		// What a failed step wrote before it failed is in the cache, the caller must hear about it.
		changed += n
		if err != nil {
			if classify(err) != failPermanent {
				return changed > 0, err
			}
			log.Warn("comments read rejected", "error", err)
			break
		}
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

// readThread is one per task read: the comments and the time entries with the stamp, then the dependencies when asked,
// and it reports the caches it touched.
// A 404 on the thread means the task is gone on the server: it is dropped, the tasks kind reported and ErrNoSuchTask answered.
// Any other rejection skips the task with a log line, it may be readable later and must not block the other threads.
// A stamped task then takes its updated date as the stamp and leaves the stale list until it changes again,
// or it would cost a request and a warning every cycle and, fifty of them, keep every other stale task from being read.
// A never read task keeps no stamp, the detail would say it is synced.
// The dependency read comes after the thread and has an outcome of its own:
// the comment read is the one that says whether the task exists, and a rejected dependency read must not undo a thread already written.
func readThread(ctx context.Context, c Client, st *store.Store, log *slog.Logger, task store.Task, deps bool) ([]EntityKind, error) {
	var touched []EntityKind
	switch err := refreshThread(ctx, c, st, task); {
	case err == nil:
		touched = append(touched, KindComments, KindTimelogs)
	case isNotFound(err):
		if err := st.Tasks().Delete(ctx, task.ID); err != nil {
			return nil, err
		}
		return []EntityKind{KindTasks}, ErrNoSuchTask
	case classify(err) == failPermanent:
		log.Warn("thread refresh rejected", "task", task.ID, "error", err)
		if task.ThreadReadAt != "" {
			if err := st.Tasks().MarkThreadRead(ctx, task.ID, task.UpdatedDate); err != nil {
				return nil, err
			}
		}
		return nil, nil
	default:
		return nil, err
	}
	if !deps {
		return touched, nil
	}
	switch _, err := refreshDependencies(ctx, c, st, task.ID); {
	case err == nil:
		touched = append(touched, KindDependencies)
	case isNotFound(err) || classify(err) == failPermanent:
		log.Warn("dependency read rejected", "task", task.ID, "error", err)
	default:
		return touched, err
	}
	return touched, nil
}

// readThreads reads the given tasks one by one and unions what they touched, a gone task is dropped and the rest go on.
// A task that left the cache between the listing and the read is skipped.
func readThreads(ctx context.Context, c Client, st *store.Store, log *slog.Logger, ids []string, deps bool) ([]EntityKind, error) {
	var touched []EntityKind
	for _, id := range ids {
		task, err := st.Tasks().Get(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		kinds, err := readThread(ctx, c, st, log, task, deps)
		if err != nil && !errors.Is(err, ErrNoSuchTask) {
			return nil, err
		}
		for _, k := range kinds {
			if !slices.Contains(touched, k) {
				touched = append(touched, k)
			}
		}
	}
	return touched, nil
}

// rereadStale reads again the threads of stamped tasks that changed since their stamp, up to limit, and reports what it touched.
// A new or a deleted comment moves the task's updatedDate (checked on the live account), an edited one is taken to do the same,
// the edit answered 403 on the account checked, and so is a change to a time entry, whose write that account refuses.
// So a stamped task the poll listed may have an edited or deleted comment or entry, which the account wide reads cannot show.
// The set is bounded by the tasks read before, the ones somebody looked at, not by the cache,
// and a task changed for another reason costs one read too, the price of one rule.
// The dependencies are left out: a change to them moves no updated date, so they cannot be why the task is here.
func rereadStale(ctx context.Context, c Client, st *store.Store, log *slog.Logger, limit int) ([]EntityKind, error) {
	ids, err := st.Tasks().StaleThreadIDs(ctx, limit)
	if err != nil {
		return nil, err
	}
	return readThreads(ctx, c, st, log, ids, false)
}

// FetchThread reads the thread of a task the cache never read, for the cursor resting on the task, enter and task show,
// and reports whether it read anything.
// It runs without the sync lock like Reach: it writes the rows of one task and touches no cursor, so it can run while a cycle does.
// A thread read once is kept current by the cycle, and a queued task is stamped when it is queued, so a second call costs no request.
// A 404 drops the task and answers ErrNoSuchTask, a rejection is logged and answers false, any other failure travels up.
// Before the first cycle compared the account of a credential no sign in checked, it reads nothing, see ErrAccountUnchecked.
func (e *Engine) FetchThread(ctx context.Context, id string) (bool, error) {
	if err := e.unchecked(ctx); err != nil {
		return false, err
	}
	task, err := e.st.Tasks().Get(ctx, id)
	if err != nil {
		return false, err
	}
	if task.ThreadReadAt != "" {
		return false, nil
	}
	kinds, err := readThread(ctx, e.client, e.st, e.log, task, true)
	if len(kinds) > 0 {
		e.emit(Event{Kind: EventStoreChanged, Entities: kinds})
	}
	return err == nil && len(kinds) > 0, err
}
