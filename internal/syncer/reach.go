package syncer

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// ErrNoSuchTask is the answer to a reach for a task Wrike does not list for this user.
// A task that does not exist, one the user has no access to and one in the Recycle Bin read the same from outside.
var ErrNoSuchTask = errors.New("sync: no such task on Wrike, or no access")

// scopeRecycleBin is the TreeScope of a deleted task (https://developers.wrike.com/api/v4/tasks/).
// A read by id still answers such a task, the searches leave it out, so the reach treats it as no answer.
const scopeRecycleBin = "RbTask"

// The by id call answers the description, the people, the folders, the super tasks and the dependency ids without being asked
// and refuses those names in fields with 400 invalid_parameter, only the attachment count has to be asked for
// (checked on the live account on 2026-10-22, the reference does not say).
var reachFields = []string{"attachmentCount"}

// reachBatch is the documented limit of GET /tasks/{ids}, so one request covers a cycle's referenced tasks.
const reachBatch = 1000

// permalinkFor builds the link the permalink filter takes from the number in it.
// The host does not matter to the filter, the token names the account (checked on the live account on 2026-10-22).
func permalinkFor(number string) string { return "https://www.wrike.com/open.htm?id=" + number }

// Reach fetches one task the followed scopes do not cover, by id or by the number from its link, and caches it with the reach stamp.
// It runs without the sync lock: it writes task rows in one transaction and touches no cursor, so it can run while a cycle does.
// A 404, a 400 for a malformed id, an answer without the task and a task in the Recycle Bin all mean ErrNoSuchTask.
// A 400 about the request itself, a field the call refuses for example, is not about the task and travels up as it is.
func (e *Engine) Reach(ctx context.Context, ref store.TaskRef) (store.Task, error) {
	var found []wrike.Task
	var err error
	if ref.Kind == store.RefNumber {
		// A subtask without a folder of its own is listed only with subTasks, as in the scope pull, and a pasted link is often one.
		var page wrike.TasksPage
		page, err = e.client.Tasks(ctx, wrike.TaskParams{Permalink: permalinkFor(ref.Value), SubTasks: true, Fields: pullFields})
		found = page.Tasks
	} else {
		found, err = e.client.TasksByIDs(ctx, []string{ref.Value}, reachFields)
	}
	switch {
	case isNotFound(err) || isMalformedID(err):
		return store.Task{}, ErrNoSuchTask
	case err != nil:
		return store.Task{}, err
	}
	found = live(found)
	if len(found) == 0 {
		return store.Task{}, ErrNoSuchTask
	}
	if err := e.st.Tasks().UpsertReached(ctx, tasksFromWrike(found[:1]), rfc3339(time.Now())); err != nil {
		return store.Task{}, err
	}
	e.emit(Event{Kind: EventStoreChanged, Entities: []EntityKind{KindTasks}})
	return e.st.Tasks().Get(ctx, found[0].ID)
}

// isMalformedID is the 400 for an id Wrike cannot parse, "Invalid Task ID" with the code invalid_request
// (seen on the live account on 2026-10-22), as opposed to a 400 about a parameter of the request.
func isMalformedID(err error) bool {
	var apiErr *wrike.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && apiErr.Code == "invalid_request"
}

// live drops the tasks an answer carries from the Recycle Bin, the reach treats them as not answered.
func live(tasks []wrike.Task) []wrike.Task {
	return slices.DeleteFunc(tasks, func(t wrike.Task) bool { return t.Scope == scopeRecycleBin })
}

// reachReferenced fetches the tasks a time entry or a relation of a followed task names and the cache lacks, one request a cycle.
// An id Wrike does not answer, or a batch it rejects for good, joins skip and is not asked again until the caller forgets it,
// so a related task the user cannot see costs one request and not one per minute.
// The query reads past the skipped ids, so a batch of them never hides the ids sorted behind it.
func reachReferenced(ctx context.Context, c Client, st *store.Store, meID, since string, skip map[string]bool, log *slog.Logger, now time.Time) (bool, error) {
	missing, err := st.Tasks().MissingReferencedIDs(ctx, meID, since, reachBatch+len(skip))
	if err != nil {
		return false, err
	}
	ids := slices.DeleteFunc(missing, func(id string) bool { return skip[id] })
	if len(ids) == 0 {
		return false, nil
	}
	ids = ids[:min(len(ids), reachBatch)]
	found, err := c.TasksByIDs(ctx, ids, reachFields)
	switch {
	case isNotFound(err):
		// The ordinary answer when the one id asked for is a task the user cannot see, nothing to log.
		found = nil
	case err != nil && classify(err) == failPermanent:
		log.Warn("reach rejected", "tasks", len(ids), "error", err)
		found = nil
	case err != nil:
		return false, err
	}
	found = live(found)
	answered := map[string]bool{}
	for _, t := range found {
		answered[t.ID] = true
	}
	for _, id := range ids {
		if !answered[id] {
			skip[id] = true
		}
	}
	if len(found) == 0 {
		return false, nil
	}
	return true, st.Tasks().UpsertReached(ctx, tasksFromWrike(found), rfc3339(now))
}

// rereadReached refreshes reached tasks, which no scope poll covers, and drops the ones Wrike no longer answers.
// The plain upsert keeps the stamp as it is: a re-read is neither a reach nor an open, so it must not extend the thirty days.
// A chunk Wrike rejects for good is skipped with a log line: the stamped set does not change on its own,
// so a failed cycle here would repeat every minute and stop the rest of the cycle with it.
func rereadReached(ctx context.Context, c Client, st *store.Store, ids []string, log *slog.Logger) (bool, error) {
	changed := false
	for chunk := range slices.Chunk(ids, reachBatch) {
		found, err := c.TasksByIDs(ctx, chunk, reachFields)
		switch {
		case isNotFound(err):
			found = nil
		case err != nil && classify(err) == failPermanent:
			log.Warn("reread rejected", "tasks", len(chunk), "error", err)
			continue
		case err != nil:
			return changed, err
		}
		// A task moved to the Recycle Bin is answered with that scope, the re-read drops it like one that is gone.
		found = live(found)
		answered := map[string]bool{}
		for _, t := range found {
			answered[t.ID] = true
		}
		var gone []string
		for _, id := range chunk {
			if !answered[id] {
				gone = append(gone, id)
			}
		}
		if len(found) > 0 {
			if err := st.Tasks().Upsert(ctx, tasksFromWrike(found)); err != nil {
				return changed, err
			}
			changed = true
		}
		if len(gone) > 0 {
			if err := st.Tasks().Delete(ctx, gone...); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	return changed, nil
}
