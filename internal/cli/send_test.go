package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// taskAnswer is what Wrike sends back for a task update or create, the envelope the client decodes.
const taskAnswer = `{"kind":"tasks","data":[{"id":"%s","title":"%s","status":"%s","customStatusId":"%s","importance":"Normal",
"responsibleIds":[],"parentIds":["PROJ1"],"createdDate":"2026-09-08T10:00:00Z","updatedDate":"2026-10-03T12:00:00Z",
"permalink":"https://www.wrike.com/open.htm?id=9"}]}`

// withNetwork gives env a token, a host and a client that talks to handler. A nil handler is a closed port.
func withNetwork(t *testing.T, env Env, handler http.Handler) Env {
	t.Helper()
	srv := httptest.NewServer(handler)
	if handler == nil {
		srv.Close()
	} else {
		t.Cleanup(srv.Close)
	}
	env.Token = "test-token"
	env.Host = func(ctx context.Context, token string) (string, error) { return "stub", nil }
	env.Client = func(token, host string) *wrike.Client { return wrike.New(token, wrike.WithBaseURL(srv.URL)) }
	env.LockFile = filepath.Join(t.TempDir(), "sync.lock")
	return env
}
