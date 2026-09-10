package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TimelogWindow is the range of the current user's time entries the store keeps:
// the current week plus the eight before it, Monday to Sunday.
// The syncer pulls this range and the demo seeds it, older weeks are not shown in the timesheet.
func TimelogWindow(now time.Time) (from, to string) {
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(wd - 1))
	return monday.AddDate(0, 0, -7*8).Format("2006-01-02"), monday.AddDate(0, 0, 6).Format("2006-01-02")
}

type TimelogRepo interface {
	Upsert(ctx context.Context, logs []Timelog) error
	ReplaceForTask(ctx context.Context, taskID string, logs []Timelog) error
	ListForTask(ctx context.Context, taskID string) ([]Timelog, error)
	Get(ctx context.Context, id string) (Timelog, error)
	ListForUser(ctx context.Context, userID, from, to string) ([]Timelog, error)
	ReplaceForUserRange(ctx context.Context, userID, from, to string, logs []Timelog) (bool, error)
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

// Upsert is a plain upsert by id, not scoped to a single task or user.
// The demo seed and the tests are its only callers.
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

// timelogColumns is shared by the list queries and Get so they cannot drift apart.
const timelogColumns = `id, task_id, user_id, category_id, tracked_date, comment, hours,
	lock_status, approval_status, created_date, updated_date`

func scanTimelog(row interface{ Scan(...any) error }) (Timelog, error) {
	var l Timelog
	err := row.Scan(&l.ID, &l.TaskID, &l.UserID, &l.CategoryID, &l.TrackedDate,
		&l.Comment, &l.Hours, &l.LockStatus, &l.ApprovalStatus, &l.CreatedDate,
		&l.UpdatedDate)
	return l, err
}

func scanTimelogs(rows *sql.Rows) ([]Timelog, error) {
	var out []Timelog
	for rows.Next() {
		l, err := scanTimelog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (t timelogRepo) ListForTask(ctx context.Context, taskID string) ([]Timelog, error) {
	rows, err := t.r.QueryContext(ctx, `
		SELECT `+timelogColumns+` FROM timelogs
		WHERE task_id = ? ORDER BY created_date, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanTimelogs(rows)
}

func (t timelogRepo) ListForUser(ctx context.Context, userID, from, to string) ([]Timelog, error) {
	rows, err := t.r.QueryContext(ctx, `
		SELECT `+timelogColumns+` FROM timelogs
		WHERE user_id = ? AND tracked_date >= ? AND tracked_date <= ?
		ORDER BY tracked_date, created_date, id`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanTimelogs(rows)
}

func (t timelogRepo) Get(ctx context.Context, id string) (Timelog, error) {
	l, err := scanTimelog(t.r.QueryRowContext(ctx,
		`SELECT `+timelogColumns+` FROM timelogs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Timelog{}, ErrNotFound
	}
	if err != nil {
		return Timelog{}, err
	}
	return l, nil
}

// ReplaceForUserRange swaps the server rows in a date window and says whether anything differs afterwards.
// The fingerprint is cheap on purpose: count, latest update and total hours catch every edit the API can make.
// Only a queued create is protected by the local id prefix, the same rule ReplaceForTask follows.
// A row with a queued update or delete has no such protection,
// so the grid can show the server value until that write lands.
// The write itself is never lost, only its display lags.
func (t timelogRepo) ReplaceForUserRange(ctx context.Context, userID, from, to string, logs []Timelog) (bool, error) {
	tx, err := t.w.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	fingerprint := func() (string, error) {
		var n int
		var latest string
		var hours float64
		err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(MAX(updated_date), ''), COALESCE(SUM(hours), 0) FROM timelogs
			WHERE user_id = ? AND tracked_date >= ? AND tracked_date <= ? AND id NOT LIKE ?`,
			userID, from, to, LocalIDPrefix+"%").Scan(&n, &latest, &hours)
		return fmt.Sprintf("%d|%s|%.4f", n, latest, hours), err
	}
	before, err := fingerprint()
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM timelogs WHERE user_id = ? AND tracked_date >= ? AND tracked_date <= ? AND id NOT LIKE ?`,
		userID, from, to, LocalIDPrefix+"%"); err != nil {
		return false, err
	}
	if err := upsertTimelogsTx(ctx, tx, logs); err != nil {
		return false, err
	}
	after, err := fingerprint()
	if err != nil {
		return false, err
	}
	return before != after, tx.Commit()
}
