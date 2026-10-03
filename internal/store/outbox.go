package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type OutboxKind string

const (
	KindTaskCreate    OutboxKind = "task_create"
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

// LocalID is the cache id of the row an outbox create made, from the outbox row's id.
func LocalID(id int64) string { return fmt.Sprintf("%s%d", LocalIDPrefix, id) }

// IsLocalID tells a row an outbox create made from one Wrike has confirmed.
func IsLocalID(id string) bool { return strings.HasPrefix(id, LocalIDPrefix) }

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
// internal/syncer decodes them, the JSON never leaves the app.
type TaskUpdatePayload struct {
	Title              string     `json:"title,omitempty"`
	Description        string     `json:"description,omitempty"`
	CustomStatusID     string     `json:"customStatusId,omitempty"`
	Importance         string     `json:"importance,omitempty"`
	AddResponsibles    []string   `json:"addResponsibles,omitempty"`
	RemoveResponsibles []string   `json:"removeResponsibles,omitempty"`
	AddParents         []string   `json:"addParents,omitempty"`
	RemoveParents      []string   `json:"removeParents,omitempty"`
	Dates              *TaskDates `json:"dates,omitempty"`
	// Status is the custom status's group.
	// It is applied to the cache row so the list sorts and filters the task as done right away.
	// The drain does not send it, Wrike derives the group from the custom status id, and rejects
	// an update that carries a group name of its own.
	Status string `json:"status,omitempty"`
}

// TaskCreatePayload mirrors CreateTask. The folder is the row's entity id.
// Responsibles holds the user's own id for a create from My tasks, so the server copy stays in that view after the swap.
type TaskCreatePayload struct {
	Title        string   `json:"title"`
	Responsibles []string `json:"responsibles,omitempty"`
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
	EnqueueTaskCreate(ctx context.Context, folderID string, p TaskCreatePayload, localStatusID string) (int64, error)
	CompleteTaskCreate(ctx context.Context, id int64, real Task) error
	EnqueueTaskUpdate(ctx context.Context, taskID string, p TaskUpdatePayload) (int64, error)
	EnqueueComment(ctx context.Context, taskID, authorID, text string) (int64, error)
	EnqueueTimelogCreate(ctx context.Context, taskID, userID string, p TimelogCreatePayload) (int64, error)
	EnqueueTimelogUpdate(ctx context.Context, timelogID string, p TimelogUpdatePayload) (int64, error)
	EnqueueTimelogDelete(ctx context.Context, timelogID string) (int64, error)
	Counts(ctx context.Context) (pending, failed int, err error)
	NextDue(ctx context.Context, now string) (OutboxRow, error)
	MarkInflight(ctx context.Context, id int64) error
	Complete(ctx context.Context, id int64) error
	CompleteTask(ctx context.Context, id int64, real Task) error
	CompleteComment(ctx context.Context, id int64, real Comment) error
	CompleteTimelog(ctx context.Context, id int64, real Timelog) error
	Reschedule(ctx context.Context, id int64, errText, nextAttemptAt string) error
	Fail(ctx context.Context, id int64, errText string) error
	Retry(ctx context.Context, id int64) error
	Discard(ctx context.Context, id int64) error
	ListFailed(ctx context.Context) ([]OutboxRow, error)
	ResetInflight(ctx context.Context) (int64, error)
	StatesByEntity(ctx context.Context) (map[string]OutboxState, error)
	RealID(ctx context.Context, localID string) (string, error)
}

func (s *Store) Outbox() OutboxRepo { return outboxRepo{w: s.writer, r: s.reader, now: s.Now} }

type outboxRepo struct {
	w, r *sql.DB
	now  func() time.Time
}

func (o outboxRepo) stamp() string { return o.now().UTC().Format(time.RFC3339) }

func (o outboxRepo) insertOutboxTx(ctx context.Context, tx *sql.Tx, kind OutboxKind, entityID string, payload any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO outbox (kind, entity_id, payload, created_at) VALUES (?, ?, ?, ?)`,
		string(kind), entityID, string(raw), o.stamp())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// resolveTx turns a local target into the real id when its create has landed since the caller read it.
// A dialog takes the task id when it opens and the create is often sent and swapped while the user types,
// so a write against the old id would name a row that no longer exists.
// A local id with no swap and no row behind it belongs to a discarded create, the write is refused.
func resolveTx(ctx context.Context, tx *sql.Tx, table, id string) (string, error) {
	if !IsLocalID(id) {
		return id, nil
	}
	var real string
	err := tx.QueryRowContext(ctx, `SELECT real_id FROM local_ids WHERE local_id = ?`, id).Scan(&real)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var one int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func recordSwapTx(ctx context.Context, tx *sql.Tx, localID, realID string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO local_ids (local_id, real_id) VALUES (?, ?)`, localID, realID)
	return err
}

