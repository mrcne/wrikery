package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// The task search returns these only when asked.
var pullFields = []string{"description", "responsibleIds", "parentIds", "superTaskIds", "dependencyIds", "attachmentCount"}

// dependencyBatch caps the edge reads of one cycle, a first pull of a large plan spreads them over a few cycles.
const dependencyBatch = 200

func pullReference(ctx context.Context, c Client, st *store.Store) error {
	folders, err := c.FolderTree(ctx)
	if err != nil {
		return err
	}
	// The client asks for the live folders only, a deleted folder found by title on the command line would take a new task with it.
	if err := st.Folders().ReplaceTree(ctx, foldersFromWrike(folders)); err != nil {
		return err
	}
	contacts, err := c.Contacts(ctx)
	if err != nil {
		return err
	}
	if err := st.Contacts().ReplaceAll(ctx, contactsFromWrike(contacts)); err != nil {
		return err
	}
	spaces, err := c.Spaces(ctx)
	if err != nil {
		return err
	}
	if err := st.Spaces().ReplaceAll(ctx, spacesFromWrike(spaces)); err != nil {
		return err
	}
	workflows, err := c.Workflows(ctx)
	if err != nil {
		return err
	}
	// The account call leaves out the workflows a space owns, and a task in such a space holds a status from one of those.
	// The union is deduplicated by id, so one answer that repeats a workflow the account call listed inserts nothing twice.
	seen := make(map[string]bool, len(workflows))
	for _, wf := range workflows {
		seen[wf.ID] = true
	}
	for _, sp := range spaces {
		owned, err := c.SpaceWorkflows(ctx, sp.ID)
		if isNotFound(err) {
			// The space call is not on Wrike's reference page, see wrike.SpaceWorkflows.
			// Should it stop answering one day, the cycle keeps the account workflows instead of failing for good.
			continue
		}
		if err != nil {
			return err
		}
		for _, wf := range owned {
			if seen[wf.ID] {
				continue
			}
			seen[wf.ID] = true
			workflows = append(workflows, wf)
		}
	}
	return st.Workflows().ReplaceAll(ctx, workflowsFromWrike(workflows))
}

func scopeParams(sc store.Scope, meID string) wrike.TaskParams {
	// A subtask with no folder of its own is listed only with subTasks, and the sweep shares these parameters,
	// so without it such a subtask would never be cached, or be pruned at the next sweep.
	// The searches answer live tasks only, so a deleted task leaves the cache through the sweep and needs no guard here:
	// a task in the Recycle Bin was absent from the space, folder and responsible searches while GET /tasks/{id}
	// still answered it with scope RbTask, checked on the live account on 2026-10-08, the reference does not say.
	p := wrike.TaskParams{PageSize: 1000, SubTasks: true}
	switch sc.Kind {
	case store.ScopeKindMe:
		p.Responsibles = []string{meID}
	case store.ScopeKindSpace:
		p.SpaceID = sc.ID
		p.Descendants = true
	default:
		p.FolderID = sc.ID
		p.Descendants = true
	}
	return p
}

// pullScope walks one scope through the updatedDate filter and returns the ids of the tasks it saw.
// Without a cursor that is every task in the scope.
// The cursor travels with the final page only,
// so a crash between pages re-pulls from the old cursor and the upserts stay idempotent.
func pullScope(ctx context.Context, c Client, st *store.Store, sc store.Scope, meID string) ([]string, error) {
	p := scopeParams(sc, meID)
	p.Fields = pullFields
	if sc.Cursor != "" {
		after, err := time.Parse(time.RFC3339, sc.Cursor)
		if err != nil {
			return nil, fmt.Errorf("sync: scope %s cursor: %w", sc.ID, err)
		}
		p.UpdatedAfter = after
	}
	maxSeen := sc.Cursor
	var seen []string
	for {
		page, err := c.Tasks(ctx, p)
		if err != nil {
			return nil, err
		}
		for _, t := range page.Tasks {
			seen = append(seen, t.ID)
			if u := rfc3339(t.UpdatedDate); u > maxSeen {
				maxSeen = u
			}
		}
		cursor := ""
		if page.NextPageToken == "" {
			cursor = maxSeen
			if cursor == "" {
				// An empty scope on the initial pull has nothing to date the cursor with, start incremental polling from now.
				cursor = rfc3339(time.Now())
			}
		}
		if err := st.Tasks().ApplyPage(ctx, sc.ID, tasksFromWrike(page.Tasks), cursor); err != nil {
			return nil, err
		}
		if page.NextPageToken == "" {
			return seen, nil
		}
		p.PageToken = page.NextPageToken
	}
}

// sweep deletes tasks that vanished from every followed scope.
// It only ever prunes with the complete union, a partial one would delete live tasks and cascade their comments.
// full holds the ids of the scopes pulled from scratch in this cycle, those are not crawled a second time.
func sweep(ctx context.Context, c Client, st *store.Store, scopes []store.Scope, meID string, full map[string][]string) error {
	var keep []string
	for _, sc := range scopes {
		if ids, ok := full[sc.ID]; ok {
			keep = append(keep, ids...)
			continue
		}
		p := scopeParams(sc, meID)
		for {
			page, err := c.Tasks(ctx, p)
			if err != nil {
				return err
			}
			for _, t := range page.Tasks {
				keep = append(keep, t.ID)
			}
			if page.NextPageToken == "" {
				break
			}
			p.PageToken = page.NextPageToken
		}
	}
	_, err := st.Tasks().PruneExcept(ctx, keep)
	return err
}

