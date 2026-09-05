package sync

import (
	"testing"
	"time"
)

func TestBackoffDoublesAndCaps(t *testing.T) {
	base, ceil := 2*time.Second, 5*time.Minute
	for _, c := range []struct {
		attempt int
		floor   time.Duration
	}{
		{0, 2 * time.Second},
		{1, 4 * time.Second},
		{5, 64 * time.Second},
		{10, 5 * time.Minute},
		{40, 5 * time.Minute},
	} {
		got := backoff(c.attempt, base, ceil)
		if got < c.floor || got > c.floor+c.floor/4 {
			t.Errorf("backoff(%d) = %v, want within [%v, %v]", c.attempt, got, c.floor, c.floor+c.floor/4)
		}
	}
}
