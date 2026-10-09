package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type ScopeRepo interface {
	Upsert(ctx context.Context, s Scope) error
	Get(ctx context.Context, id string) (Scope, error)
	Followed(ctx context.Context) ([]Scope, error)
	// SetFollowed makes the given scopes the followed set and unfollows every other row.
	SetFollowed(ctx context.Context, scopes []Scope) error
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

// SetFollowed keeps the cursor of a scope that stays followed, so its next pull is still incremental.
// A scope that comes back from unfollowed starts from scratch whatever its row holds:
// the refresh after an unfollow sweeps the scope's tasks out of the cache, and a pull that was on the wire during the unfollow
// can still write its cursor onto the row, so a pull from that cursor would never bring the swept tasks back.
// The cursor of a row unfollowed here is cleared too, it means nothing without the tasks.
func (s scopeRepo) SetFollowed(ctx context.Context, scopes []Scope) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	keep := make([]string, 0, len(scopes))
	args := make([]any, 0, len(scopes))
	for _, sc := range scopes {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO scopes (scope_id, kind, title, followed)
			VALUES (?, ?, ?, 1)
			ON CONFLICT(scope_id) DO UPDATE SET
				kind = excluded.kind, title = excluded.title, followed = 1,
				cursor = CASE WHEN scopes.followed = 1 THEN scopes.cursor END,
				last_synced_at = CASE WHEN scopes.followed = 1 THEN scopes.last_synced_at END`,
			sc.ID, sc.Kind, sc.Title)
		if err != nil {
			return err
		}
		keep = append(keep, "?")
		args = append(args, sc.ID)
	}
	unfollow := `UPDATE scopes SET followed = 0, cursor = NULL, last_synced_at = NULL WHERE followed = 1`
	if len(keep) > 0 {
		unfollow += ` AND scope_id NOT IN (` + strings.Join(keep, ", ") + `)`
	}
	if _, err := tx.ExecContext(ctx, unfollow, args...); err != nil {
		return err
	}
	return tx.Commit()
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
