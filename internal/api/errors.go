package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Error code constants for structured API error responses.
const (
	ErrCodeBadRequest             = "bad_request"
	ErrCodeNotFound               = "not_found"
	ErrCodeInternal               = "internal"
	ErrCodeUnauthorized           = "unauthorized"
	ErrCodeForbidden              = "forbidden"
	ErrCodeInsufficientAdminScope = "insufficient_admin_scope"
	ErrCodeRateLimited            = "rate_limited"
	ErrCodeSignupDisabled         = "signup_disabled"
	ErrCodeExpired                = "expired"
	ErrCodeAlreadyUsed            = "already_used"
	ErrCodeNoEvents               = "no_events"
	ErrCodeNotImplemented         = "not_implemented"
	// New codes for sections 9.5, 10
	ErrCodeProjectDeleted      = "project_deleted"
	ErrCodeSnapshotUnavailable = "snapshot_unavailable"
	ErrCodeExportTooLarge      = "export_too_large"
	ErrCodeInvalidQuery        = "invalid_query"
)

// APIError represents a structured error returned by the API.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse wraps an APIError for JSON serialization.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// writeError writes a JSON error response with the given HTTP status code.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(ErrorResponse{
		Error: APIError{Code: code, Message: message},
	}); err != nil {
		slog.Error("write error response", "err", err)
	}
}

// writeJSON writes a JSON response with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("write json response", "err", err)
	}
}
