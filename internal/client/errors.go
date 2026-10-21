// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError represents an HTTP error returned by the Anthropic Admin API.
type APIError struct {
	StatusCode int
	Type       string
	Message    string
	RequestID  string
}

type errorEnvelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Error     struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *APIError) Error() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Anthropic API error (HTTP %d)", e.StatusCode))
	if e.Type != "" {
		parts = append(parts, fmt.Sprintf("[%s]", e.Type))
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.RequestID != "" {
		parts = append(parts, fmt.Sprintf("(request_id: %s)", e.RequestID))
	}
	return strings.Join(parts, ": ")
}

// ParseAPIError constructs an *APIError from an HTTP status code, headers, and raw response body.
func ParseAPIError(statusCode int, headers http.Header, body []byte) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
	}
	if headers != nil {
		apiErr.RequestID = headers.Get("request-id")
		if apiErr.RequestID == "" {
			apiErr.RequestID = headers.Get("x-request-id")
		}
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		apiErr.Message = http.StatusText(statusCode)
		return apiErr
	}

	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && (env.Error.Message != "" || env.Error.Type != "") {
		apiErr.Type = env.Error.Type
		apiErr.Message = env.Error.Message
		if env.RequestID != "" {
			apiErr.RequestID = env.RequestID
		}
		return apiErr
	}

	// Truncate non-JSON fallback bodies to avoid polluting diagnostics.
	const maxRawErrorLen = 512
	if len(trimmed) > maxRawErrorLen {
		trimmed = trimmed[:maxRawErrorLen] + "...(truncated)"
	}
	apiErr.Message = trimmed
	return apiErr
}

// StatusCode extracts the HTTP status code from err if it wraps an *APIError, or returns 0.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

// IsNotFound reports whether err represents an HTTP 404 Not Found response.
func IsNotFound(err error) bool {
	return StatusCode(err) == http.StatusNotFound
}

// IsBadRequest reports whether err represents an HTTP 400 Bad Request response.
func IsBadRequest(err error) bool {
	return StatusCode(err) == http.StatusBadRequest
}

// IsConflict reports whether err represents an HTTP 409 Conflict response.
func IsConflict(err error) bool {
	return StatusCode(err) == http.StatusConflict
}

// IsForbidden reports whether err represents an HTTP 403 Forbidden response.
func IsForbidden(err error) bool {
	return StatusCode(err) == http.StatusForbidden
}
