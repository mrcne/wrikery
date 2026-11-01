package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

func TestOpenDemoStoreSeedsAndCleansUp(t *testing.T) {
	st, dir, cleanup, err := openDemoStore(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 0)
	if err != nil {
		t.Fatal(err)
	}
	me, err := st.GetMeta(context.Background(), store.MetaKeyMe)
	if err != nil || me != demo.MeID {
		t.Errorf("meta = %q, %v", me, err)
	}
	cleanup()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("demo dir still exists: %v", err)
	}
}

func TestOpenDemoStoreExtendsWhenAsked(t *testing.T) {
	st, _, cleanup, err := openDemoStore(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 20)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	followed, err := st.Scopes().Followed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(followed))
	for _, sc := range followed {
		ids = append(ids, sc.ID)
	}
	if !slices.Contains(ids, demo.SpaceWork) {
		t.Errorf("followed scopes = %v, want the generated space among them", ids)
	}
}

func TestTheDemoProfileCoversTheSeeding(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envProfile, dir)
	t.Setenv(envDemoTasks, "3000")
	_, cleanup, stop, err := startDemo(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	// A profile is a gzipped protocol buffer whose string table names every function of the sampled stacks.
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("internal/demo.Extend")) {
		t.Error("the cpu profile has no sample of the seeding, it started after it")
	}
}

func TestTheDemoRefusesABadProfileDirectory(t *testing.T) {
	t.Setenv(envProfile, filepath.Join(t.TempDir(), "missing"))
	t.Setenv(envDemoTasks, "")
	if _, _, _, err := startDemo(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), envProfile) {
		t.Errorf("err = %v, want one naming %s", err, envProfile)
	}
}
