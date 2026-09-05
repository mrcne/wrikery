package store

import (
	"context"
	"database/sql"
	"errors"
)

type ScopeRepo interface {
	Upsert(ctx context.Context, s Scope) error
	Get(ctx context.Context, id string) (Scope, error)
	Followed(ctx context.Context) ([]Scope, error)
}

func (s *Store) Scopes() ScopeRepo { return scopeRepo{w: s.writer, r: s.reader} }

type scopeRepo struct {
	w, r *sql.DB
}

func (s scopeRepo) Upsert(ctx context.Context, sc Scope) error {
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO scopes (scope_id, kind, title, followed)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scope_id) DO UPDATE SET
			kind = excluded.kind, title = excluded.title, followed = excluded.followed`,
		sc.ID, sc.Kind, sc.Title, sc.Followed)
	return err
}

func (s scopeRepo) Get(ctx context.Context, id string) (Scope, error) {
	row := s.r.QueryRowContext(ctx, `
		SELECT scope_id, kind, title, followed,
		       COALESCE(cursor, ''), COALESCE(last_synced_at, '')
		FROM scopes WHERE scope_id = ?`, id)
	var sc Scope
	err := row.Scan(&sc.ID, &sc.Kind, &sc.Title, &sc.Followed, &sc.Cursor, &sc.LastSyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, ErrNotFound
	}
	return sc, err
}

func (s scopeRepo) Followed(ctx context.Context) ([]Scope, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT scope_id, kind, title, followed,
		       COALESCE(cursor, ''), COALESCE(last_synced_at, '')
		FROM scopes WHERE followed = 1 ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Scope
	for rows.Next() {
		var sc Scope
		if err := rows.Scan(&sc.ID, &sc.Kind, &sc.Title, &sc.Followed, &sc.Cursor, &sc.LastSyncedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}
