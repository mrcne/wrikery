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
	Subtree(ctx context.Context, rootID string) ([]Folder, error)
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
		space := 0
		if fo.Space {
			space = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO folders (id, title, scope, space, is_project, project_status,
				project_custom_status_id, project_start_date, project_end_date)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fo.ID, fo.Title, fo.Scope, space, isProject, pStatus, pCustom, pStart, pEnd); err != nil {
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
	var isProject, space int
	var pStatus, pCustom, pStart, pEnd sql.NullString
	err := row.Scan(&fo.ID, &fo.Title, &fo.Scope, &space, &isProject,
		&pStatus, &pCustom, &pStart, &pEnd)
	if err != nil {
		return Folder{}, err
	}
	fo.Space = space == 1
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

const folderColumns = `id, title, scope, space, is_project, project_status,
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

// Subtree returns the root and everything under it, parents before children, siblings by title.
// The depth guard stops a cycle in a corrupt tree from running forever.
func (f folderRepo) Subtree(ctx context.Context, rootID string) ([]Folder, error) {
	// char(1) and not "/" separates path segments, a sibling titled "API v2" would otherwise sort
	// between "API" and the children of "API" (space is 0x20, slash is 0x2F, 0x01 is below both).
	rows, err := f.r.QueryContext(ctx, `
		WITH RECURSIVE tree(id, depth, path) AS (
			SELECT id, 0, title FROM folders WHERE id = ?
			UNION ALL
			SELECT fo.id, tree.depth + 1, tree.path || char(1) || fo.title
			FROM folder_children fc
			JOIN tree ON fc.parent_id = tree.id
			JOIN folders fo ON fo.id = fc.child_id
			WHERE tree.depth < 32
		)
		SELECT `+folderColumns+` FROM folders JOIN tree USING (id) ORDER BY tree.path`, rootID)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].ChildIDs, err = f.childIDs(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
