package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// apiHosts are the hosts probeHost tries in order when the config leaves host unset.
var apiHosts = []string{wrike.DefaultHost, wrike.EUHost}

// errNoDataCenter is returned when every host in the list answered 300,
// so the token belongs to none of the data centers this build knows about.
var errNoDataCenter = errors.New("no Wrike data center accepted this token")

// probeHost tries each host in turn and returns the first one that accepts the token.
// hc is optional: a nil value leaves the client on its own default transport.
// Tests pass one that routes to a fake server instead of the real Wrike hosts.
// A host from the wrong data center answers 300 to every request, see wrike.APIError.IsWrongHost,
// so that error moves on to the next host.
// Any other error, including a 401, comes back at once:
// it says something about the token, not about the host, and trying another host would not change it.
func probeHost(ctx context.Context, token string, hosts []string, hc *http.Client) (string, wrike.Contact, error) {
	for _, host := range hosts {
		var opts []wrike.Option
		if hc != nil {
			opts = append(opts, wrike.WithHTTPClient(hc))
		}
		me, err := newClient(token, host, opts...).Me(ctx)
		if err == nil {
			return host, me, nil
		}
		var apiErr *wrike.APIError
		if errors.As(err, &apiErr) && apiErr.IsWrongHost() {
			continue
		}
		return "", wrike.Contact{}, err
	}
	return "", wrike.Contact{}, errNoDataCenter
}
