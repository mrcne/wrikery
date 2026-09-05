package store

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesFileAndAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrike.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	if _, err := st.writer.Exec(
		`INSERT INTO meta (key, value) VALUES ('hello', 'world')`); err != nil {
		t.Fatalf("meta table not usable: %v", err)
	}
	var n int
	if err := st.writer.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("applied migrations = %d, want 1", n)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrike.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	_ = st.Close()
}

func TestFTS5IsAvailable(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	stmts := []string{
		`CREATE VIRTUAL TABLE probe USING fts5(title, body)`,
		`INSERT INTO probe (title, body) VALUES ('fix login', 'the token expires early')`,
	}
	for _, s := range stmts {
		if _, err := st.writer.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	var title string
	err = st.writer.QueryRow(
		`SELECT title FROM probe WHERE probe MATCH 'token'`).Scan(&title)
	if err != nil {
		t.Fatalf("fts query: %v", err)
	}
	if title != "fix login" {
		t.Errorf("title = %q, want fix login", title)
	}
}

func TestReaderSeesCommittedWrites(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	if _, err := st.writer.Exec(
		`INSERT INTO meta (key, value) VALUES ('k', 'v')`); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := st.reader.QueryRow(
		`SELECT value FROM meta WHERE key = 'k'`).Scan(&v); err != nil {
		t.Fatalf("read through reader: %v", err)
	}
	if v != "v" {
		t.Errorf("value = %q, want v", v)
	}
}

func TestReadWorksWhileWriteTxOpen(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	tx, err := st.writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('open', 'tx')`); err != nil {
		t.Fatal(err)
	}
	// WAL must let a reader run while a write transaction is open.
	var n int
	if err := st.reader.QueryRow(`SELECT COUNT(*) FROM meta`).Scan(&n); err != nil {
		t.Fatalf("read during write tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
