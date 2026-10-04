package store

import (
	"context"
	"database/sql"
)

type PinRepo interface {
	Set(ctx context.Context, folderID string, pinned bool) error
	List(ctx context.Context) ([]string, error)
}

func (s *Store) Pins() PinRepo { return pinRepo{w: s.writer, r: s.reader} }

type pinRepo struct {
	w, r *sql.DB
}

func (p pinRepo) Set(ctx context.Context, folderID string, pinned bool) error {
	var err error
	if pinned {
		_, err = p.w.ExecContext(ctx, `INSERT OR IGNORE INTO pins (folder_id) VALUES (?)`, folderID)
	} else {
		_, err = p.w.ExecContext(ctx, `DELETE FROM pins WHERE folder_id = ?`, folderID)
	}
	return err
}

func (p pinRepo) List(ctx context.Context) ([]string, error) {
	rows, err := p.r.QueryContext(ctx, `SELECT folder_id FROM pins ORDER BY folder_id`)
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
