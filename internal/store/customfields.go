package store

import (
	"context"
	"database/sql"
)

type CustomFieldRepo interface {
	ReplaceAll(ctx context.Context, fields []CustomField) error
	List(ctx context.Context) ([]CustomField, error)
}

func (s *Store) CustomFields() CustomFieldRepo { return customFieldRepo{w: s.writer, r: s.reader} }

type customFieldRepo struct {
	w, r *sql.DB
}

func (c customFieldRepo) ReplaceAll(ctx context.Context, fields []CustomField) error {
	tx, err := c.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM custom_fields`); err != nil {
		return err
	}
	for _, f := range fields {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO custom_fields (id, title, type, space_id) VALUES (?, ?, ?, ?)`,
			f.ID, f.Title, f.Type, f.SpaceID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// List answers the fields in the order the API gave them, which is the order the inserts kept.
func (c customFieldRepo) List(ctx context.Context) ([]CustomField, error) {
	rows, err := c.r.QueryContext(ctx, `SELECT id, title, type, space_id FROM custom_fields ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CustomField
	for rows.Next() {
		var f CustomField
		if err := rows.Scan(&f.ID, &f.Title, &f.Type, &f.SpaceID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
