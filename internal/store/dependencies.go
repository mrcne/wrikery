package store

import (
	"context"
	"database/sql"
)

type DependencyRepo interface {
	// ReplaceForTask makes deps the task's dependency list and the edges it names, as the task's own endpoint answers it.
	// The answer holds every edge that touches the task, so it speaks for the other end too:
	// an edge the task listed before and lists no more leaves every task's list, and a new edge joins the other end's list when that task is cached.
	// An edge no task lists afterwards is dropped.
	ReplaceForTask(ctx context.Context, taskID string, deps []Dependency) error
	// ForgetForTask drops the task's own dependency list and nothing else, for a task whose edges could not be read.
	// The ids come back with the next pull of the task.
	ForgetForTask(ctx context.Context, taskID string) error
	// ListForTask returns the edges the task lists, predecessors first.
	ListForTask(ctx context.Context, taskID string) ([]Dependency, error)
	// TasksMissingEdges maps a task to the dependency ids it lists with no edge fetched yet, for up to limit tasks.
	TasksMissingEdges(ctx context.Context, limit int) (map[string][]string, error)
}

func (s *Store) Dependencies() DependencyRepo { return dependencyRepo{w: s.writer, r: s.reader} }

type dependencyRepo struct {
	w, r *sql.DB
}

func (d dependencyRepo) ReplaceForTask(ctx context.Context, taskID string, deps []Dependency) error {
	tx, err := d.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	kept := make(map[string]bool, len(deps))
	for _, dep := range deps {
		kept[dep.ID] = true
	}
	// Adding or removing a dependency moves neither task's updatedDate on Wrike,
	// so the other end is never re-pulled for it and only this answer can take a removed edge off that end's list.
	before, err := listedDependencyIDs(ctx, tx, taskID)
	if err != nil {
		return err
	}
	for _, id := range before {
		if kept[id] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_dependencies WHERE dependency_id = ?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_dependencies WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, dep := range deps {
		other := dep.SuccessorID
		if other == taskID {
			other = dep.PredecessorID
		}
		// The other end may be a task outside the cache, and the foreign key refuses a row for it.
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO task_dependencies (task_id, dependency_id)
			SELECT id, ? FROM tasks WHERE id IN (?, ?)`, dep.ID, taskID, other); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO dependencies (id, predecessor_id, successor_id, relation_type, lag_minutes)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				predecessor_id = excluded.predecessor_id, successor_id = excluded.successor_id,
				relation_type = excluded.relation_type, lag_minutes = excluded.lag_minutes`,
			dep.ID, dep.PredecessorID, dep.SuccessorID, dep.RelationType, dep.LagMinutes); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM dependencies WHERE id NOT IN (SELECT dependency_id FROM task_dependencies)`); err != nil {
		return err
	}
	return tx.Commit()
}

func listedDependencyIDs(ctx context.Context, tx *sql.Tx, taskID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT dependency_id FROM task_dependencies WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (d dependencyRepo) ForgetForTask(ctx context.Context, taskID string) error {
	tx, err := d.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_dependencies WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM dependencies WHERE id NOT IN (SELECT dependency_id FROM task_dependencies)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (d dependencyRepo) ListForTask(ctx context.Context, taskID string) ([]Dependency, error) {
	rows, err := d.r.QueryContext(ctx, `
		SELECT dp.id, dp.predecessor_id, dp.successor_id, dp.relation_type, dp.lag_minutes
		FROM dependencies dp JOIN task_dependencies td ON td.dependency_id = dp.id
		WHERE td.task_id = ?
		ORDER BY dp.successor_id = ? DESC, dp.id`, taskID, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Dependency
	for rows.Next() {
		var dep Dependency
		if err := rows.Scan(&dep.ID, &dep.PredecessorID, &dep.SuccessorID, &dep.RelationType, &dep.LagMinutes); err != nil {
			return nil, err
		}
		out = append(out, dep)
	}
	return out, rows.Err()
}

func (d dependencyRepo) TasksMissingEdges(ctx context.Context, limit int) (map[string][]string, error) {
	rows, err := d.r.QueryContext(ctx, `
		SELECT td.task_id, td.dependency_id FROM task_dependencies td
		LEFT JOIN dependencies dp ON dp.id = td.dependency_id
		WHERE dp.id IS NULL
		ORDER BY td.task_id, td.dependency_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var taskID, depID string
		if err := rows.Scan(&taskID, &depID); err != nil {
			return nil, err
		}
		if _, seen := out[taskID]; !seen && len(out) == limit {
			break
		}
		out[taskID] = append(out[taskID], depID)
	}
	return out, rows.Err()
}