// refreshThreads pulls comments, timelogs and dependencies for recently opened tasks and reports which caches it touched.
// A 404 on the thread means the task is gone on the server, drop it. Any other rejection skips the task,
// it stays in the window for days and must not block the other threads for that long.
// The dependency read comes after the thread and has an outcome of its own:
// the comment read is the one that says whether the task exists, and a rejected dependency read must not undo a thread already written.
func refreshThreads(ctx context.Context, c Client, st *store.Store, log *slog.Logger, window time.Duration, limit int) ([]EntityKind, error) {
	since := rfc3339(time.Now().Add(-window))
	ids, err := st.Tasks().RecentlyOpenedIDs(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	threads, dropped, deps := false, false, false
	for _, id := range ids {
		err := refreshThread(ctx, c, st, id)
		switch {
		case err == nil:
			threads = true
		case isNotFound(err):
			if err := st.Tasks().Delete(ctx, id); err != nil {
				return nil, err
			}
			dropped = true
			continue
		case classify(err) == failPermanent:
			log.Warn("thread refresh rejected", "task", id, "error", err)
			continue
		default:
			return nil, err
		}
		switch _, err := refreshDependencies(ctx, c, st, id); {
		case err == nil:
			deps = true
		case isNotFound(err) || classify(err) == failPermanent:
			log.Warn("dependency read rejected", "task", id, "error", err)
		default:
			return nil, err
		}
	}
	var touched []EntityKind
	if threads {
		touched = append(touched, KindComments, KindTimelogs)
	}
	if dropped {
		touched = append(touched, KindTasks)
	}
	if deps {
		touched = append(touched, KindDependencies)
	}
	return touched, nil
}

// pullMyTimelogs replaces the store's rows for the window as a whole, so a deleted or moved entry disappears too.
// The page size is the documented maximum (https://developers.wrike.com/api/v4/timelogs/),
// so the window is usually one request.
// The window start goes to the meta table afterwards, for the timesheet to tell an unsynced week from an empty one.
func pullMyTimelogs(ctx context.Context, c Client, st *store.Store, meID string, now time.Time) (bool, error) {
	from, to := store.TimelogWindow(now)
	p := wrike.TimelogParams{Me: true, TrackedFrom: from, TrackedTo: to, PageSize: 1000}
	var all []wrike.Timelog
	for {
		page, err := c.Timelogs(ctx, p)
		if err != nil {
			return false, err
		}
		all = append(all, page.Timelogs...)
		// An empty page ends the walk even when it carries a token.
		// Wrike sends one with an empty answer (seen on an account with no entries) and refuses it on the next request.
		// The reference (https://developers.wrike.com/api/v4/timelogs/) only says the token applies an offset to the next page.
		if page.NextPageToken == "" || len(page.Timelogs) == 0 {
			break
		}
		p.PageToken = page.NextPageToken
	}
	changed, err := st.Timelogs().ReplaceForUserRange(ctx, meID, from, to, timelogsFromWrike(all))
	if err != nil {
		return false, err
	}
	if err := st.SetMeta(ctx, store.MetaKeyTimelogFrom, from); err != nil {
		return false, err
	}
	return changed, nil
}

func refreshThread(ctx context.Context, c Client, st *store.Store, id string) error {
	comments, err := c.TaskComments(ctx, id)
	if err != nil {
		return err
	}
	if err := st.Comments().ReplaceForTask(ctx, id, commentsFromWrike(comments)); err != nil {
		return err
	}
	logs, err := c.TaskTimelogs(ctx, id)
	if err != nil {
		return err
	}
	return st.Timelogs().ReplaceForTask(ctx, id, timelogsFromWrike(logs))
}

// refreshDependencies replaces the task's dependency list with what Wrike holds now and returns it.
// Adding or removing a dependency moves neither task's updatedDate (checked on the live account on 2026-10-08),
// so the poll never brings such a change and only this read, part of the thread refresh of an opened task, notices it.
func refreshDependencies(ctx context.Context, c Client, st *store.Store, id string) ([]store.Dependency, error) {
	deps, err := c.TaskDependencies(ctx, id)
	if err != nil {
		return nil, err
	}
	out := dependenciesFromWrike(deps)
	return out, st.Dependencies().ReplaceForTask(ctx, id, out)
}

// pullDependencies fetches the edges of the tasks that list a dependency id with no edge in the cache yet.
// The task's own endpoint answers every edge of the task, so the other end is covered by the same read and skipped,
// and a cache with every edge in costs no request.
// A rejection of any kind, a 404 included, forgets the task's list instead of the task:
// the scope pull listed the task in this very cycle and the sweep is what decides a task is gone,
// and without the forget the read and its warning would repeat every cycle.
// The ids come back with the task's next pull, which is the moment a new read makes sense.
func pullDependencies(ctx context.Context, c Client, st *store.Store, log *slog.Logger) (bool, error) {
	missing, err := st.Dependencies().TasksMissingEdges(ctx, dependencyBatch)
	if err != nil {
		return false, err
	}
	changed := false
	covered := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(missing)) {
		if !slices.ContainsFunc(missing[id], func(depID string) bool { return !covered[depID] }) {
			continue
		}
		deps, err := refreshDependencies(ctx, c, st, id)
		switch {
		case err == nil:
			for _, d := range deps {
				covered[d.ID] = true
			}
			changed = true
		case isNotFound(err) || classify(err) == failPermanent:
			log.Warn("dependency read rejected", "task", id, "error", err)
			if err := st.Dependencies().ForgetForTask(ctx, id); err != nil {
				return changed, err
			}
		default:
			return changed, err
		}
	}
	return changed, nil
}
