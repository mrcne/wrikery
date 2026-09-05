package sync

import (
	"math/rand/v2"
	"time"
)

// backoff returns the wait before retrying attempt (0 based): base doubled per attempt up to ceil,
// plus up to a quarter of jitter so rows and reconnects do not retry in lockstep.
func backoff(attempt int, base, ceil time.Duration) time.Duration {
	d := ceil
	if attempt < 30 {
		if shifted := base << attempt; shifted < ceil {
			d = shifted
		}
	}
	return d + rand.N(d/4+1)
}
