package store

import (
	"context"
	"database/sql"
	"errors"
)

type ContactRepo interface {
	ReplaceAll(ctx context.Context, contacts []Contact) error
	Get(ctx context.Context, id string) (Contact, error)
	List(ctx context.Context) ([]Contact, error)
}

func (s *Store) Contacts() ContactRepo { return contactRepo{w: s.writer, r: s.reader} }

type contactRepo struct {
	w, r *sql.DB
}

const contactColumns = `id, first_name, last_name, type, primary_email, deleted, me`

func (c contactRepo) ReplaceAll(ctx context.Context, contacts []Contact) error {
	tx, err := c.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM contacts`); err != nil {
		return err
	}
	for _, ct := range contacts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO contacts (`+contactColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ct.ID, ct.FirstName, ct.LastName, ct.Type, ct.PrimaryEmail, ct.Deleted, ct.Me); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c contactRepo) Get(ctx context.Context, id string) (Contact, error) {
	row := c.r.QueryRowContext(ctx,
		`SELECT `+contactColumns+` FROM contacts WHERE id = ?`, id)
	var ct Contact
	err := row.Scan(&ct.ID, &ct.FirstName, &ct.LastName, &ct.Type, &ct.PrimaryEmail,
		&ct.Deleted, &ct.Me)
	if errors.Is(err, sql.ErrNoRows) {
		return Contact{}, ErrNotFound
	}
	return ct, err
}

func (c contactRepo) List(ctx context.Context) ([]Contact, error) {
	rows, err := c.r.QueryContext(ctx,
		`SELECT `+contactColumns+` FROM contacts ORDER BY first_name, last_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Contact
	for rows.Next() {
		var ct Contact
		if err := rows.Scan(&ct.ID, &ct.FirstName, &ct.LastName, &ct.Type, &ct.PrimaryEmail,
			&ct.Deleted, &ct.Me); err != nil {
			return nil, err
		}
		out = append(out, ct)
	}
	return out, rows.Err()
}
