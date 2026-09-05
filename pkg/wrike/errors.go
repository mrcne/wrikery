package wrike

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// APIError is any non-2xx answer from the Wrike API.
// Code and Description carry the API's own error fields when the body was parseable.
type APIError struct {
	StatusCode  int
	Code        string
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("wrike: http %d", e.StatusCode)
	}
	return fmt.Sprintf("wrike: %s (http %d): %s", e.Code, e.StatusCode, e.Description)
}

func (e *APIError) IsAuth() bool      { return e.StatusCode == http.StatusUnauthorized }
func (e *APIError) IsRateLimit() bool { return e.StatusCode == http.StatusTooManyRequests }
func (e *APIError) IsNotFound() bool  { return e.StatusCode == http.StatusNotFound }

func parseAPIError(status int, header http.Header, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status}
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
