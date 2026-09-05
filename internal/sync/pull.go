package sync

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
	if sc.Kind == store.ScopeKindMe {
		p.Responsibles = []string{meID}
	} else {
		p.FolderID = sc.ID
		p.Descendants = true
	}
	return p
}

// pullScope walks one scope through the updatedDate filter.
// The cursor travels with the final page only,
// so a crash between pages re-pulls from the old cursor and the upserts stay idempotent.
func pullScope(ctx context.Context, c Client, st *store.Store, sc store.Scope, meID string) error {
	p := scopeParams(sc, meID)
	p.Fields = pullFields
	if sc.Cursor != "" {
		after, err := time.Parse(time.RFC3339, sc.Cursor)
		if err != nil {
			return fmt.Errorf("sync: scope %s cursor: %w", sc.ID, err)
		}
		p.UpdatedAfter = after
	}
	maxSeen := sc.Cursor
	for {
		page, err := c.Tasks(ctx, p)
		if err != nil {
			return err
		}
		for _, t := range page.Tasks {
			if u := rfc3339(t.UpdatedDate); u > maxSeen {
				maxSeen = u
			}
		}
		cursor := ""
		if page.NextPageToken == "" {
			cursor = maxSeen
			if cursor == "" {
				// An empty scope on the initial pull has nothing to date the cursor with, start incremental polling from now.
				cursor = time.Now().UTC().Format(time.RFC3339)
			}
		}
		if err := st.Tasks().ApplyPage(ctx, sc.ID, tasksFromWrike(page.Tasks), cursor); err != nil {
			return err
		}
		if page.NextPageToken == "" {
			return nil
		}
		p.PageToken = page.NextPageToken
	}
}

// sweep deletes tasks that vanished from every followed scope.
// It only ever prunes with the complete union, a partial one would delete live tasks and cascade their comments.
func sweep(ctx context.Context, c Client, st *store.Store, scopes []store.Scope, meID string) error {
	var keep []string
	for _, sc := range scopes {
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

// refreshThreads pulls comments and timelogs for recently opened tasks.
// A 404 means the task is gone on the server, drop it. Any other rejection skips the task,
// it stays in the window for days and must not block the other threads for that long.
func refreshThreads(ctx context.Context, c Client, st *store.Store, log *slog.Logger, window time.Duration, limit int) error {
	since := rfc3339(time.Now().Add(-window))
	ids, err := st.Tasks().RecentlyOpenedIDs(ctx, since, limit)
	if err != nil {
		return err
	}
	for _, id := range ids {
		err := refreshThread(ctx, c, st, id)
		switch {
		case err == nil:
		case isNotFound(err):
			if err := st.Tasks().Delete(ctx, id); err != nil {
				return err
			}
		case classify(err) == failPermanent:
			log.Warn("thread refresh rejected", "task", id, "error", err)
		default:
			return err
		}
	}
	return nil
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