// RealID is the server id a local one was swapped for, ErrNotFound while the create is still queued.
func (o outboxRepo) RealID(ctx context.Context, localID string) (string, error) {
	var real string
	err := o.r.QueryRowContext(ctx, `SELECT real_id FROM local_ids WHERE local_id = ?`, localID).Scan(&real)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return real, err
}

func (o outboxRepo) EnqueueTaskUpdate(ctx context.Context, taskID string, p TaskUpdatePayload) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if taskID, err = resolveTx(ctx, tx, "tasks", taskID); err != nil {
		return 0, err
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTaskUpdate, taskID, p)
	if err != nil {
		return 0, err
	}
	var set []string
	var args []any
	if p.Title != "" {
		set = append(set, "title = ?")
		args = append(args, p.Title)
	}
	if p.Description != "" {
		set = append(set, "description = ?", "description_plain = ?")
		args = append(args, p.Description, stripHTML(p.Description))
	}
	if p.CustomStatusID != "" {
		set = append(set, "custom_status_id = ?")
		args = append(args, p.CustomStatusID)
	}
	if p.Importance != "" {
		set = append(set, "importance = ?")
		args = append(args, p.Importance)
	}
	if p.Status != "" {
		// See the Status field's doc comment on TaskUpdatePayload for why this is local only.
		set = append(set, "status = ?")
		args = append(args, p.Status)
	}
	if p.Dates != nil {
		// A Backlog write carries no start or due, and an empty string would sort ahead of every real date,
		// the same reason the task upsert path in tasks.go stores them as NULL.
		set = append(set, "dates_type = ?", "dates_duration = ?", "dates_start = ?", "dates_due = ?")
		args = append(args, p.Dates.Type, p.Dates.Duration, nullIfEmpty(p.Dates.Start), nullIfEmpty(p.Dates.Due))
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
	for _, f := range p.AddParents {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO task_parents (task_id, folder_id) VALUES (?, ?)`,
			taskID, f); err != nil {
			return 0, err
		}
	}
	for _, f := range p.RemoveParents {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM task_parents WHERE task_id = ? AND folder_id = ?`,
			taskID, f); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// EnqueueTaskCreate queues a create in folderID and writes the task under a local id so the list shows it at once.
// localStatusID is a guess for that row only, the first Active status of the workflow in view.
// It is not sent, Wrike picks the folder's own default and the swap brings the real one.
func (o outboxRepo) EnqueueTaskCreate(ctx context.Context, folderID string, p TaskCreatePayload, localStatusID string) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := o.insertOutboxTx(ctx, tx, KindTaskCreate, folderID, p)
	if err != nil {
		return 0, err
	}
	now := o.stamp()
	local := Task{
		ID: LocalID(id), Title: p.Title, Status: "Active", CustomStatusID: localStatusID, Importance: "Normal",
		ResponsibleIDs: p.Responsibles, ParentIDs: []string{folderID},
		CreatedDate: now, UpdatedDate: now,
	}
	if err := upsertTasksTx(ctx, tx, []Task{local}); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (o outboxRepo) EnqueueComment(ctx context.Context, taskID, authorID, text string) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if taskID, err = resolveTx(ctx, tx, "tasks", taskID); err != nil {
		return 0, err
	}
	id, err := o.insertOutboxTx(ctx, tx, KindCommentCreate, taskID, CommentCreatePayload{Text: text})
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO comments (id, task_id, author_id, text, created_date)
		VALUES (?, ?, ?, ?, ?)`,
		LocalID(id), taskID, authorID, text, o.stamp()); err != nil {
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
	if taskID, err = resolveTx(ctx, tx, "tasks", taskID); err != nil {
		return 0, err
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTimelogCreate, taskID, p)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO timelogs (id, task_id, user_id, tracked_date, comment, hours,
			created_date, updated_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		LocalID(id), taskID, userID,
		p.TrackedDate, p.Comment, p.Hours, o.stamp(), o.stamp()); err != nil {
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
	if timelogID, err = resolveTx(ctx, tx, "timelogs", timelogID); err != nil {
		return 0, err
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTimelogUpdate, timelogID, p)
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
	set = append(set, "updated_date = ?")
	args = append(args, o.stamp(), timelogID)
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
	if timelogID, err = resolveTx(ctx, tx, "timelogs", timelogID); err != nil {
		return 0, err
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTimelogDelete, timelogID, struct{}{})
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

const outboxColumns = `id, kind, entity_id, payload, state, attempts, last_error,
	COALESCE(next_attempt_at, ''), created_at`

func scanOutboxRow(row interface{ Scan(...any) error }) (OutboxRow, error) {
	var r OutboxRow
	var kind, state, payload string
	err := row.Scan(&r.ID, &kind, &r.EntityID, &payload, &state, &r.Attempts,
		&r.LastError, &r.NextAttemptAt, &r.CreatedAt)
	if err != nil {
		return OutboxRow{}, err
	}
	r.Kind, r.State, r.Payload = OutboxKind(kind), OutboxState(state), []byte(payload)
	return r, nil
}

func (o outboxRepo) NextDue(ctx context.Context, now string) (OutboxRow, error) {
	// A dependent edit or delete still targeting a local id waits for its create to drain and remap it,
	// ordering by id alone is not enough once the create backs off and the dependent becomes due first.
	r, err := scanOutboxRow(o.r.QueryRowContext(ctx, `
		SELECT `+outboxColumns+` FROM outbox
		WHERE state = 'pending' AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
			AND entity_id NOT LIKE ?
		ORDER BY id LIMIT 1`, now, LocalIDPrefix+"%"))
	if errors.Is(err, sql.ErrNoRows) {
		return OutboxRow{}, ErrNotFound
	}
	return r, err
}

func (o outboxRepo) MarkInflight(ctx context.Context, id int64) error {
	return o.expectOne(ctx,
		`UPDATE outbox SET state = 'inflight' WHERE id = ? AND state = 'pending'`, id)
}

func (o outboxRepo) expectOne(ctx context.Context, query string, args ...any) error {
	res, err := o.w.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (o outboxRepo) Complete(ctx context.Context, id int64) error {
	return o.expectOne(ctx, `DELETE FROM outbox WHERE id = ?`, id)
}

func (o outboxRepo) CompleteTask(ctx context.Context, id int64, real Task) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, id); err != nil {
		return err
	}
	if err := upsertTasksTx(ctx, tx, []Task{real}); err != nil {
		return err
	}
	return tx.Commit()
}

// CompleteTaskCreate swaps the local task for the server's and points everything that named the local id at the real one.
// The server row goes in first and the thread rows move before the local row is deleted,
// the foreign key would otherwise cascade the queued comments away.
func (o outboxRepo) CompleteTaskCreate(ctx context.Context, id int64, real Task) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, id); err != nil {
		return err
	}
	if err := upsertTasksTx(ctx, tx, []Task{real}); err != nil {
		return err
	}
	localID := LocalID(id)
	for _, table := range []string{"comments", "timelogs"} {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET task_id = ? WHERE task_id = ?`, real.ID, localID); err != nil {
			return err
		}
	}
	// The user may be looking at the task, the thread refresh must keep fetching its comments after the swap.
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET last_opened_at = (SELECT last_opened_at FROM tasks WHERE id = ?) WHERE id = ?`, localID, real.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, localID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE outbox SET entity_id = ? WHERE entity_id = ?`, real.ID, localID); err != nil {
		return err
	}
	if err := recordSwapTx(ctx, tx, localID, real.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (o outboxRepo) CompleteComment(ctx context.Context, id int64, real Comment) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`,
		LocalID(id)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO comments (id, task_id, author_id, text, created_date)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET author_id = excluded.author_id,
			text = excluded.text, created_date = excluded.created_date`,
		real.ID, real.TaskID, real.AuthorID, real.Text, real.CreatedDate); err != nil {
		return err
	}
	if err := recordSwapTx(ctx, tx, LocalID(id), real.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (o outboxRepo) CompleteTimelog(ctx context.Context, id int64, real Timelog) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM timelogs WHERE id = ?`, LocalID(id))
	if err != nil {
		return err
	}
	swapped, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO timelogs (id, task_id, user_id, category_id, tracked_date,
			comment, hours, lock_status, approval_status, created_date, updated_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET task_id = excluded.task_id,
			user_id = excluded.user_id, category_id = excluded.category_id,
			tracked_date = excluded.tracked_date, comment = excluded.comment,
			hours = excluded.hours, lock_status = excluded.lock_status,
			approval_status = excluded.approval_status,
			created_date = excluded.created_date, updated_date = excluded.updated_date`,
		real.ID, real.TaskID, real.UserID, real.CategoryID, real.TrackedDate,
		real.Comment, real.Hours, real.LockStatus, real.ApprovalStatus,
		real.CreatedDate, real.UpdatedDate); err != nil {
		return err
	}
	// An edit or delete queued while the create was still pending points at
	// the local id. Point it at the confirmed id so it can drain.
	if _, err := tx.ExecContext(ctx,
		`UPDATE outbox SET entity_id = ? WHERE entity_id = ?`,
		real.ID, LocalID(id)); err != nil {
		return err
	}
	// An update completes through here too and never had a local row, only a create's swap is worth recording.
	if swapped > 0 {
		if err := recordSwapTx(ctx, tx, LocalID(id), real.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (o outboxRepo) Reschedule(ctx context.Context, id int64, errText, nextAttemptAt string) error {
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'pending', attempts = attempts + 1,
			last_error = ?, next_attempt_at = ?
		WHERE id = ?`, errText, nextAttemptAt, id)
}

func (o outboxRepo) Fail(ctx context.Context, id int64, errText string) error {
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'failed', attempts = attempts + 1,
			last_error = ?, next_attempt_at = NULL
		WHERE id = ?`, errText, id)
}

func (o outboxRepo) Retry(ctx context.Context, id int64) error {
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'pending', attempts = 0, last_error = '',
			next_attempt_at = NULL
		WHERE id = ? AND state = 'failed'`, id)
}

// Discard drops a queued write.
// For creates the optimistic cache row goes with it.
// A discarded update leaves the cache ahead of the server until the next pull corrects it, which the spec accepts.
func (o outboxRepo) Discard(ctx context.Context, id int64) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// An inflight row belongs to the engine. API call may already be underway, so only a pending or failed row can be discarded.
	var kind string
	err = tx.QueryRowContext(ctx,
		`SELECT kind FROM outbox WHERE id = ? AND state IN ('pending', 'failed')`, id).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	localID := LocalID(id)
	switch OutboxKind(kind) {
	case KindCommentCreate:
		if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, localID); err != nil {
			return err
		}
	case KindTaskCreate:
		// The comments go with the task through the cascade, the writes queued on it are dropped here,
		// their target never existed on the server.
		// Time entries have no foreign key to tasks, so they are deleted by hand.
		// Edits and deletes queued under such an entry name its local id, which only the queue still knows:
		// a queued delete has already taken the entry's own row out of the timelogs table.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM outbox WHERE entity_id IN (SELECT ? || id FROM outbox WHERE kind = ? AND entity_id = ?)`,
			LocalIDPrefix, string(KindTimelogCreate), localID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM timelogs WHERE task_id = ?`, localID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, localID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE entity_id = ?`, localID); err != nil {
			return err
		}
	case KindTimelogCreate:
		if _, err := tx.ExecContext(ctx, `DELETE FROM timelogs WHERE id = ?`, localID); err != nil {
			return err
		}
		// Edits queued against the create die with it,
		// their target never existed on the server and would sit pending forever otherwise.
		if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE entity_id = ?`, localID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (o outboxRepo) ListFailed(ctx context.Context) ([]OutboxRow, error) {
	rows, err := o.r.QueryContext(ctx,
		`SELECT `+outboxColumns+` FROM outbox WHERE state = 'failed' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OutboxRow
	for rows.Next() {
		r, err := scanOutboxRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (o outboxRepo) ResetInflight(ctx context.Context) (int64, error) {
	res, err := o.w.ExecContext(ctx,
		`UPDATE outbox SET state = 'pending' WHERE state = 'inflight'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// StatesByEntity maps each entity with queued work to pending or failed. Inflight counts as pending, failed wins.
func (o outboxRepo) StatesByEntity(ctx context.Context) (map[string]OutboxState, error) {
	// A create names its folder as the entity, the pending mark belongs to the task it made.
	rows, err := o.r.QueryContext(ctx, `
		SELECT CASE WHEN kind = ? THEN ? || id ELSE entity_id END, state FROM outbox`,
		string(KindTaskCreate), LocalIDPrefix)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]OutboxState{}
	for rows.Next() {
		var id string
		var state OutboxState
		if err := rows.Scan(&id, &state); err != nil {
			return nil, err
		}
		if state == StateInflight {
			state = StatePending
		}
		if out[id] != StateFailed {
			out[id] = state
		}
	}
	return out, rows.Err()
}
