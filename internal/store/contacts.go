package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type ContactRepo interface {
	ReplaceAll(ctx context.Context, contacts []Contact) error
	Get(ctx context.Context, id string) (Contact, error)
	List(ctx context.Context) ([]Contact, error)
	// GroupsOf lists the groups the contact is a member of, by name.
	GroupsOf(ctx context.Context, contactID string) ([]Contact, error)
}

func (s *Store) Contacts() ContactRepo { return contactRepo{w: s.writer, r: s.reader} }

type contactRepo struct {
	w, r *sql.DB
}

// The members come folded into the row, one query for the list the pickers read.
const contactColumns = `id, first_name, last_name, type, primary_email, deleted, me,
	COALESCE((SELECT GROUP_CONCAT(contact_id) FROM group_members gm WHERE gm.group_id = contacts.id), '')`

func (c contactRepo) ReplaceAll(ctx context.Context, contacts []Contact) error {
	tx, err := c.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The members go with the contacts, the foreign key cascades the delete.
	if _, err := tx.ExecContext(ctx, `DELETE FROM contacts`); err != nil {
		return err
	}
	for _, ct := range contacts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO contacts (id, first_name, last_name, type, primary_email, deleted, me)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ct.ID, ct.FirstName, ct.LastName, ct.Type, ct.PrimaryEmail, ct.Deleted, ct.Me); err != nil {
			return err
		}
		for _, member := range ct.MemberIDs {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO group_members (group_id, contact_id) VALUES (?, ?)`, ct.ID, member); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func scanContact(row interface{ Scan(dest ...any) error }) (Contact, error) {
	var ct Contact
	var members string
	err := row.Scan(&ct.ID, &ct.FirstName, &ct.LastName, &ct.Type, &ct.PrimaryEmail, &ct.Deleted, &ct.Me, &members)
	if members != "" {
		ct.MemberIDs = strings.Split(members, ",")
	}
	return ct, err
}

func (c contactRepo) Get(ctx context.Context, id string) (Contact, error) {
	ct, err := scanContact(c.r.QueryRowContext(ctx, `SELECT `+contactColumns+` FROM contacts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Contact{}, ErrNotFound
	}
	return ct, err
}

func (c contactRepo) List(ctx context.Context) ([]Contact, error) {
	return c.list(ctx, `SELECT `+contactColumns+` FROM contacts ORDER BY first_name, last_name`)
}

func (c contactRepo) GroupsOf(ctx context.Context, contactID string) ([]Contact, error) {
	return c.list(ctx, `
		SELECT `+contactColumns+` FROM contacts
		WHERE id IN (SELECT group_id FROM group_members WHERE contact_id = ?) ORDER BY first_name, last_name`, contactID)
}

func (c contactRepo) list(ctx context.Context, query string, args ...any) ([]Contact, error) {
	rows, err := c.r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Contact
	for rows.Next() {
		ct, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ct)
	}
	return out, rows.Err()
}
