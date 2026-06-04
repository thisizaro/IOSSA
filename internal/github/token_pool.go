package github

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type tokenState struct {
	token     string
	remaining int
	resetAt   time.Time
	mu        sync.Mutex
}

// TokenPool manages GitHub PATs with rate-limit awareness.
// Thread-safe. Uses rateLimit fields returned by every GraphQL response.
//
// Rotation policy:
//   - Round-robin across all tokens
//   - Skip tokens with remaining < 200
//   - If ALL tokens have remaining < 200, sleep until earliest resetAt then retry
//   - Log a warning when remaining drops below 500 for any token
type TokenPool struct {
	tokens  []*tokenState
	current int64 // atomic counter for round-robin
}

// NewTokenPool creates a TokenPool from a list of PAT strings.
func NewTokenPool(tokens []string) *TokenPool {
	states := make([]*tokenState, len(tokens))
	for i, t := range tokens {
		states[i] = &tokenState{
			token:     t,
			remaining: 5000,
			resetAt:   time.Now().Add(time.Hour),
		}
	}
	return &TokenPool{tokens: states}
}

// GetToken returns an available token using round-robin, skipping depleted ones.
func (p *TokenPool) GetToken() (string, error) {
	n := len(p.tokens)
	if n == 0 {
		return "", fmt.Errorf("token_pool: no tokens configured")
	}

	// Try all tokens in round-robin order.
	var earliest time.Time
	for attempt := 0; attempt < n; attempt++ {
		idx := int(atomic.AddInt64(&p.current, 1)-1) % n
		ts := p.tokens[idx]
		ts.mu.Lock()
		remaining := ts.remaining
		resetAt := ts.resetAt
		ts.mu.Unlock()

		if remaining >= 200 {
			return ts.token, nil
		}
		if earliest.IsZero() || resetAt.Before(earliest) {
			earliest = resetAt
		}
	}

	// All tokens exhausted — sleep until earliest reset.
	sleepDur := time.Until(earliest)
	if sleepDur > 0 {
		slog.Warn("all GitHub tokens exhausted, sleeping until reset",
			"sleep_seconds", int(sleepDur.Seconds()),
			"reset_at", earliest,
		)
		time.Sleep(sleepDur)
	}

	// Reset all token states after sleep and return first token.
	for _, ts := range p.tokens {
		ts.mu.Lock()
		ts.remaining = 5000
		ts.mu.Unlock()
	}
	idx := int(atomic.AddInt64(&p.current, 1)-1) % n
	return p.tokens[idx].token, nil
}

// UpdateFromGraphQL updates a token's rate limit state from GraphQL response data.
func (p *TokenPool) UpdateFromGraphQL(token string, remaining int, resetAt time.Time) {
	for _, ts := range p.tokens {
		if ts.token == token {
			ts.mu.Lock()
			ts.remaining = remaining
			ts.resetAt = resetAt
			if remaining < 500 {
				slog.Warn("GitHub token rate limit low",
					"remaining", remaining,
					"reset_at", resetAt,
				)
			}
			ts.mu.Unlock()
			return
		}
	}
}

// UpdateFromRESTHeaders updates a token's rate limit state from REST response headers.
func (p *TokenPool) UpdateFromRESTHeaders(token string, remaining int, resetUnix int64) {
	p.UpdateFromGraphQL(token, remaining, time.Unix(resetUnix, 0))
}
