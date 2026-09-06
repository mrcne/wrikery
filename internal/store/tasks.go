package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type TaskRepo interface {
	Upsert(ctx context.Context, tasks []Task) error
	ApplyPage(ctx context.Context, scopeID string, tasks []Task, cursor string) error
	Get(ctx context.Context, id string) (Task, error)
	Delete(ctx context.Context, ids ...string) error
	PruneExcept(ctx context.Context, keep []string) (int64, error)
	MarkOpened(ctx context.Context, id, openedAt string) error
	RecentlyOpenedIDs(ctx context.Context, since string, limit int) ([]string, error)
	Search(ctx context.Context, query string, limit int) ([]Task, error)
	ListInFolder(ctx context.Context, folderID string) ([]Task, error)
	ListForResponsible(ctx context.Context, contactID string) ([]Task, error)
}

func (s *Store) Tasks() TaskRepo { return taskRepo{w: s.writer, r: s.reader} }

type taskRepo struct {
	w, r *sql.DB
}

// A Backlog task has a dates block with a type and no dates at all, see https://developers.wrike.com/api/v4/tasks/.
// Written as an empty string a missing due date would sort ahead of every real one, so an empty end reaches the column as NULL.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func upsertTasksTx(ctx context.Context, tx *sql.Tx, tasks []Task) error {
	for _, t := range tasks {
		var dType, dStart, dDue any
		var dDur any
		if t.Dates != nil {
			dDur = t.Dates.Duration
			dType, dStart, dDue = nullIfEmpty(t.Dates.Type), nullIfEmpty(t.Dates.Start), nullIfEmpty(t.Dates.Due)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tasks (id, title, description, description_plain, status,
				custom_status_id, importance, permalink, dates_type, dates_duration,
				dates_start, dates_due, created_date, updated_date)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				title = excluded.title, description = excluded.description,
				description_plain = excluded.description_plain, status = excluded.status,
				custom_status_id = excluded.custom_status_id, importance = excluded.importance,
				permalink = excluded.permalink, dates_type = excluded.dates_type,
				dates_duration = excluded.dates_duration, dates_start = excluded.dates_start,
				dates_due = excluded.dates_due, created_date = excluded.created_date,
				updated_date = excluded.updated_date`,
			t.ID, t.Title, t.Description, stripHTML(t.Description), t.Status,
			t.CustomStatusID, t.Importance, t.Permalink, dType, dDur,
			dStart, dDue, t.CreatedDate, t.UpdatedDate); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM task_responsibles WHERE task_id = ?`, t.ID); err != nil {
			return err
		}
		for _, c := range t.ResponsibleIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO task_responsibles (task_id, contact_id) VALUES (?, ?)`, t.ID, c); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM task_parents WHERE task_id = ?`, t.ID); err != nil {
			return err
		}
		for _, f := range t.ParentIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO task_parents (task_id, folder_id) VALUES (?, ?)`, t.ID, f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t taskRepo) Upsert(ctx context.Context, tasks []Task) error {
	return t.ApplyPage(ctx, "", tasks, "")
}

// ApplyPage writes one pulled page.
// A non empty cursor marks the final page of a pull and advances the scope in the same transaction,
// so a crash can never leave a cursor ahead of its data.
func (t taskRepo) ApplyPage(ctx context.Context, scopeID string, tasks []Task, cursor string) error {
	tx, err := t.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertTasksTx(ctx, tx, tasks); err != nil {
		return err
	}
	if cursor != "" {
		res, err := tx.ExecContext(ctx, `
			UPDATE scopes SET cursor = ?,
				last_synced_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE scope_id = ?`, cursor, scopeID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("store: unknown scope %s", scopeID)
		}
	}
	return tx.Commit()
}

func (t taskRepo) Get(ctx context.Context, id string) (Task, error) {
	row := t.r.QueryRowContext(ctx, `
		SELECT id, title, description, description_plain, status, custom_status_id,
		       importance, permalink, dates_type, dates_duration, dates_start,
		       dates_due, created_date, updated_date, COALESCE(last_opened_at, '')
		FROM tasks WHERE id = ?`, id)
	var task Task
	var dType, dStart, dDue sql.NullString
	var dDur sql.NullInt64
	err := row.Scan(&task.ID, &task.Title, &task.Description, &task.DescriptionPlain,
		&task.Status, &task.CustomStatusID, &task.Importance, &task.Permalink,
		&dType, &dDur, &dStart, &dDue, &task.CreatedDate, &task.UpdatedDate,
		&task.LastOpenedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	if dType.Valid {
		task.Dates = &TaskDates{
			Type:     dType.String,
			Duration: int(dDur.Int64),
			Start:    dStart.String,
			Due:      dDue.String,
		}
	}
	task.ResponsibleIDs, err = t.stringColumn(ctx,
		`SELECT contact_id FROM task_responsibles WHERE task_id = ? ORDER BY contact_id`, id)
	if err != nil {
		return Task{}, err
	}
	task.ParentIDs, err = t.stringColumn(ctx,
		`SELECT folder_id FROM task_parents WHERE task_id = ? ORDER BY folder_id`, id)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (t taskRepo) stringColumn(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := t.r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t taskRepo) Delete(ctx context.Context, ids ...string) error {
	tx, err := t.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PruneExcept implements the deletion sweep.
// The keep set goes through a temp table because an IN list has a practical size limit
// and a followed account can hold tens of thousands of tasks.
func (t taskRepo) PruneExcept(ctx context.Context, keep []string) (int64, error) {
	tx, err := t.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`CREATE TEMP TABLE keep_ids (id TEXT PRIMARY KEY)`); err != nil {
		return 0, err
	}
	for _, id := range keep {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO keep_ids (id) VALUES (?)`, id); err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM tasks WHERE id NOT IN (SELECT id FROM keep_ids)`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE keep_ids`); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

