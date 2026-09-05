package store

import (
	"context"
	"database/sql"
)

type CommentRepo interface {
	ReplaceForTask(ctx context.Context, taskID string, comments []Comment) error
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
