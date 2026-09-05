package syncer

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want failureClass
	}{
		{"network error", errors.New("dial tcp: connection refused"), failTransient},
		{"wrapped network error", fmt.Errorf("get: %w", errors.New("timeout")), failTransient},
		{"401", &wrike.APIError{StatusCode: 401, Code: "not_authorized"}, failAuth},
		{"429", &wrike.APIError{StatusCode: 429, Code: "rate_limit_exceeded"}, failTransient},
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
