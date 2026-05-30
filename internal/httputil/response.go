// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// JSON writes a JSON-encoded response with the given status code.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// ClientError writes a JSON error response with the given status and message.
func ClientError(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

// ServerError logs the error and writes a 500 JSON response.
func ServerError(w http.ResponseWriter, logger *slog.Logger, err error) {
	logger.Error("internal error", "error", err)
	ClientError(w, http.StatusInternalServerError, "internal server error")
}
