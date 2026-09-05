package store

import (
	"context"
	"database/sql"
)

type TimelogRepo interface {
	Upsert(ctx context.Context, logs []Timelog) error
	ReplaceForTask(ctx context.Context, taskID string, logs []Timelog) error
	ListForTask(ctx context.Context, taskID string) ([]Timelog, error)
}

func (s *Store) Timelogs() TimelogRepo { return timelogRepo{w: s.writer, r: s.reader} }

type timelogRepo struct {
	w, r *sql.DB
}

func upsertTimelogsTx(ctx context.Context, tx *sql.Tx, logs []Timelog) error {
	for _, l := range logs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO timelogs (id, task_id, user_id, category_id, tracked_date, comment,
				hours, lock_status, approval_status, created_date, updated_date)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET task_id = excluded.task_id, user_id = excluded.user_id,
				category_id = excluded.category_id, tracked_date = excluded.tracked_date,
				comment = excluded.comment, hours = excluded.hours,
				lock_status = excluded.lock_status, approval_status = excluded.approval_status,
				created_date = excluded.created_date, updated_date = excluded.updated_date`,
			l.ID, l.TaskID, l.UserID, l.CategoryID, l.TrackedDate, l.Comment,
			l.Hours, l.LockStatus, l.ApprovalStatus, l.CreatedDate, l.UpdatedDate); err != nil {
			return err
		}
	}
	return nil
}

// Upsert is a plain upsert by id, used by the account wide me pull which is
// not scoped to a single task.
func (t timelogRepo) Upsert(ctx context.Context, logs []Timelog) error {
	tx, err := t.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertTimelogsTx(ctx, tx, logs); err != nil {
		return err
	}
	return tx.Commit()
}

func (t timelogRepo) ReplaceForTask(ctx context.Context, taskID string, logs []Timelog) error {
	tx, err := t.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Keep optimistic rows, they vanish when their outbox row completes.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM timelogs WHERE task_id = ? AND id NOT LIKE ?`,
		taskID, LocalIDPrefix+"%"); err != nil {
		return err
	}
	if err := upsertTimelogsTx(ctx, tx, logs); err != nil {
		return err
	}
	return tx.Commit()
}

func (t timelogRepo) ListForTask(ctx context.Context, taskID string) ([]Timelog, error) {
	rows, err := t.r.QueryContext(ctx, `
		SELECT id, task_id, user_id, category_id, tracked_date, comment, hours,
			lock_status, approval_status, created_date, updated_date FROM timelogs
		WHERE task_id = ? ORDER BY created_date, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Timelog
	for rows.Next() {
		var l Timelog
		if err := rows.Scan(&l.ID, &l.TaskID, &l.UserID, &l.CategoryID, &l.TrackedDate,
			&l.Comment, &l.Hours, &l.LockStatus, &l.ApprovalStatus, &l.CreatedDate,
			&l.UpdatedDate); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
