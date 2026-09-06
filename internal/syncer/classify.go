package syncer

import (
	"errors"

	"github.com/mrcne/wrikery/pkg/wrike"
)

type failureClass int

const (
	// failTransient errors are retried later: the network, 5xx, and a 429 that survived the client's own retries.
	failTransient failureClass = iota
	// failAuth pauses the engine until the user refreshes the token.
	failAuth
	// failPermanent rejections will not succeed by waiting. An outbox row goes to the failed state, a scope or a task thread is skipped for the cycle with a log line.
	failPermanent
)

func classify(err error) failureClass {
	if errors.Is(err, errCorruptRow) {
		return failPermanent
	}
	var apiErr *wrike.APIError
	if !errors.As(err, &apiErr) {
		// Network trouble or a malformed response.
		// Retrying is right for the first and harmless for the second thanks to the backoff cap.
		return failTransient
	}
	switch {
	case apiErr.IsAuth():
		return failAuth
	case apiErr.IsWrongHost():
		// The engine pauses and the UI shows the re-auth screen,
		// where verifyToken probes the hosts again and repairs the stored host.
		return failAuth
	case apiErr.IsRateLimit():
		return failTransient
	case apiErr.StatusCode >= 500:
		return failTransient
	default:
		return failPermanent
	}
}

func isNotFound(err error) bool {
	var apiErr *wrike.APIError
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}
