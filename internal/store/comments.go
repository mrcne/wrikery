package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// InboxDays is the inbox's window, a constant and no config key, like the history days of the changes screen.
const InboxDays = 30

// InboxLimit caps the whole list of the inbox, on the screen and from the inbox command:
// a month of comments on a busy account is more than anyone reads in a list.
const InboxLimit = 200

// InboxSince is the start of the inbox's window as the comments store their creation date.
func InboxSince(now time.Time) string {
	return now.AddDate(0, 0, -InboxDays).UTC().Format(time.RFC3339)
}

// InboxRow is one comment of the inbox with what the screen draws next to it.
type InboxRow struct {
	Comment
	TaskTitle string
	Mentioned bool // the comment mentions the user, by id or through @assignees on a task the user is responsible for
}

// Unread says whether the row arrived after the seen marker, the rule InboxUnread counts by.
func (r InboxRow) Unread(seenTo string) bool { return r.ArrivedAt > seenTo }

type CommentRepo interface {
	// ReplaceForTask writes a thread as Wrike answered it: the rows upserted, the ones Wrike no longer lists deleted, the optimistic rows kept.
	ReplaceForTask(ctx context.Context, taskID string, comments []Comment) error
	// UpsertCached writes the comments whose task the cache has, skips the rest and counts the rows that changed.
	UpsertCached(ctx context.Context, comments []Comment) (int, error)
	ListForTask(ctx context.Context, taskID string) ([]Comment, error)
	// Inbox lists the comments addressed to the user since the given date, newest first, up to limit:
	// the comments of other people on the tasks the user follows, and the ones that mention the user.
	Inbox(ctx context.Context, meID, since string, limit int) ([]InboxRow, error)
	// InboxUnread counts the rows Inbox would list that arrived after seenTo, every one when seenTo is empty.
	InboxUnread(ctx context.Context, meID, since, seenTo string) (int, error)
	// InboxList is the inbox as the screen and the inbox command list it, with the seen marker its rows are judged against.
	// The whole list is the newest InboxLimit rows of the window, or limit of them when that is fewer.
	// With unread it is every row that arrived after the marker, the set InboxUnread counts, newest first, cut only by a limit above zero.
	InboxList(ctx context.Context, meID string, now time.Time, unread bool, limit int) (rows []InboxRow, seenTo string, err error)
	// MarkInboxSeen moves the seen marker to at, never back: a second interface on the cache with an older list must not star rows again.
	MarkInboxSeen(ctx context.Context, at string) error
}

func (s *Store) Comments() CommentRepo { return commentRepo{w: s.writer, r: s.reader, now: s.Now} }

type commentRepo struct {
	w, r *sql.DB
	now  func() time.Time
}

func joinMentions(m []string) string { return strings.Join(m, ",") }

