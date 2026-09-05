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