func (t taskRepo) MarkOpened(ctx context.Context, id, openedAt string) error {
	res, err := t.w.ExecContext(ctx,
		`UPDATE tasks SET last_opened_at = ? WHERE id = ?`, openedAt, id)
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

func (t taskRepo) RecentlyOpenedIDs(ctx context.Context, since string, limit int) ([]string, error) {
	return t.stringColumn(ctx, `
		SELECT id FROM tasks WHERE last_opened_at >= ?
		ORDER BY last_opened_at DESC LIMIT ?`, since, limit)
}

// Search runs the user's words as quoted FTS terms, the last one as a prefix.
// Quoting is what keeps FTS5 operators in user input from being parsed as syntax.
func (t taskRepo) Search(ctx context.Context, query string, limit int) ([]Task, error) {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	var m strings.Builder
	for i, term := range terms {
		if i > 0 {
			m.WriteByte(' ')
		}
		m.WriteByte('"')
		m.WriteString(strings.ReplaceAll(term, `"`, `""`))
		m.WriteByte('"')
	}
	m.WriteByte('*')
	matchIDs, err := t.stringColumn(ctx, `
		SELECT tasks.id FROM tasks_fts
		JOIN tasks ON tasks.rowid = tasks_fts.rowid
		WHERE tasks_fts MATCH ? ORDER BY tasks_fts.rank LIMIT ?`, m.String(), limit)
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, id := range matchIDs {
		task, err := t.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
}

// The list queries skip the description columns on purpose: a folder can hold thousands of tasks and the list never shows them.
const taskListColumns = `t.id, t.title, t.status, t.custom_status_id, t.importance, t.permalink,
	t.dates_type, t.dates_duration, t.dates_start, t.dates_due, t.created_date, t.updated_date,
	COALESCE(t.last_opened_at, ''),
	COALESCE((SELECT GROUP_CONCAT(contact_id) FROM task_responsibles r WHERE r.task_id = t.id), '')`

// Open tasks first, then by due date with undated tasks after dated ones, newest change first inside a day.
// NULLIF covers a database written before the empty due date became a NULL, where the column still holds ”.
const taskListOrder = `ORDER BY CASE WHEN t.status IN ('Completed', 'Cancelled') THEN 1 ELSE 0 END,
	NULLIF(t.dates_due, '') IS NULL, NULLIF(t.dates_due, ''), t.updated_date DESC`

func (t taskRepo) ListInFolder(ctx context.Context, folderID string) ([]Task, error) {
	return t.list(ctx, `
		WITH RECURSIVE tree(id) AS (
			SELECT ?
			UNION
			SELECT fc.child_id FROM folder_children fc JOIN tree ON fc.parent_id = tree.id
		)
		SELECT DISTINCT `+taskListColumns+` FROM tasks t
		JOIN task_parents tp ON tp.task_id = t.id
		JOIN tree ON tree.id = tp.folder_id
		`+taskListOrder, folderID)
}

func (t taskRepo) ListForResponsible(ctx context.Context, contactID string) ([]Task, error) {
	return t.list(ctx, `
		SELECT `+taskListColumns+` FROM tasks t
		JOIN task_responsibles tr ON tr.task_id = t.id
		WHERE tr.contact_id = ? `+taskListOrder, contactID)
}

func (t taskRepo) list(ctx context.Context, query string, args ...any) ([]Task, error) {
	rows, err := t.r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		var task Task
		var dType, dStart, dDue sql.NullString
		var dDur sql.NullInt64
		var resp string
		if err := rows.Scan(&task.ID, &task.Title, &task.Status, &task.CustomStatusID, &task.Importance,
			&task.Permalink, &dType, &dDur, &dStart, &dDue, &task.CreatedDate, &task.UpdatedDate,
			&task.LastOpenedAt, &resp); err != nil {
			return nil, err
		}
		if dType.Valid {
			task.Dates = &TaskDates{Type: dType.String, Duration: int(dDur.Int64), Start: dStart.String, Due: dDue.String}
		}
		if resp != "" {
			task.ResponsibleIDs = strings.Split(resp, ",")
			sort.Strings(task.ResponsibleIDs)
		}
		out = append(out, task)
	}
	return out, rows.Err()
}
