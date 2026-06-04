// Package handler provides HTTP request handlers for the IOSSA API.
package handler

import (
	"encoding/json"
	"net/http"
	"time"
)

// Health returns a handler for GET /health.
// Returns a simple status object. No DB or GitHub calls — always 200.
// Used by Cloud Run health checks.
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"status":    "ok",
			"version":   "0.1.0",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}
