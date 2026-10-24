package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want failureClass
	}{
		// A plain error is what a response that does not decode gives, it may be the payload's fault.
		{"plain error", errors.New("decoding PUT /tasks/T1 response: unexpected EOF"), failTransient},
		{"wrapped plain error", fmt.Errorf("get: %w", errors.New("timeout")), failTransient},
		{"no answer", &url.Error{Op: "Put", URL: "https://x/tasks/T1", Err: errors.New("connection refused")}, failUnavailable},
		{"wrapped no answer", fmt.Errorf("update: %w", &url.Error{Op: "Get", URL: "https://x", Err: errors.New("dial tcp")}), failUnavailable},
		{"deadline", context.DeadlineExceeded, failUnavailable},
		{"cancelled", context.Canceled, failUnavailable},
		{"401", &wrike.APIError{StatusCode: 401, Code: "not_authorized"}, failAuth},
		{"300 wrong host", &wrike.APIError{StatusCode: 300}, failAuth},
		{"429", &wrike.APIError{StatusCode: 429, Code: "rate_limit_exceeded"}, failUnavailable},
		{"502", &wrike.APIError{StatusCode: 502}, failUnavailable},
		{"503", &wrike.APIError{StatusCode: 503}, failUnavailable},
		{"504", &wrike.APIError{StatusCode: 504}, failUnavailable},
		{"500", &wrike.APIError{StatusCode: 500, Code: "server_error"}, failTransient},
		{"400", &wrike.APIError{StatusCode: 400, Code: "invalid_request"}, failPermanent},
		{"403", &wrike.APIError{StatusCode: 403, Code: "access_forbidden"}, failPermanent},
		{"404", &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}, failPermanent},
		{"wrapped api error", fmt.Errorf("update: %w", &wrike.APIError{StatusCode: 403}), failPermanent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify(c.err); got != c.want {
				t.Errorf("classify(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
