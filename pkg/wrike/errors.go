package wrike

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIError is any non-2xx answer from the Wrike API.
// Code and Description carry the API's own error fields when the body was parseable.
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
		return fmt.Sprintf("%shttp %d", prefix, e.StatusCode)
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
	}
	if secs, err := strconv.Atoi(header.Get("Retry-After")); err == nil && secs > 0 {
		apiErr.RetryAfter = time.Duration(secs) * time.Second
	}
	return apiErr
}
