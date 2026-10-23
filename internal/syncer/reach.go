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

// ErrNoSuchTask is the answer to a reach for a task Wrike does not list for this user, which reads the same from outside
// whether the task does not exist or the user has no access to it.
var ErrNoSuchTask = errors.New("sync: no such task on Wrike, or no access")

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
// A 404, a 400 for a malformed id, and an answer without the task all mean ErrNoSuchTask.
func (e *Engine) Reach(ctx context.Context, ref string) (store.Task, error) {
	r, ok := store.ParseTaskRef(ref)
	if !ok {
		return store.Task{}, ErrNoSuchTask
	}
	var found []wrike.Task
	var err error
	if r.Kind == store.RefNumber {
		var page wrike.TasksPage
		page, err = e.client.Tasks(ctx, wrike.TaskParams{Permalink: permalinkFor(r.Value), Fields: pullFields})
		found = page.Tasks
	} else {
		found, err = e.client.TasksByIDs(ctx, []string{r.Value}, reachFields)
	}
	switch {
	case isNotFound(err) || isBadRequest(err):
		return store.Task{}, ErrNoSuchTask
	case err != nil:
		return store.Task{}, err
	case len(found) == 0:
		return store.Task{}, ErrNoSuchTask
	}
	if err := e.st.Tasks().UpsertReached(ctx, tasksFromWrike(found[:1]), rfc3339(time.Now())); err != nil {
		return store.Task{}, err
	}
	e.emit(Event{Kind: EventStoreChanged, Entities: []EntityKind{KindTasks}})
	return e.st.Tasks().Get(ctx, found[0].ID)
}

func isBadRequest(err error) bool {
	var apiErr *wrike.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest
}

// reachReferenced fetches the tasks a time entry or a relation of a followed task names and the cache lacks, one request a cycle.
// An id Wrike does not answer, or a batch it rejects for good, joins skip and is not asked again until the caller forgets it,
// so a related task the user cannot see costs one request and not one per minute.
func reachReferenced(ctx context.Context, c Client, st *store.Store, skip map[string]bool, log *slog.Logger, now time.Time) (bool, error) {
	missing, err := st.Tasks().MissingReferencedIDs(ctx, reachBatch)
	if err != nil {
		return false, err
	}
	ids := slices.DeleteFunc(missing, func(id string) bool { return skip[id] })
	if len(ids) == 0 {
		return false, nil
	}
	found, err := c.TasksByIDs(ctx, ids, reachFields)
	if err != nil && classify(err) != failPermanent {
		return false, err
	}
	if err != nil {
		log.Warn("reach rejected", "tasks", len(ids), "error", err)
		found = nil
	}
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
func rereadReached(ctx context.Context, c Client, st *store.Store, ids []string) (bool, error) {
	changed := false
	for chunk := range slices.Chunk(ids, reachBatch) {
		found, err := c.TasksByIDs(ctx, chunk, reachFields)
		if isNotFound(err) {
			found, err = nil, nil
		}
		if err != nil {
			return changed, err
		}
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
