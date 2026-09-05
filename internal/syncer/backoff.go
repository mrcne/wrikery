package syncer

import (
	"math/rand/v2"
	"time"
)

// backoff returns the wait before retrying attempt (0 based): base doubled per attempt up to ceil,
// plus up to a quarter of jitter so rows and reconnects do not retry in lockstep.
// The loop stops at the ceiling instead of shifting by attempt: a shift overflows for a base of
// 18 seconds or more at attempt 29, and attempts are not bounded anywhere.
func backoff(attempt int, base, ceil time.Duration) time.Duration {
	d := base
	for i := 0; i < attempt && d < ceil; i++ {
		d *= 2
	}
	if d > ceil {
		d = ceil
	}
	return d + rand.N(d/4+1)
}
