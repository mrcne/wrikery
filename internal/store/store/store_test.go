package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesFileAndAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrike.db")
	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	if _, err := st.db.Exec(
		`INSERT INTO meta (key, value) VALUES ('hello', 'world')`); err != nil {
		t.Fatalf("meta table not usable: %v", err)
	}
	var n int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("applied migrations = %d, want 1", n)
	}
}

// Reopening must not run the migration again, or a later one could wipe the data it finds.
func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrike.db")
	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(
		`INSERT INTO meta (key, value) VALUES ('cursor', '42')`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer func() { _ = st.Close() }()

	var applied int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Errorf("applied migrations = %d, want 1", applied)
	}
	var value string
	if err := st.db.QueryRow(
		`SELECT value FROM meta WHERE key = 'cursor'`).Scan(&value); err != nil {
		t.Fatalf("row written before the reopen is gone: %v", err)
	}
	if value != "42" {
		t.Errorf("value = %q, want 42", value)
	}
}

// The UI will query inside a loop over rows. With too few connections that hangs.
func TestQueryWhileRowsAreOpen(t *testing.T) {
	st, err := Open(t.Context(), filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := st.db.Exec(
		`INSERT INTO meta (key, value) VALUES ('a', '1'), ('b', '2')`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		rows, err := st.db.Query(`SELECT key FROM meta`)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var n int
			if err := st.db.QueryRow(`SELECT COUNT(*) FROM meta`).Scan(&n); err != nil {
				done <- err
				return
			}
		}
		done <- rows.Err()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a query issued while rows were open never returned, the pool is too small")
	}
}

func TestOpenHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Open(ctx, filepath.Join(t.TempDir(), "wrike.db")); err == nil {
		t.Fatal("want an error from a cancelled context, got nil")
	}
}

func TestFTS5IsAvailable(t *testing.T) {
	st, err := Open(t.Context(), filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	stmts := []string{
		`CREATE VIRTUAL TABLE probe USING fts5(title, body)`,
		`INSERT INTO probe (title, body) VALUES ('fix login', 'the token expires early')`,
	}
	for _, s := range stmts {
		if _, err := st.db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	var title string
	err = st.db.QueryRow(
		`SELECT title FROM probe WHERE probe MATCH 'token'`).Scan(&title)
	if err != nil {
		t.Fatalf("fts query: %v", err)
	}
	if title != "fix login" {
		t.Errorf("title = %q, want fix login", title)
	}
}
