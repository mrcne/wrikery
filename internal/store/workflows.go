package store

import (
	"context"
	"database/sql"
)

type WorkflowRepo interface {
	ReplaceAll(ctx context.Context, workflows []Workflow) error
	List(ctx context.Context) ([]Workflow, error)
}

func (s *Store) Workflows() WorkflowRepo { return workflowRepo{w: s.writer, r: s.reader} }

type workflowRepo struct {
	w, r *sql.DB
}

func (w workflowRepo) ReplaceAll(ctx context.Context, workflows []Workflow) error {
	tx, err := w.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// custom_statuses cascades from workflows on delete, no separate delete needed.
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflows`); err != nil {
		return err
	}
	for _, wf := range workflows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO workflows (id, name, standard, hidden)
			VALUES (?, ?, ?, ?)`, wf.ID, wf.Name, wf.Standard, wf.Hidden); err != nil {
			return err
		}
		for i, cs := range wf.CustomStatuses {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO custom_statuses
					(id, workflow_id, name, color, status_group, standard, hidden, position)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				cs.ID, wf.ID, cs.Name, cs.Color, cs.Group, cs.Standard, cs.Hidden, i); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (w workflowRepo) List(ctx context.Context) ([]Workflow, error) {
	rows, err := w.r.QueryContext(ctx,
		`SELECT id, name, standard, hidden FROM workflows ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Workflow
	for rows.Next() {
		var wf Workflow
		if err := rows.Scan(&wf.ID, &wf.Name, &wf.Standard, &wf.Hidden); err != nil {
			return nil, err
		}
		out = append(out, wf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		statuses, err := w.statuses(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].CustomStatuses = statuses
	}
	return out, nil
}

func (w workflowRepo) statuses(ctx context.Context, workflowID string) ([]CustomStatus, error) {
	rows, err := w.r.QueryContext(ctx, `
		SELECT id, name, color, status_group, standard, hidden
		FROM custom_statuses WHERE workflow_id = ? ORDER BY position`, workflowID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CustomStatus
	for rows.Next() {
		var cs CustomStatus
		if err := rows.Scan(&cs.ID, &cs.Name, &cs.Color, &cs.Group, &cs.Standard, &cs.Hidden); err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}
