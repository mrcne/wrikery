package syncer

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/mrcne/wrikery/pkg/wrike"
)

type failureClass int

const (
	// failTransient errors are retried later with the row's own backoff: a 500 and an answer that does not decode,
	// either can be the payload's fault, so the row waits longer each time and a person can discard it.
	failTransient failureClass = iota
	// failAuth pauses the engine until the user refreshes the token.
	failAuth
	// failPermanent rejections will not succeed by waiting. An outbox row goes to the failed state, a scope or a task thread is skipped for the cycle with a log line.
	failPermanent
	// failUnavailable is Wrike not answering: no answer at all, a 429 that outlived the client's own retries, or the edge's 502, 503 or 504.
	// It is not the row's fault, so the row keeps no backoff of its own and the engine's reconnect wait paces the next try,
	// which sends the queue in order the moment Wrike answers again.
	failUnavailable
)

func classify(err error) failureClass {
	if errors.Is(err, errCorruptRow) {
		return failPermanent
	}
	var apiErr *wrike.APIError
	if !errors.As(err, &apiErr) {
		// A url.Error from the http client and a context deadline are net errors, the request got no answer.
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return failUnavailable
		}
		// A malformed response. The engine cannot tell an answer garbled once from one this payload earns every time,
		// and a row in backoff holds the later writes on its entity.
		// It shows on the changes screen while it retries, so a person can discard it.
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
		return failUnavailable
	case apiErr.StatusCode == http.StatusBadGateway || apiErr.StatusCode == http.StatusServiceUnavailable || apiErr.StatusCode == http.StatusGatewayTimeout:
		return failUnavailable
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

// isNotAllowed is the 403 an account answers for a feature its plan does not have, not_allowed in the API's words.
func isNotAllowed(err error) bool {
	var apiErr *wrike.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden
}
