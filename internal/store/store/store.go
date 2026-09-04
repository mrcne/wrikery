// Package store is the SQLite layer: schema, migrations, queries and the outbox. No HTTP, no UI code.
package store

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

// One connection is not enough. A query made while another is still reading would wait forever.
const maxConns = 4

type Store struct {
	db *sql.DB
}

// Open opens or creates the database at path and applies pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	// WAL for reads while syncing, busy_timeout so a locked write waits instead of failing.
	// _txlock takes the write lock at the start, so a transaction that reads first still works.
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
