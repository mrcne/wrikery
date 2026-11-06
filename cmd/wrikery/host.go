package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// apiHosts are the hosts probeHost tries when the config leaves host unset.
// The order decides only the order of the hosts in a probeError.
var apiHosts = []string{wrike.DefaultHost, wrike.EUHost, wrike.US2Host}

// errNoDataCenter is returned when every host in the list answered 300,
// so the token belongs to none of the data centers this build knows about.
var errNoDataCenter = errors.New("no Wrike data center accepted this token")

// probeError is the probe's answer when no host accepted the token and not every host answered 300:
// each host with what it answered, so a reader sees which host did not answer and which one said the account is elsewhere.
type probeError struct {
	hosts []string
	errs  map[string]error
}

func (e *probeError) Error() string {
	parts := make([]string, 0, len(e.hosts))
	for _, host := range e.hosts {
		parts = append(parts, host+": "+outcome(e.errs[host]))
	}
	return strings.Join(parts, ", ")
}

// outcome is one host's answer in a few words: an API answer by its status, a request error by its cause alone,
// since the probe error names the host and the request error repeats it with the path.
func outcome(err error) string {
	var apiErr *wrike.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.IsWrongHost():
			return "answered 300, another data center"
		}
		s := fmt.Sprintf("http %d", apiErr.StatusCode)
		if apiErr.Code != "" {
			s += " " + apiErr.Code
		}
		if apiErr.Description != "" {
			s += ": " + apiErr.Description
		}
		return s
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err.Error()
	}
	return err.Error()
}

// probeHost asks every host at the same time and returns the first one that accepts the token.
// hc is optional: a nil value leaves the client on its own default transport.
// Tests pass one that routes to a fake server instead of the real Wrike hosts.
// The hosts are asked at once because a host that does not answer, a firewall that drops it or a slow route,
// must not cost the next host its turn: asked one after another, the first host's timeout was the whole probe.
// A host from the wrong data center answers 300 to every request, see wrike.APIError.IsWrongHost.
// A 401 is returned as it arrives: Wrike answers 300, not 401, to a valid token of another data center,
// so a 401 is about the token whatever the other hosts would say, and a host that hangs must not hold it back.
// Otherwise the verdict waits for every answer: 300 from every host is errNoDataCenter,
// and anything else is a probeError naming each host's answer.
func probeHost(ctx context.Context, token string, hosts []string, hc *http.Client) (string, wrike.Contact, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type answer struct {
		host string
		me   wrike.Contact
		err  error
	}
	// Buffered to the number of hosts, so the goroutines of an early return finish on their own.
	answers := make(chan answer, len(hosts))
	for _, host := range hosts {
		go func() {
			var opts []wrike.Option
			if hc != nil {
				opts = append(opts, wrike.WithHTTPClient(hc))
			}
			me, err := newClient(wrike.StaticToken(token), host, opts...).Me(ctx)
			answers <- answer{host: host, me: me, err: err}
		}()
	}
	errs := make(map[string]error, len(hosts))
	for range hosts {
		a := <-answers
		if a.err == nil {
			return a.host, a.me, nil
		}
		var apiErr *wrike.APIError
		if errors.As(a.err, &apiErr) && apiErr.IsAuth() {
			return "", wrike.Contact{}, a.err
		}
		errs[a.host] = a.err
	}
	wrong := 0
	for _, host := range hosts {
		var apiErr *wrike.APIError
		if errors.As(errs[host], &apiErr) && apiErr.IsWrongHost() {
			wrong++
		}
	}
	if wrong == len(hosts) {
		return "", wrike.Contact{}, errNoDataCenter
	}
	return "", wrike.Contact{}, &probeError{hosts: hosts, errs: errs}
}
