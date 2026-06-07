package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	rateLimitRequests = 50
	rateLimitWindow   = 60 * time.Second
	cleanupInterval   = 5 * time.Minute
)

type ipEntry struct {
	count       int
	windowStart time.Time
}

// ipRateLimiter holds per-IP rate limit state.
type ipRateLimiter struct {
	entries sync.Map
}

var limiter = &ipRateLimiter{}

func init() {
	go limiter.cleanup()
}

func (l *ipRateLimiter) cleanup() {
	for range time.Tick(cleanupInterval) {
		now := time.Now()
		l.entries.Range(func(k, v any) bool {
			e := v.(*ipEntry)
			if now.Sub(e.windowStart) > rateLimitWindow {
				l.entries.Delete(k)
			}
			return true
		})
	}
}

// RateLimit is a per-IP middleware that allows rateLimitRequests per rateLimitWindow.
// Responds 429 with Retry-After header when the limit is exceeded.
func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()

		val, _ := limiter.entries.LoadOrStore(ip, &ipEntry{windowStart: now})
		e := val.(*ipEntry)

		// Use a simple atomic-style check; good enough for a single-instance MVP.
		if now.Sub(e.windowStart) > rateLimitWindow {
			e.count = 0
			e.windowStart = now
		}
		e.count++

		if e.count > rateLimitRequests {
			retryAfter := int(rateLimitWindow.Seconds() - now.Sub(e.windowStart).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `{"error":"rate limit exceeded, retry in %d seconds","code":"RATE_LIMITED"}`, retryAfter)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		return ip[:idx]
	}
	return ip
}
