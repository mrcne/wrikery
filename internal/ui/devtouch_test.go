package ui

import (
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func TestBranchName(t *testing.T) {
	task := store.Task{ID: "IEAATASK01", Title: "Fix: auth retry loop (401) -- again!", Permalink: "https://www.wrike.com/open.htm?id=1200001"}
	if got := branchName("{id}-{slug}", task, nil); got != "1200001-fix-auth-retry-loop-401-again" {
		t.Errorf("got %q", got)
	}
	if got := branchName("feat/{slug}", task, nil); got != "feat/fix-auth-retry-loop-401-again" {
		t.Errorf("got %q", got)
	}
	long := store.Task{ID: "X", Title: strings.Repeat("word ", 20)}
	if got := branchName("{id}-{slug}", long, nil); len(got) > 2+40 || !strings.HasPrefix(got, "X-word-word") {
		t.Errorf("slug not cut at 40 or id fallback missing: %q", got)
	}
}

func TestTaskNumber(t *testing.T) {
	if taskNumber("https://www.wrike.com/open.htm?id=42") != "42" || taskNumber("") != "" {
		t.Error("taskNumber")
	}
}

func TestSlugifyTransliteratesNonASCII(t *testing.T) {
	// Escaped so the file stays ASCII: "Zazolc gesla jazn" and "Cafe Zurich" with diacritics.
	polish := "Za\u017c\u00f3\u0142\u0107 g\u0119\u015bl\u0105 ja\u017a\u0144"
	if got := slugify(polish, 40); got != "zazolc-gesla-jazn" {
		t.Errorf("got %q", got)
	}
	french := "Caf\u00e9 Z\u00fcrich"
	if got := slugify(french, 40); got != "cafe-zurich" {
		t.Errorf("got %q", got)
	}
	// "Oresund bridge" with the Danish O with stroke, an uppercase letter NFD does not decompose.
	danish := "\u00d8resund bridge"
	if got := slugify(danish, 40); got != "oresund-bridge" {
		t.Errorf("got %q", got)
	}
}

func TestBranchNameWithSymbolOnlyTitle(t *testing.T) {
	task := store.Task{ID: "X999", Title: "!!! ??? ###"}
	if got := branchName("{id}-{slug}", task, nil); got != "X999-" {
		t.Errorf("got %q", got)
	}
}

func TestBranchNameDropsAHiddenCode(t *testing.T) {
	task := store.Task{ID: "X1", Title: "(MX) Backend: Kafka - Processing", Permalink: "https://www.wrike.com/open.htm?id=77"}
	if got := branchName("{id}-{slug}", task, []string{"(MX)"}); got != "77-backend-kafka-processing" {
		t.Errorf("the hidden code stays out of the branch name, got %q", got)
	}
	if got := branchName("{id}-{slug}", task, nil); got != "77-mx-backend-kafka-processing" {
		t.Errorf("with nothing hidden the code is part of the slug, got %q", got)
	}
}
