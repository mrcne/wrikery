package store

import (
	"context"
	"errors"
	"io/fs"
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
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.writer.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(names) {
		t.Errorf("applied migrations = %d, want %d", n, len(names))
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
	defer func() { _ = st.Close() }()
	rows, err := st.reader.Query(`SELECT space FROM folders LIMIT 0`)
	if err != nil {
		t.Fatalf("space column missing after reopen: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the space column probe: %v", err)
	}
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

func TestDeletingATaskCascades(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	stmts := []string{
		`INSERT INTO tasks (id, title, created_date, updated_date)
		 VALUES ('T1', 'a task', '2026-09-01T10:00:00Z', '2026-09-01T10:00:00Z')`,
		`INSERT INTO comments (id, task_id, author_id, text, created_date)
		 VALUES ('C1', 'T1', 'U1', 'hello', '2026-09-01T10:00:00Z')`,
		`INSERT INTO task_responsibles (task_id, contact_id) VALUES ('T1', 'U1')`,
		`INSERT INTO timelogs (id, task_id, user_id, tracked_date, hours)
		 VALUES ('L1', 'T1', 'U1', '2026-09-01', 1.5)`,
		`DELETE FROM tasks WHERE id = 'T1'`,
	}
	for _, s := range stmts {
		if _, err := st.writer.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	counts := map[string]int{"comments": 0, "task_responsibles": 0, "timelogs": 1}
	for table, want := range counts {
		var n int
		if err := st.reader.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s rows after task delete = %d, want %d", table, n, want)
		}
	}
}

func TestScopeKindIsChecked(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	_, err = st.writer.Exec(
		`INSERT INTO scopes (scope_id, kind, title) VALUES ('X', 'bogus', 'x')`)
	if err == nil {
		t.Fatal("bogus scope kind was accepted")
	}
}

func TestMetaRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	if _, err := st.GetMeta(ctx, "me_contact_id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key error = %v, want ErrNotFound", err)
	}
	if err := st.SetMeta(ctx, "me_contact_id", "U1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta(ctx, "me_contact_id", "U2"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	v, err := st.GetMeta(ctx, "me_contact_id")
	if err != nil {
		t.Fatal(err)
	}
	if v != "U2" {
		t.Errorf("value = %q, want U2", v)
	}
}
