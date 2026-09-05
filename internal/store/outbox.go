package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type OutboxKind string

const (
	KindTaskUpdate    OutboxKind = "task_update"
	KindCommentCreate OutboxKind = "comment_create"
	KindTimelogCreate OutboxKind = "timelog_create"
	KindTimelogUpdate OutboxKind = "timelog_update"
	KindTimelogDelete OutboxKind = "timelog_delete"
)

type OutboxState string

const (
	StatePending  OutboxState = "pending"
	StateInflight OutboxState = "inflight"
	StateFailed   OutboxState = "failed"
)

// LocalIDPrefix marks optimistic cache rows an enqueue created before the server confirmed them.
// The suffix is the outbox row id.
const LocalIDPrefix = "local:"

type OutboxRow struct {
	ID            int64
	Kind          OutboxKind
	EntityID      string
	Payload       []byte
	State         OutboxState
	Attempts      int
	LastError     string
	NextAttemptAt string
	CreatedAt     string
}

// Payloads mirror the params of the client call each kind replays.
// internal/sync decodes them, the JSON never leaves the app.
type TaskUpdatePayload struct {
	Title              string     `json:"title,omitempty"`
	CustomStatusID     string     `json:"customStatusId,omitempty"`
	AddResponsibles    []string   `json:"addResponsibles,omitempty"`
	RemoveResponsibles []string   `json:"removeResponsibles,omitempty"`
	Dates              *TaskDates `json:"dates,omitempty"`
}

type CommentCreatePayload struct {
	Text string `json:"text"`
}

type TimelogCreatePayload struct {
	Hours       float64 `json:"hours"`
	TrackedDate string  `json:"trackedDate"`
	Comment     string  `json:"comment,omitempty"`
}

type TimelogUpdatePayload struct {
	Hours       float64 `json:"hours,omitempty"`
	TrackedDate string  `json:"trackedDate,omitempty"`
	Comment     string  `json:"comment,omitempty"`
}

type OutboxRepo interface {
	EnqueueTaskUpdate(ctx context.Context, taskID string, p TaskUpdatePayload) (int64, error)
	EnqueueComment(ctx context.Context, taskID, authorID, text string) (int64, error)
	EnqueueTimelogCreate(ctx context.Context, taskID, userID string, p TimelogCreatePayload) (int64, error)
	EnqueueTimelogUpdate(ctx context.Context, timelogID string, p TimelogUpdatePayload) (int64, error)
	EnqueueTimelogDelete(ctx context.Context, timelogID string) (int64, error)
	Counts(ctx context.Context) (pending, failed int, err error)
}

func (s *Store) Outbox() OutboxRepo { return outboxRepo{w: s.writer, r: s.reader} }

type outboxRepo struct {
	w, r *sql.DB
}

const nowUTC = `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`

func insertOutboxTx(ctx context.Context, tx *sql.Tx, kind OutboxKind, entityID string, payload any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO outbox (kind, entity_id, payload) VALUES (?, ?, ?)`,
		string(kind), entityID, string(raw))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (o outboxRepo) EnqueueTaskUpdate(ctx context.Context, taskID string, p TaskUpdatePayload) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertOutboxTx(ctx, tx, KindTaskUpdate, taskID, p)
	if err != nil {
		return 0, err
	}
	var set []string
	var args []any
	if p.Title != "" {
		set = append(set, "title = ?")
		args = append(args, p.Title)
	}
	if p.CustomStatusID != "" {
		set = append(set, "custom_status_id = ?")
		args = append(args, p.CustomStatusID)
	}
	if p.Dates != nil {
		set = append(set, "dates_type = ?", "dates_duration = ?", "dates_start = ?", "dates_due = ?")
		args = append(args, p.Dates.Type, p.Dates.Duration, p.Dates.Start, p.Dates.Due)
	}
	if len(set) > 0 {
		args = append(args, taskID)
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET `+strings.Join(set, ", ")+` WHERE id = ?`, args...); err != nil {
			return 0, err
		}
	}
	for _, c := range p.AddResponsibles {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO task_responsibles (task_id, contact_id) VALUES (?, ?)`,
			taskID, c); err != nil {
			return 0, err
		}
	}
	for _, c := range p.RemoveResponsibles {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM task_responsibles WHERE task_id = ? AND contact_id = ?`,
			taskID, c); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (o outboxRepo) EnqueueComment(ctx context.Context, taskID, authorID, text string) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertOutboxTx(ctx, tx, KindCommentCreate, taskID, CommentCreatePayload{Text: text})
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO comments (id, task_id, author_id, text, created_date)
		VALUES (?, ?, ?, ?, `+nowUTC+`)`,
		fmt.Sprintf("%s%d", LocalIDPrefix, id), taskID, authorID, text); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (o outboxRepo) EnqueueTimelogCreate(ctx context.Context, taskID, userID string, p TimelogCreatePayload) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertOutboxTx(ctx, tx, KindTimelogCreate, taskID, p)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO timelogs (id, task_id, user_id, tracked_date, comment, hours,
			created_date, updated_date)
		VALUES (?, ?, ?, ?, ?, ?, `+nowUTC+`, `+nowUTC+`)`,
		fmt.Sprintf("%s%d", LocalIDPrefix, id), taskID, userID,
		p.TrackedDate, p.Comment, p.Hours); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (o outboxRepo) EnqueueTimelogUpdate(ctx context.Context, timelogID string, p TimelogUpdatePayload) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertOutboxTx(ctx, tx, KindTimelogUpdate, timelogID, p)
	if err != nil {
		return 0, err
	}
	var set []string
	var args []any
	if p.Hours > 0 {
		set = append(set, "hours = ?")
		args = append(args, p.Hours)
	}
	if p.TrackedDate != "" {
		set = append(set, "tracked_date = ?")
		args = append(args, p.TrackedDate)
	}
	if p.Comment != "" {
		set = append(set, "comment = ?")
		args = append(args, p.Comment)
	}
	set = append(set, "updated_date = "+nowUTC)
	args = append(args, timelogID)
	if _, err := tx.ExecContext(ctx,
		`UPDATE timelogs SET `+strings.Join(set, ", ")+` WHERE id = ?`, args...); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (o outboxRepo) EnqueueTimelogDelete(ctx context.Context, timelogID string) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertOutboxTx(ctx, tx, KindTimelogDelete, timelogID, struct{}{})
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM timelogs WHERE id = ?`, timelogID); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Counts feeds the status bar.
// Inflight rows count as pending, the user cares whether the write has landed, not which internal state it is in.
func (o outboxRepo) Counts(ctx context.Context) (pending, failed int, err error) {
	err = o.r.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE state IN ('pending', 'inflight')),
			COUNT(*) FILTER (WHERE state = 'failed')
		FROM outbox`).Scan(&pending, &failed)
	return pending, failed, err
}
