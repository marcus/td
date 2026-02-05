package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
