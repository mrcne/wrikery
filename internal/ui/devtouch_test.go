package ui

import (
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func TestBranchName(t *testing.T) {
	task := store.Task{ID: "IEAATASK01", Title: "Fix: auth retry loop (401) -- again!", Permalink: "https://www.wrike.com/open.htm?id=1200001"}
	if got := branchName("{id}-{slug}", task); got != "1200001-fix-auth-retry-loop-401-again" {
		t.Errorf("got %q", got)
	}
	if got := branchName("feat/{slug}", task); got != "feat/fix-auth-retry-loop-401-again" {
		t.Errorf("got %q", got)
	}
	long := store.Task{ID: "X", Title: strings.Repeat("word ", 20)}
	if got := branchName("{id}-{slug}", long); len(got) > 2+40 || !strings.HasPrefix(got, "X-word-word") {
		t.Errorf("slug not cut at 40 or id fallback missing: %q", got)
	}
}

func TestTaskNumber(t *testing.T) {
	if taskNumber("https://www.wrike.com/open.htm?id=42") != "42" || taskNumber("") != "" {
		t.Error("taskNumber")
	}
}
