package store

import (
	"cmp"
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

// The queue is pending, inflight and failed, the states the engine claims, sends and retries.
// A row that went out or was dropped stays as sent or discarded for HistoryDays, the changes screen reads it
// and a revert needs the values it kept, then finishTx sweeps it with the next completion.
// Every query that means the queue has to say so, see StatesByEntity and Blocker.
const (
	StatePending   OutboxState = "pending"
	StateInflight  OutboxState = "inflight"
	StateFailed    OutboxState = "failed"
	StateSent      OutboxState = "sent"
	StateDiscarded OutboxState = "discarded"
)

// HistoryDays is how long a sent or discarded row stays in the outbox.
const HistoryDays = 30

// The SQL for the two groups, so a query that means the queue names the same three states everywhere.
const (
	queueStates   = `('pending', 'inflight', 'failed')`
	historyStates = `('sent', 'discarded')`
)

// The front end that queued a write, so the changes screen can tell a command's write from the interface's.
const (
	SourceTUI = "tui"
	SourceCLI = "cli"
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
	Before        []byte // the values the write replaced, in the payload's shape, empty when it replaced nothing
	TaskID        string // the task the write belongs to, the local id of a create until it lands, empty when unknown
	TaskTitle     string // the title at enqueue, a fallback when the task has left the cache
	Source        string // SourceTUI or SourceCLI
	State         OutboxState
	Attempts      int
	LastError     string
	NextAttemptAt string
	CreatedAt     string
	DoneAt        string // when the row became sent or discarded, empty on the queue
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
// Description and Importance come from a command, the box creates with a title alone.
type TaskCreatePayload struct {
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Importance   string   `json:"importance,omitempty"`
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
	EnqueueTaskCreate(ctx context.Context, folderID string, p TaskCreatePayload) (int64, error)
	CompleteTaskCreate(ctx context.Context, id int64, real Task) error
	EnqueueTaskUpdate(ctx context.Context, taskID string, p TaskUpdatePayload) (int64, error)
	EnqueueComment(ctx context.Context, taskID, authorID, text string) (int64, error)
	EnqueueTimelogCreate(ctx context.Context, taskID, userID string, p TimelogCreatePayload) (int64, error)
	EnqueueTimelogUpdate(ctx context.Context, timelogID string, p TimelogUpdatePayload) (int64, error)
	EnqueueTimelogDelete(ctx context.Context, timelogID string) (int64, error)
	Counts(ctx context.Context) (pending, failed int, err error)
	Get(ctx context.Context, id int64) (OutboxRow, error)
	NextDue(ctx context.Context, now string) (OutboxRow, error)
	// Blocker is the older row the given one waits for: the oldest pending or in flight row on the same entity,
	// or the create a row on a local task waits for, while that create is still in the queue.
	// ErrNotFound when the row waits for nothing. A task create waits for nothing and blocks nothing, see NextDue.
	Blocker(ctx context.Context, rowID int64) (OutboxRow, error)
	MarkInflight(ctx context.Context, id int64) error
	Complete(ctx context.Context, id int64) error
	CompleteTask(ctx context.Context, id int64, real Task) error
	CompleteComment(ctx context.Context, id int64, real Comment) error
	CompleteTimelog(ctx context.Context, id int64, real Timelog) error
	Reschedule(ctx context.Context, id int64, errText, nextAttemptAt string) error
	Fail(ctx context.Context, id int64, errText string) error
	// Hold fails a task edit that someone changed on Wrike since it was queued, and puts what Wrike holds now into its before,
	// so the changes screen shows what sending it anyway would do.
	Hold(ctx context.Context, id int64, errText string, before []byte) error
	// Defer puts a row back to pending after Wrike could not be reached, no attempt counted and no wait of its own,
	// the engine's reconnect wait paces the next try.
	Defer(ctx context.Context, id int64, errText string) error
	// Retry makes a failed row pending and a pending row due at once, with a clean attempt count.
	Retry(ctx context.Context, id int64) error
	Discard(ctx context.Context, id int64) error
	// ListChanges is the queue first in the order it sends, then the history of the last HistoryDays by the time it finished, newest first.
	ListChanges(ctx context.Context) ([]OutboxRow, error)
	ResetInflight(ctx context.Context) (int64, error)
	StatesByEntity(ctx context.Context) (map[string]OutboxState, error)
	RealID(ctx context.Context, localID string) (string, error)
}

func (s *Store) Outbox() OutboxRepo {
	return outboxRepo{w: s.writer, r: s.reader, now: s.Now, source: s.Source}
}

type outboxRepo struct {
	w, r   *sql.DB
	now    func() time.Time
	source string
}

func (o outboxRepo) stamp() string { return o.now().UTC().Format(time.RFC3339) }

// insertOutboxTx queues one write.
// before is the payload that would put the replaced values back, nil when the write replaces nothing or the cache had nothing to read.
// Its fields are omitempty like the payload's, so a value that was empty is absent from it:
// the payload's own field set says which absent fields were empty, a revert has to read both.
func (o outboxRepo) insertOutboxTx(ctx context.Context, tx *sql.Tx, kind OutboxKind, entityID, taskID, taskTitle string, payload, before any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var old []byte
	if before != nil {
		if old, err = json.Marshal(before); err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO outbox (kind, entity_id, payload, before, task_id, task_title, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(kind), entityID, string(raw), string(old), taskID, taskTitle, o.source, o.stamp())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// taskTitleTx is the cached title of a task, empty when the cache has no row for it.
func taskTitleTx(ctx context.Context, tx *sql.Tx, taskID string) (string, error) {
	var title string
	err := tx.QueryRowContext(ctx, `SELECT title FROM tasks WHERE id = ?`, taskID).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return title, err
}

// taskBeforeTx reads the values p is about to replace, as the payload that would put them back, and the title for the row.
// The enqueue is the last moment the cache still has them, its own update overwrites them next.
// ok is false for a task the cache does not have, the write is still valid and goes out with nothing remembered.
func taskBeforeTx(ctx context.Context, tx *sql.Tx, taskID string, p TaskUpdatePayload) (before TaskUpdatePayload, title string, ok bool, err error) {
	if IsLocalID(taskID) {
		// The local row's status is a guess and its description this client's own HTML, Wrike normalizes both,
		// so there is nothing true to remember, and the drain would hold the edit against values Wrike never had.
		title, err = taskTitleTx(ctx, tx, taskID)
		return TaskUpdatePayload{}, title, false, err
	}
	var description, statusID, group, importance string
	var dType, dStart, dDue sql.NullString
	var dDur sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT title, description, custom_status_id, status, importance, dates_type, dates_duration, dates_start, dates_due
		FROM tasks WHERE id = ?`, taskID).Scan(&title, &description, &statusID, &group, &importance, &dType, &dDur, &dStart, &dDue)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskUpdatePayload{}, "", false, nil
	}
	if err != nil {
		return TaskUpdatePayload{}, "", false, err
	}
	if p.Title != "" {
		before.Title = title
	}
	if p.Description != "" {
		before.Description = description
	}
	if p.CustomStatusID != "" {
		before.CustomStatusID, before.Status = statusID, group
	}
	if p.Importance != "" {
		before.Importance = importance
	}
	if p.Dates != nil {
		// A task without a dates block comes back to that through a Backlog write, the same write the dates box uses to clear them.
		before.Dates = &TaskDates{Type: "Backlog"}
		if dType.String != "" {
			before.Dates = &TaskDates{Type: dType.String, Duration: int(dDur.Int64), Start: dStart.String, Due: dDue.String}
		}
	}
	// The mirror is built against the link tables and not from the payload alone: a pull between the dialog and its submit
	// can have put an added id on the task already, then the add changes nothing on Wrike and putting it back must not remove it.
	responsibles, err := idSetTx(ctx, tx, `SELECT contact_id FROM task_responsibles WHERE task_id = ?`, taskID)
	if err != nil {
		return TaskUpdatePayload{}, "", false, err
	}
	parents, err := idSetTx(ctx, tx, `SELECT folder_id FROM task_parents WHERE task_id = ?`, taskID)
	if err != nil {
		return TaskUpdatePayload{}, "", false, err
	}
	before.RemoveResponsibles, before.AddResponsibles = only(p.AddResponsibles, responsibles, false), only(p.RemoveResponsibles, responsibles, true)
	before.RemoveParents, before.AddParents = only(p.AddParents, parents, false), only(p.RemoveParents, parents, true)
	return before, title, true, nil
}

func idSetTx(ctx context.Context, tx *sql.Tx, query, arg string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	set := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}

// only keeps the ids whose presence in set is present.
func only(ids []string, set map[string]bool, present bool) []string {
	var out []string
	for _, id := range ids {
		if set[id] == present {
			out = append(out, id)
		}
	}
	return out
}

// timelogBeforeTx reads the cached entry a write is about to change or remove, as the create payload that would put it back, and its task.
// ok is false when the cache has no row for it.
func timelogBeforeTx(ctx context.Context, tx *sql.Tx, timelogID string) (old TimelogCreatePayload, taskID string, ok bool, err error) {
	err = tx.QueryRowContext(ctx, `SELECT task_id, hours, tracked_date, comment FROM timelogs WHERE id = ?`, timelogID).
		Scan(&taskID, &old.Hours, &old.TrackedDate, &old.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return TimelogCreatePayload{}, "", false, nil
	}
	if err != nil {
		return TimelogCreatePayload{}, "", false, err
	}
	return old, taskID, true, nil
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
	before, title, ok, err := taskBeforeTx(ctx, tx, taskID, p)
	if err != nil {
		return 0, err
	}
	var remembered any
	if ok {
		remembered = before
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTaskUpdate, taskID, taskID, title, p, remembered)
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
		args = append(args, p.Description, StripHTML(p.Description))
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
// The local row carries the status Wrike will give the task, see statusForNewTask, the create itself sends none
// and the swap brings the real one.
func (o outboxRepo) EnqueueTaskCreate(ctx context.Context, folderID string, p TaskCreatePayload) (int64, error) {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := o.insertOutboxTx(ctx, tx, KindTaskCreate, folderID, "", p.Title, p, nil)
	if err != nil {
		return 0, err
	}
	// The task's id is the row's own number, known only now.
	if _, err := tx.ExecContext(ctx, `UPDATE outbox SET task_id = ? WHERE id = ?`, LocalID(id), id); err != nil {
		return 0, err
	}
	statusID, err := statusForNewTask(ctx, tx, folderID)
	if err != nil {
		return 0, err
	}
	now := o.stamp()
	local := Task{
		ID: LocalID(id), Title: p.Title, Description: p.Description, Status: "Active", CustomStatusID: statusID,
		Importance: cmp.Or(p.Importance, "Normal"), ResponsibleIDs: p.Responsibles, ParentIDs: []string{folderID},
		CreatedDate: now, UpdatedDate: now,
	}
	if err := upsertTasksTx(ctx, tx, []Task{local}, "", false); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// statusForNewTask is the status a task created in folderID starts in on Wrike: the first visible Active status
// of the default task workflow of the folder's space, the one the Space object names in defaultTaskWorkflowId.
// On the live account that was the standard workflow even in a space that owns a workflow of its own,
// so the workflow the view shows is no guide.
// A folder under no cached space, or a space whose default the cache lacks, falls back to the standard workflow.
// A folder shared by two spaces takes the first space by id.
func statusForNewTask(ctx context.Context, tx *sql.Tx, folderID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE up(id) AS (
			SELECT ?
			UNION
			SELECT fc.parent_id FROM folder_children fc JOIN up ON fc.child_id = up.id
		)
		SELECT cs.id FROM up
		JOIN spaces sp ON sp.id = up.id
		JOIN custom_statuses cs ON cs.workflow_id = sp.default_task_workflow_id
		WHERE cs.status_group = 'Active' AND cs.hidden = 0
		ORDER BY sp.id, cs.position
		LIMIT 1`, folderID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT cs.id FROM custom_statuses cs JOIN workflows w ON w.id = cs.workflow_id
		WHERE w.standard = 1 AND cs.status_group = 'Active' AND cs.hidden = 0
		ORDER BY cs.position
		LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
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
	title, err := taskTitleTx(ctx, tx, taskID)
	if err != nil {
		return 0, err
	}
	id, err := o.insertOutboxTx(ctx, tx, KindCommentCreate, taskID, taskID, title, CommentCreatePayload{Text: text}, nil)
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
	title, err := taskTitleTx(ctx, tx, taskID)
	if err != nil {
		return 0, err
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTimelogCreate, taskID, taskID, title, p, nil)
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
	old, taskID, ok, err := timelogBeforeTx(ctx, tx, timelogID)
	if err != nil {
		return 0, err
	}
	title, err := taskTitleTx(ctx, tx, taskID)
	if err != nil {
		return 0, err
	}
	var remembered any
	if ok {
		remembered = TimelogUpdatePayload(old)
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTimelogUpdate, timelogID, taskID, title, p, remembered)
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
	old, taskID, ok, err := timelogBeforeTx(ctx, tx, timelogID)
	if err != nil {
		return 0, err
	}
	title, err := taskTitleTx(ctx, tx, taskID)
	if err != nil {
		return 0, err
	}
	var remembered any
	if ok {
		remembered = old
	}
	id, err := o.insertOutboxTx(ctx, tx, KindTimelogDelete, timelogID, taskID, title, struct{}{}, remembered)
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
		FROM outbox WHERE state IN `+queueStates).Scan(&pending, &failed)
	return pending, failed, err
}

// Get reads one row by id, ErrNotFound once the sweep has removed it.
func (o outboxRepo) Get(ctx context.Context, id int64) (OutboxRow, error) {
	r, err := scanOutboxRow(o.r.QueryRowContext(ctx,
		`SELECT `+outboxColumns+` FROM outbox WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return OutboxRow{}, ErrNotFound
	}
	return r, err
}

const outboxColumns = `id, kind, entity_id, payload, before, task_id, task_title, source, state, attempts, last_error,
	COALESCE(next_attempt_at, ''), created_at, COALESCE(done_at, '')`

func scanOutboxRow(row interface{ Scan(...any) error }) (OutboxRow, error) {
	var r OutboxRow
	var kind, state, payload, before string
	err := row.Scan(&r.ID, &kind, &r.EntityID, &payload, &before, &r.TaskID, &r.TaskTitle, &r.Source, &state, &r.Attempts,
		&r.LastError, &r.NextAttemptAt, &r.CreatedAt, &r.DoneAt)
	if err != nil {
		return OutboxRow{}, err
	}
	r.Kind, r.State, r.Payload, r.Before = OutboxKind(kind), OutboxState(state), []byte(payload), []byte(before)
	return r, nil
}

func (o outboxRepo) NextDue(ctx context.Context, now string) (OutboxRow, error) {
	// A dependent edit or delete still targeting a local id waits for its create to drain and remap it,
	// ordering by id alone is not enough once the create backs off and the dependent becomes due first.
	// A row also waits while an older row on the same entity is pending or in flight,
	// a later write on a task sent ahead of one in backoff would be overwritten when the older one lands.
	// A create names its folder as the entity and two new tasks in one folder do not depend on each other.
	r, err := scanOutboxRow(o.r.QueryRowContext(ctx, `
		SELECT `+outboxColumns+` FROM outbox
		WHERE state = 'pending' AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
			AND entity_id NOT LIKE ?
			AND NOT EXISTS (SELECT 1 FROM outbox o WHERE o.entity_id = outbox.entity_id AND o.id < outbox.id
				AND o.state IN ('pending', 'inflight') AND o.kind <> ?)
		ORDER BY id LIMIT 1`, now, LocalIDPrefix+"%", string(KindTaskCreate)))
	if errors.Is(err, sql.ErrNoRows) {
		return OutboxRow{}, ErrNotFound
	}
	return r, err
}

func (o outboxRepo) Blocker(ctx context.Context, rowID int64) (OutboxRow, error) {
	var entity string
	err := o.r.QueryRowContext(ctx,
		`SELECT entity_id FROM outbox WHERE id = ? AND kind <> ?`, rowID, string(KindTaskCreate)).Scan(&entity)
	if errors.Is(err, sql.ErrNoRows) {
		return OutboxRow{}, ErrNotFound
	}
	if err != nil {
		return OutboxRow{}, err
	}
	var r OutboxRow
	if IsLocalID(entity) {
		// The create is the oldest row the task has, every row on the local id came after it.
		// It is the blocker until it lands, which swaps the local id away, or until it is discarded, which takes the dependents with it.
		// A finished create is history and blocks nothing.
		r, err = scanOutboxRow(o.r.QueryRowContext(ctx,
			`SELECT `+outboxColumns+` FROM outbox WHERE id = ? AND state IN `+queueStates,
			strings.TrimPrefix(entity, LocalIDPrefix)))
	} else {
		r, err = scanOutboxRow(o.r.QueryRowContext(ctx, `
			SELECT `+outboxColumns+` FROM outbox
			WHERE entity_id = ? AND id < ? AND state IN ('pending', 'inflight') AND kind <> ?
			ORDER BY id LIMIT 1`, entity, rowID, string(KindTaskCreate)))
	}
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

// finishTx takes a row out of the queue as sent or discarded and sweeps the history older than HistoryDays.
// A row that already left the queue is refused with ErrNotFound: two drainers can work one row when the lock file is unavailable,
// and the later one's bookkeeping must not send a discarded write again or mark a sent one twice.
// The sweep names the two history states, so a queued row never matches whatever its age.
func (o outboxRepo) finishTx(ctx context.Context, tx *sql.Tx, id int64, state OutboxState) error {
	res, err := tx.ExecContext(ctx, `UPDATE outbox SET state = ?, done_at = ? WHERE id = ? AND state IN `+queueStates,
		string(state), o.stamp(), id)
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
	cutoff := o.now().UTC().AddDate(0, 0, -HistoryDays).Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `DELETE FROM outbox WHERE state IN `+historyStates+` AND done_at < ?`, cutoff)
	return err
}

func (o outboxRepo) Complete(ctx context.Context, id int64) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := o.finishTx(ctx, tx, id, StateSent); err != nil {
		return err
	}
	return tx.Commit()
}

func (o outboxRepo) CompleteTask(ctx context.Context, id int64, real Task) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := o.finishTx(ctx, tx, id, StateSent); err != nil {
		return err
	}
	if err := upsertTasksTx(ctx, tx, []Task{real}, "", false); err != nil {
		return err
	}
	return tx.Commit()
}

// CompleteTaskCreate swaps the local task for the server's and points everything that named the local id at the real one.
// The server row goes in first and the thread rows move before the local row is deleted,
// the foreign key would otherwise cascade the queued comments away.
// The row itself stays as sent, see finishTx.
func (o outboxRepo) CompleteTaskCreate(ctx context.Context, id int64, real Task) error {
	tx, err := o.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := o.finishTx(ctx, tx, id, StateSent); err != nil {
		return err
	}
	if err := upsertTasksTx(ctx, tx, []Task{real}, "", false); err != nil {
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
	// The create's own row and the writes made on the local task name the real task from here on, the history included.
	if _, err := tx.ExecContext(ctx, `UPDATE outbox SET task_id = ? WHERE task_id = ?`, real.ID, localID); err != nil {
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
	if err := o.finishTx(ctx, tx, id, StateSent); err != nil {
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
	if err := o.finishTx(ctx, tx, id, StateSent); err != nil {
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

// Reschedule and Fail take only a queue row, like finishTx, a finished row a second drainer still holds stays finished.
func (o outboxRepo) Reschedule(ctx context.Context, id int64, errText, nextAttemptAt string) error {
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'pending', attempts = attempts + 1,
			last_error = ?, next_attempt_at = ?
		WHERE id = ? AND state IN `+queueStates, errText, nextAttemptAt, id)
}

func (o outboxRepo) Fail(ctx context.Context, id int64, errText string) error {
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'failed', attempts = attempts + 1,
			last_error = ?, next_attempt_at = NULL
		WHERE id = ? AND state IN `+queueStates, errText, id)
}

func (o outboxRepo) Hold(ctx context.Context, id int64, errText string, before []byte) error {
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'failed', attempts = attempts + 1,
			last_error = ?, next_attempt_at = NULL, before = ?
		WHERE id = ? AND state IN `+queueStates, errText, string(before), id)
}

func (o outboxRepo) Defer(ctx context.Context, id int64, errText string) error {
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'pending', last_error = ?
		WHERE id = ? AND state IN `+queueStates, errText, id)
}

func (o outboxRepo) Retry(ctx context.Context, id int64) error {
	// An inflight row belongs to the engine, like in Discard.
	return o.expectOne(ctx, `
		UPDATE outbox SET state = 'pending', attempts = 0, last_error = '',
			next_attempt_at = NULL
		WHERE id = ? AND state IN ('pending', 'failed')`, id)
}

// Discard drops a queued write. The row stays as discarded, see finishTx.
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
	done := o.stamp()
	// The dependents leave the queue as discarded rows, in the same breath as the row they waited for.
	// Only queue states are touched, a dependent cannot be in flight while its create is still here to discard.
	const dropDependents = `UPDATE outbox SET state = 'discarded', done_at = ? WHERE entity_id = ? AND state IN ('pending', 'failed')`
	switch OutboxKind(kind) {
	case KindCommentCreate:
		if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, localID); err != nil {
			return err
		}
	case KindTaskCreate:
		// The comments go with the task through the cascade, the writes queued on it are discarded here,
		// their target never existed on the server.
		// Time entries have no foreign key to tasks, so they are deleted by hand.
		// Edits and deletes queued under such an entry name its local id, which only the queue still knows:
		// a queued delete has already taken the entry's own row out of the timelogs table.
		if _, err := tx.ExecContext(ctx, `
			UPDATE outbox SET state = 'discarded', done_at = ? WHERE state IN ('pending', 'failed')
				AND entity_id IN (SELECT ? || id FROM outbox WHERE kind = ? AND entity_id = ? AND state IN ('pending', 'failed'))`,
			done, LocalIDPrefix, string(KindTimelogCreate), localID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM timelogs WHERE task_id = ?`, localID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, localID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, dropDependents, done, localID); err != nil {
			return err
		}
	case KindTimelogCreate:
		if _, err := tx.ExecContext(ctx, `DELETE FROM timelogs WHERE id = ?`, localID); err != nil {
			return err
		}
		// Edits queued against the create die with it,
		// their target never existed on the server and would sit pending forever otherwise.
		if _, err := tx.ExecContext(ctx, dropDependents, done, localID); err != nil {
			return err
		}
	}
	if err := o.finishTx(ctx, tx, id, StateDiscarded); err != nil {
		return err
	}
	return tx.Commit()
}

func (o outboxRepo) ListChanges(ctx context.Context) ([]OutboxRow, error) {
	// A pending row with an error is one Reschedule put into backoff, the engine cannot tell a passing failure
	// from one the payload earns every time, and such a row holds the later writes on its entity, see NextDue.
	// Listing it with the rest lets a person decide.
	// The history is cut at HistoryDays here as well as by the sweep: the sweep runs with a write,
	// and a user who only reads for a while would otherwise see rows older than the screen promises.
	// It is ordered by the time a row went out or was dropped, a failed row discarded today was queued long before
	// the writes that went out since, and the person who dropped it looks for it at the top.
	cutoff := o.now().UTC().AddDate(0, 0, -HistoryDays).Format(time.RFC3339)
	rows, err := o.r.QueryContext(ctx, `
		SELECT `+outboxColumns+` FROM outbox
		WHERE state IN `+queueStates+` OR done_at >= ?
		ORDER BY state IN `+historyStates+`,
			CASE WHEN state IN `+historyStates+` THEN done_at ELSE '' END DESC,
			CASE WHEN state IN `+historyStates+` THEN -id ELSE id END`, cutoff)
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
		SELECT CASE WHEN kind = ? THEN ? || id ELSE entity_id END, state FROM outbox
		WHERE state IN `+queueStates,
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
