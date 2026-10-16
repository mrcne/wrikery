package syncer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func seedTask(t *testing.T, st *store.Store, id, title string) {
	t.Helper()
	err := st.Tasks().Upsert(context.Background(), []store.Task{{
		ID: id, Title: title, Status: "Active",
		CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z",
	}})
	if err != nil {
		t.Fatal(err)
	}
}

// wantSent fails unless the row stayed in the outbox as sent, the engine no longer deletes what it sent.
func wantSent(t *testing.T, st *store.Store, id int64) {
	t.Helper()
	row, err := st.Outbox().Get(context.Background(), id)
	if err != nil || row.State != store.StateSent {
		t.Errorf("row %d = %+v, %v, want sent", id, row, err)
	}
}

// failedRows is the queue's failed rows, oldest first.
func failedRows(t *testing.T, st *store.Store) []store.OutboxRow {
	t.Helper()
	all, err := st.Outbox().ListChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []store.OutboxRow
	for _, r := range all {
		if r.State == store.StateFailed {
			out = append(out, r)
		}
	}
	return out
}
