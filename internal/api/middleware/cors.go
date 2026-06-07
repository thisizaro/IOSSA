// Package middleware provides HTTP middleware for the IOSSA API.
package middleware

import (
	// "fmt"
	"net/http"
	"os"
	// "time"
)

// CORS returns a middleware that sets Cross-Origin Resource Sharing headers.
// In development (ENV != "production"), all origins are allowed.
// In production, only ALLOWED_ORIGIN is permitted.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := "*"
		if os.Getenv("ENV") == "production" {
			if ao := os.Getenv("ALLOWED_ORIGIN"); ao != "" {
				origin = ao
			}
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
