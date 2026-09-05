package ui

import (
	"strings"
	"testing"
)

func TestCompositePlacesForegroundOverBackground(t *testing.T) {
	bg := "aaaaaaaa\nbbbbbbbb\ncccccccc"
	fg := "XX\nYY"
	got := composite(bg, fg, 3, 1)
	want := "aaaaaaaa\nbbbXXbbb\ncccYYccc"
	if got != want {
		t.Errorf("composite =\n%s\nwant\n%s", got, want)
	}
}

func TestCompositeIgnoresRowsOutsideBackground(t *testing.T) {
	if got := composite("ab", "Z\nZ\nZ", 0, 1); got != "ab" {
		t.Errorf("got %q", got)
	}
}

func TestCenteredPlacesForegroundInTheMiddle(t *testing.T) {
	bg := "aaaaaaaa\naaaaaaaa\naaaaaaaa"
	got := centered(bg, "X", 8, 3)
	lines := strings.Split(got, "\n")
	if lines[1] != "aaaXaaaa" {
		t.Errorf("centered = %q, want X on the middle row at column 3", got)
	}
}
