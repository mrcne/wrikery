package store

import (
	"context"
	"database/sql"
)

type CommentRepo interface {
	ReplaceForTask(ctx context.Context, taskID string, comments []Comment) error
	// UpsertCached writes the comments whose task the cache has, skips the rest and counts the rows that changed.
	UpsertCached(ctx context.Context, comments []Comment) (int, error)
	ListForTask(ctx context.Context, taskID string) ([]Comment, error)
}

func (s *Store) Comments() CommentRepo { return commentRepo{w: s.writer, r: s.reader} }

type commentRepo struct {
	w, r *sql.DB
}

func (c commentRepo) ReplaceForTask(ctx context.Context, taskID string, comments []Comment) error {
	tx, err := c.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Keep optimistic rows, they vanish when their outbox row completes.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM comments WHERE task_id = ? AND id NOT LIKE ?`,
		taskID, LocalIDPrefix+"%"); err != nil {
		return err
	}
	for _, cm := range comments {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO comments (id, task_id, author_id, text, created_date)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET author_id = excluded.author_id,
				text = excluded.text, created_date = excluded.created_date`,
			cm.ID, taskID, cm.AuthorID, cm.Text, cm.CreatedDate); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// The SELECT form keeps the foreign key quiet for a task the cache lacks, the account wide read answers comments on every task.
// The WHERE on the update keeps a row the overlap of two reads sends again from counting as a change,
// or the interface would read the detail again after every poll.
func (c commentRepo) UpsertCached(ctx context.Context, comments []Comment) (int, error) {
	tx, err := c.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	changed := 0
	for _, cm := range comments {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO comments (id, task_id, author_id, text, created_date)
			SELECT ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM tasks WHERE id = ?)
			ON CONFLICT(id) DO UPDATE SET author_id = excluded.author_id,
				text = excluded.text, created_date = excluded.created_date
			WHERE excluded.author_id IS NOT author_id OR excluded.text IS NOT text OR excluded.created_date IS NOT created_date`,
			cm.ID, cm.TaskID, cm.AuthorID, cm.Text, cm.CreatedDate, cm.TaskID)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		changed += int(n)
	}
	return changed, tx.Commit()
}

func (c commentRepo) ListForTask(ctx context.Context, taskID string) ([]Comment, error) {
	rows, err := c.r.QueryContext(ctx, `
		SELECT id, task_id, author_id, text, created_date FROM comments
		WHERE task_id = ? ORDER BY created_date, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Comment
	for rows.Next() {
		var cm Comment
		if err := rows.Scan(&cm.ID, &cm.TaskID, &cm.AuthorID, &cm.Text, &cm.CreatedDate); err != nil {
			return nil, err
		}
		out = append(out, cm)
	}
	return out, rows.Err()
}
