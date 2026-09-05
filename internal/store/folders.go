package store

import (
	"context"
	"database/sql"
	"errors"
)

type FolderRepo interface {
	ReplaceTree(ctx context.Context, folders []Folder) error
	Get(ctx context.Context, id string) (Folder, error)
	Children(ctx context.Context, parentID string) ([]Folder, error)
}

func (s *Store) Folders() FolderRepo { return folderRepo{w: s.writer, r: s.reader} }

type folderRepo struct {
	w, r *sql.DB
}

func (f folderRepo) ReplaceTree(ctx context.Context, folders []Folder) error {
	tx, err := f.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM folder_children`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM folders`); err != nil {
		return err
	}
	for _, fo := range folders {
		var pStatus, pCustom, pStart, pEnd any
		isProject := 0
		if fo.Project != nil {
			isProject = 1
			pStatus, pCustom, pStart, pEnd = fo.Project.Status, fo.Project.CustomStatusID,
				fo.Project.StartDate, fo.Project.EndDate
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO folders (id, title, scope, is_project, project_status,
				project_custom_status_id, project_start_date, project_end_date)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fo.ID, fo.Title, fo.Scope, isProject, pStatus, pCustom, pStart, pEnd); err != nil {
			return err
		}
		for _, child := range fo.ChildIDs {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO folder_children (parent_id, child_id)
				VALUES (?, ?)`, fo.ID, child); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func scanFolder(row interface{ Scan(...any) error }) (Folder, error) {
	var fo Folder
	var isProject int
	var pStatus, pCustom, pStart, pEnd sql.NullString
	err := row.Scan(&fo.ID, &fo.Title, &fo.Scope, &isProject,
		&pStatus, &pCustom, &pStart, &pEnd)
	if err != nil {
		return Folder{}, err
	}
	if isProject == 1 {
		fo.Project = &Project{
			Status:         pStatus.String,
			CustomStatusID: pCustom.String,
			StartDate:      pStart.String,
			EndDate:        pEnd.String,
		}
	}
	return fo, nil
}

const folderColumns = `id, title, scope, is_project, project_status,
	project_custom_status_id, project_start_date, project_end_date`

func (f folderRepo) Get(ctx context.Context, id string) (Folder, error) {
	fo, err := scanFolder(f.r.QueryRowContext(ctx,
		`SELECT `+folderColumns+` FROM folders WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Folder{}, ErrNotFound
	}
	if err != nil {
		return Folder{}, err
	}
	fo.ChildIDs, err = f.childIDs(ctx, id)
	return fo, err
}

func (f folderRepo) childIDs(ctx context.Context, id string) ([]string, error) {
	rows, err := f.r.QueryContext(ctx,
		`SELECT child_id FROM folder_children WHERE parent_id = ? ORDER BY child_id`, id)
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

func (f folderRepo) Children(ctx context.Context, parentID string) ([]Folder, error) {
	rows, err := f.r.QueryContext(ctx, `
		SELECT `+folderColumns+` FROM folders
		JOIN folder_children ON child_id = id
		WHERE parent_id = ? ORDER BY title`, parentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Folder
	for rows.Next() {
		fo, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fo)
	}
	return out, rows.Err()
}
