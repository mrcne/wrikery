package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// The task search returns these only when asked.
var pullFields = []string{"description", "responsibleIds", "parentIds"}

func pullReference(ctx context.Context, c Client, st *store.Store) error {
	folders, err := c.FolderTree(ctx)
	if err != nil {
		return err
	}
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
	return st.Workflows().ReplaceAll(ctx, workflowsFromWrike(workflows))
}

func scopeParams(sc store.Scope, meID string) wrike.TaskParams {
	p := wrike.TaskParams{PageSize: 1000}
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

// refreshThreads pulls comments and timelogs for recently opened tasks and reports which caches it touched.
// A 404 means the task is gone on the server, drop it. Any other rejection skips the task,
// it stays in the window for days and must not block the other threads for that long.
func refreshThreads(ctx context.Context, c Client, st *store.Store, log *slog.Logger, window time.Duration, limit int) ([]EntityKind, error) {
	since := rfc3339(time.Now().Add(-window))
	ids, err := st.Tasks().RecentlyOpenedIDs(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	threads, dropped := false, false
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
		case classify(err) == failPermanent:
			log.Warn("thread refresh rejected", "task", id, "error", err)
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
	return touched, nil
}

// timelogWindow is the current week plus the eight before it, Monday to Sunday.
// Older weeks are not shown in the timesheet.
func timelogWindow(now time.Time) (from, to string) {
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(wd - 1))
	return monday.AddDate(0, 0, -7*8).Format("2006-01-02"), monday.AddDate(0, 0, 6).Format("2006-01-02")
}

// pullMyTimelogs replaces the store's rows for the window as a whole, so a deleted or moved entry disappears too.
// The page size is the documented maximum (https://developers.wrike.com/api/v4/timelogs/),
// so the window is usually one request.
func pullMyTimelogs(ctx context.Context, c Client, st *store.Store, meID string, now time.Time) (bool, error) {
	from, to := timelogWindow(now)
	p := wrike.TimelogParams{Me: true, TrackedFrom: from, TrackedTo: to, PageSize: 1000}
	var all []wrike.Timelog
	for {
		page, err := c.Timelogs(ctx, p)
		if err != nil {
			return false, err
		}
		all = append(all, page.Timelogs...)
		if page.NextPageToken == "" {
			break
		}
		p.PageToken = page.NextPageToken
	}
	return st.Timelogs().ReplaceForUserRange(ctx, meID, from, to, timelogsFromWrike(all))
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
