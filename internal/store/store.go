// Package store is the SQLite layer: schema, migrations, queries and the outbox. No HTTP, no UI code.
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type Store struct {
	writer *sql.DB
	reader *sql.DB
}

// Open opens or creates the database at path and applies pending migrations.
// Two handles on one file: the writer is capped at one connection with immediate transactions
// so writes serialize cleanly, the readers run concurrently thanks to WAL.
// Requires a file path, :memory: would give each handle its own database.
func Open(path string) (*Store, error) {
	pragmas := "?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)"
	writer, err := sql.Open("sqlite", "file:"+path+pragmas+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(1)
	if err := migrate(writer); err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader, err := sql.Open("sqlite", "file:"+path+pragmas)
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(4)
	return &Store{writer: writer, reader: reader}, nil
}

func (s *Store) Close() error {
	rerr := s.reader.Close()
	werr := s.writer.Close()
	if werr != nil {
		return werr
	}
	return rerr
}
