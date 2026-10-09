package wrike

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// APIError is any non-2xx answer from the Wrike API.
// Code and Description carry the API's own error fields when the body was parseable.
// A body that is not JSON but one short line of plain text, a load balancer's "no healthy upstream" for example,
// goes into Description on its own, since that line is the whole explanation.
// Method and Path name the request, since the API's own text rarely does and a log line is all a reader gets.
type APIError struct {
	StatusCode  int
	Code        string
	Description string
	RetryAfter  time.Duration
	Method      string
	Path        string
}

// Error renders the API's own error code and description when the response body carried them,
// or a generic message keyed off the status code otherwise, after the request when it is known.
func (e *APIError) Error() string {
	prefix := "wrike: "
	if e.Method != "" || e.Path != "" {
		prefix += strings.TrimSpace(e.Method+" "+e.Path) + ": "
	}
	if e.Code == "" {
		if e.StatusCode == http.StatusMultipleChoices {
			// The docs do not describe this response, this wording is from observing it:
			// Wrike answers 300 with an empty body when the token's account lives in another data center.
			return prefix + "http 300, the account is served by another data center"
		}
		msg := fmt.Sprintf("%shttp %d", prefix, e.StatusCode)
		if e.StatusCode == http.StatusBadGateway || e.StatusCode == http.StatusServiceUnavailable || e.StatusCode == http.StatusGatewayTimeout {
			// An answer of the API itself carries error and errorDescription for every 4XX and 5XX status (https://developers.wrike.com/errors/),
			// so a 502, 503 or 504 without them comes from the edge in front of it, and a reader would otherwise suspect the request or the token.
			msg += ", Wrike is unavailable"
		}
		if e.Description != "" {
			msg += ": " + e.Description
		}
		return msg
	}
	return fmt.Sprintf("%s%s (http %d): %s", prefix, e.Code, e.StatusCode, e.Description)
}

// IsAuth is true when the token was rejected as invalid or expired.
func (e *APIError) IsAuth() bool { return e.StatusCode == http.StatusUnauthorized }

// IsRateLimit is true when Wrike throttled the request, see RetryAfter for how long to wait.
func (e *APIError) IsRateLimit() bool { return e.StatusCode == http.StatusTooManyRequests }

// IsNotFound is true when the requested entity does not exist or is not visible to the token.
func (e *APIError) IsNotFound() bool { return e.StatusCode == http.StatusNotFound }

// IsWrongHost is true when the token's account lives in a different Wrike data center than the host this client used,
// see the observation cited on Error above.
func (e *APIError) IsWrongHost() bool { return e.StatusCode == http.StatusMultipleChoices }

func parseAPIError(method, path string, status int, header http.Header, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status, Method: method, Path: path}
	var payload struct {
		Code        string `json:"error"`
		Description string `json:"errorDescription"`
	}
	if json.Unmarshal(body, &payload) == nil {
		apiErr.Code = payload.Code
		apiErr.Description = payload.Description
	} else if text := strings.TrimSpace(string(body)); shortText(text) {
		apiErr.Description = text
	}
	if secs, err := strconv.Atoi(header.Get("Retry-After")); err == nil && secs > 0 {
		apiErr.RetryAfter = time.Duration(secs) * time.Second
	}
	return apiErr
}

// shortText is true for one line of plain text short enough to print after the status: not HTML, not binary, not a page.
func shortText(s string) bool {
	if s == "" || len(s) > 100 || strings.HasPrefix(s, "<") || !utf8.ValidString(s) {
		return false
	}
	return strings.IndexFunc(s, unicode.IsControl) < 0
}