func splitMentions(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// upsertCommentTx writes one row, the arrival stamp set once and kept by every later write, the rest replaced.
// The SELECT form keeps the foreign key quiet for a task the cache lacks, the account wide read answers comments on every task.
// The WHERE on the update keeps a row the overlap of two reads sends again from counting as a change,
// or the interface would read the detail again after every poll.
func upsertCommentTx(ctx context.Context, tx *sql.Tx, cm Comment, arrived string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO comments (id, task_id, author_id, text, created_date, mentions, arrived_at)
		SELECT ?, ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM tasks WHERE id = ?)
		ON CONFLICT(id) DO UPDATE SET author_id = excluded.author_id,
			text = excluded.text, created_date = excluded.created_date, mentions = excluded.mentions
		WHERE excluded.author_id IS NOT author_id OR excluded.text IS NOT text
			OR excluded.created_date IS NOT created_date OR excluded.mentions IS NOT mentions`,
		cm.ID, cm.TaskID, cm.AuthorID, cm.Text, cm.CreatedDate, joinMentions(cm.Mentions), arrived, cm.TaskID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (c commentRepo) ReplaceForTask(ctx context.Context, taskID string, comments []Comment) error {
	tx, err := c.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	arrived := c.now().UTC().Format(time.RFC3339)
	args := []any{taskID, LocalIDPrefix + "%"}
	for _, cm := range comments {
		cm.TaskID = taskID
		if _, err := upsertCommentTx(ctx, tx, cm, arrived); err != nil {
			return err
		}
		args = append(args, cm.ID)
	}
	// The rows Wrike no longer lists go, the optimistic rows stay, they vanish when their outbox row completes.
	keep := ""
	if len(comments) > 0 {
		keep = ` AND id NOT IN (?` + strings.Repeat(", ?", len(comments)-1) + `)`
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE task_id = ? AND id NOT LIKE ?`+keep, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func (c commentRepo) UpsertCached(ctx context.Context, comments []Comment) (int, error) {
	tx, err := c.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	arrived := c.now().UTC().Format(time.RFC3339)
	changed := 0
	for _, cm := range comments {
		n, err := upsertCommentTx(ctx, tx, cm, arrived)
		if err != nil {
			return 0, err
		}
		changed += int(n)
	}
	return changed, tx.Commit()
}

func (c commentRepo) ListForTask(ctx context.Context, taskID string) ([]Comment, error) {
	rows, err := c.r.QueryContext(ctx, `
		SELECT id, task_id, author_id, text, created_date, mentions, arrived_at FROM comments
		WHERE task_id = ? ORDER BY created_date, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Comment
	for rows.Next() {
		var cm Comment
		var mentions string
		if err := rows.Scan(&cm.ID, &cm.TaskID, &cm.AuthorID, &cm.Text, &cm.CreatedDate, &mentions, &cm.ArrivedAt); err != nil {
			return nil, err
		}
		cm.Mentions = splitMentions(mentions)
		out = append(out, cm)
	}
	return out, rows.Err()
}

// inboxMentioned says the comment mentions the user: by id, through a group the user is a member of, named as the comment showed it,
// or through @assignees on a task the user is responsible for.
// @followers adds nothing, a follower reads every comment of the task through the follow flag.
// The list is matched with its separators on both sides, so an id never matches inside another.
const inboxMentioned = `(instr(',' || c.mentions || ',', ',' || ?1 || ',') > 0
	OR EXISTS (SELECT 1 FROM group_members gm JOIN contacts g ON g.id = gm.group_id
		WHERE gm.contact_id = ?1 AND instr(',' || c.mentions || ',', ',` + GroupMentionPrefix + `' || g.first_name || ',') > 0)
	OR (instr(',' || c.mentions || ',', ',@assignees,') > 0
		AND EXISTS (SELECT 1 FROM task_responsibles r WHERE r.task_id = t.id AND r.contact_id = ?1)))`

// inboxWhere is the set of the inbox, bound by the window the index on created_date serves, ?1 the user, ?2 the window's start.
// An optimistic row of a queued comment is the user's own, the author test leaves it out with the rest.
const inboxWhere = `c.author_id != ?1 AND c.created_date >= ?2
	AND (t.followed_by_me = 1 OR ` + inboxMentioned + `)`

// Without a user nothing is addressed to anyone: an empty id would match the empty mention list of every comment.
func (c commentRepo) Inbox(ctx context.Context, meID, since string, limit int) ([]InboxRow, error) {
	if meID == "" {
		return nil, nil
	}
	return c.inboxRows(ctx, "", "", meID, since, limit)
}

func (c commentRepo) InboxList(ctx context.Context, meID string, now time.Time, unread bool, limit int) ([]InboxRow, string, error) {
	if meID == "" {
		return nil, "", nil
	}
	var seenTo string
	err := c.r.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, MetaKeyInboxSeenTo).Scan(&seenTo)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}
	since := InboxSince(now)
	if !unread {
		if limit <= 0 || limit > InboxLimit {
			limit = InboxLimit
		}
		rows, err := c.Inbox(ctx, meID, since, limit)
		return rows, seenTo, err
	}
	// The unread rows are found by their arrival, the walk of the count, and not among the newest rows by creation date:
	// an older comment of a task cached late is unread and far down that list, past its cap on a busy account.
	// A negative limit is none in SQLite.
	if limit <= 0 {
		limit = -1
	}
	rows, err := c.inboxRows(ctx, "INDEXED BY idx_comments_arrived", " AND c.arrived_at > ?4", meID, since, limit, max(seenTo, since))
	return rows, seenTo, err
}

// inboxRows runs the list query, ?1 the user, ?2 the window's start, ?3 the limit, with an index to walk and a condition more when given.
func (c commentRepo) inboxRows(ctx context.Context, index, and string, args ...any) ([]InboxRow, error) {
	rows, err := c.r.QueryContext(ctx, `
		SELECT c.id, c.task_id, c.author_id, c.text, c.created_date, c.mentions, c.arrived_at, t.title, `+inboxMentioned+`
		FROM comments c `+index+` JOIN tasks t ON t.id = c.task_id
		WHERE `+inboxWhere+and+`
		ORDER BY c.created_date DESC, c.id DESC LIMIT ?3`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []InboxRow
	for rows.Next() {
		var r InboxRow
		var mentions string
		if err := rows.Scan(&r.ID, &r.TaskID, &r.AuthorID, &r.Text, &r.CreatedDate, &mentions, &r.ArrivedAt, &r.TaskTitle, &r.Mentioned); err != nil {
			return nil, err
		}
		r.Mentions = splitMentions(mentions)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (c commentRepo) InboxUnread(ctx context.Context, meID, since, seenTo string) (int, error) {
	if meID == "" {
		return 0, nil
	}
	// The count walks the arrival index from the later of the marker and the window's start, a comment arrives after it was written,
	// so a row of the window cannot have arrived before the window began, and with a recent marker the walk covers the few rows since.
	// The creation index would read the whole window on every sync cycle, a month of comments, and SQLite without statistics
	// cannot tell the two ranges apart, so the index is named.
	var n int
	err := c.r.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM comments c INDEXED BY idx_comments_arrived JOIN tasks t ON t.id = c.task_id
		WHERE `+inboxWhere+` AND c.arrived_at > ?3`, meID, since, max(seenTo, since)).Scan(&n)
	return n, err
}

func (c commentRepo) MarkInboxSeen(ctx context.Context, at string) error {
	_, err := c.w.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = MAX(value, excluded.value)`, MetaKeyInboxSeenTo, at)
	return err
}
