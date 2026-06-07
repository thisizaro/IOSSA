package middleware

import (
	// "fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger logs every request with method, path, status, duration, and IP.
// In development this gives you a clear line per request so you can see the full
// timeline of what's happening. In production the structured fields are queryable
// in Cloud Logging.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// Log the incoming request BEFORE processing so you can see it arrive
		// in your terminal even if the handler hangs.
		slog.Info("→ request received",
			"method", r.Method,
			"path", r.URL.Path,
			"ip", realIP(r),
			"request_id", chimiddleware.GetReqID(r.Context()),
		)

		next.ServeHTTP(ww, r)

		duration := time.Since(start)
		level := slog.LevelInfo
		// Warn on slow requests (over 10 seconds) and error on 5xx.
		if ww.Status() >= 500 {
			level = slog.LevelError
		} else if duration > 10*time.Second {
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "← request complete",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", duration.Milliseconds(),
			"ip", realIP(r),
			"request_id", chimiddleware.GetReqID(r.Context()),
		)
	})
}

// realIP extracts the real client IP from proxy headers or RemoteAddr.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.Split(xff, ",")[0]
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}
