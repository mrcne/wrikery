package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// InboxDays is the inbox's window, a constant and no config key, like the history days of the changes screen.
const InboxDays = 30

// InboxRow is one comment of the inbox with what the screen draws next to it.
type InboxRow struct {
	Comment
	TaskTitle string
	Mentioned bool // the comment mentions the user, by id or through @assignees on a task the user is responsible for
}

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
	rows, err := c.r.QueryContext(ctx, `
		SELECT c.id, c.task_id, c.author_id, c.text, c.created_date, c.mentions, c.arrived_at, t.title, `+inboxMentioned+`
		FROM comments c JOIN tasks t ON t.id = c.task_id
		WHERE `+inboxWhere+`
		ORDER BY c.created_date DESC, c.id DESC LIMIT ?3`, meID, since, limit)
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
	var n int
	err := c.r.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM comments c JOIN tasks t ON t.id = c.task_id
		WHERE `+inboxWhere+` AND c.arrived_at > ?3`, meID, since, seenTo).Scan(&n)
	return n, err
}

func (c commentRepo) MarkInboxSeen(ctx context.Context, at string) error {
	_, err := c.w.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = MAX(value, excluded.value)`, MetaKeyInboxSeenTo, at)
	return err
}
