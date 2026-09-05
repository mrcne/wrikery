package sync

import (
	"testing"
	"time"
)

func TestBackoffDoublesAndCaps(t *testing.T) {
	for _, c := range []struct {
		attempt    int
		base, ceil time.Duration
		floor      time.Duration
	}{
		{0, 2 * time.Second, 5 * time.Minute, 2 * time.Second},
		{1, 2 * time.Second, 5 * time.Minute, 4 * time.Second},
		{5, 2 * time.Second, 5 * time.Minute, 64 * time.Second},
		{10, 2 * time.Second, 5 * time.Minute, 5 * time.Minute},
		{40, 2 * time.Second, 5 * time.Minute, 5 * time.Minute},
		// A shift would overflow here and hand a negative wait to the jitter.
		{29, 18 * time.Second, 5 * time.Minute, 5 * time.Minute},
		{40, time.Hour, 2 * time.Hour, 2 * time.Hour},
	} {
		got := backoff(c.attempt, c.base, c.ceil)
		if got < c.floor || got > c.floor+c.floor/4 {
			t.Errorf("backoff(%d, %v, %v) = %v, want within [%v, %v]",
				c.attempt, c.base, c.ceil, got, c.floor, c.floor+c.floor/4)
		}
	}
}
