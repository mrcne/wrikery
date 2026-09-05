package store

import (
	"context"
	"database/sql"
)

type SpaceRepo interface {
	ReplaceAll(ctx context.Context, spaces []Space) error
	List(ctx context.Context) ([]Space, error)
}

func (s *Store) Spaces() SpaceRepo { return spaceRepo{w: s.writer, r: s.reader} }

type spaceRepo struct {
	w, r *sql.DB
}

func (s spaceRepo) ReplaceAll(ctx context.Context, spaces []Space) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM spaces`); err != nil {
		return err
	}
	for _, sp := range spaces {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO spaces (id, title, access_type, archived)
			VALUES (?, ?, ?, ?)`, sp.ID, sp.Title, sp.AccessType, sp.Archived); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s spaceRepo) List(ctx context.Context) ([]Space, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT id, title, access_type, archived FROM spaces ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Space
	for rows.Next() {
		var sp Space
		if err := rows.Scan(&sp.ID, &sp.Title, &sp.AccessType, &sp.Archived); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}
