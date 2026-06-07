package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// ErrNotFound is returned by DoREST when the server responds with 404.
var ErrNotFound = errors.New("not found")

// Client is a single HTTP client used for both GraphQL and REST GitHub API calls.
type Client struct {
	http *http.Client
	pool *TokenPool
}

// NewClient creates a Client.
// BUG FIX: old timeout was 30s — not enough for slow GitHub responses on large repos.
// We now use 60s per individual HTTP call. The outer request context (90s) still
// acts as the hard ceiling for the entire analysis pipeline.
func NewClient(pool *TokenPool) *Client {
	return &Client{
		http: &http.Client{Timeout: 60 * time.Second},
		pool: pool,
	}
}

// DoGraphQL executes a GraphQL query against the GitHub API.
// Logs every request with timing so you can see exactly how long each GitHub call takes.
// Retries once on 429 or transient 5xx after a short backoff.
func (c *Client) DoGraphQL(ctx context.Context, query string, variables map[string]any, dest any) error {
	token, err := c.pool.GetToken()
	if err != nil {
		return fmt.Errorf("client: get token: %w", err)
	}

	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("client: marshal request: %w", err)
	}

	return c.doGraphQLWithRetry(ctx, token, body, dest, 2)
}

func (c *Client) doGraphQLWithRetry(ctx context.Context, token string, body []byte, dest any, attemptsLeft int) error {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GraphQLEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("client: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Log the error with timing so you know if it was a timeout or a network issue.
		slog.Error("github: graphql request failed",
			"duration_ms", time.Since(start).Milliseconds(),
			"attempts_left", attemptsLeft,
			"err", err,
		)
		return fmt.Errorf("client: do request: %w", err)
	}
	defer resp.Body.Close()

	slog.Debug("github: graphql response",
		"status", resp.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if (resp.StatusCode == 429 || resp.StatusCode >= 500) && attemptsLeft > 1 {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		backoff := 3 * time.Second
		slog.Warn("github: graphql retrying after error",
			"status", resp.StatusCode,
			"backoff_s", backoff.Seconds(),
			"attempts_left", attemptsLeft-1,
		)
		time.Sleep(backoff)
		return c.doGraphQLWithRetry(ctx, token, body, dest, attemptsLeft-1)
	}

	var raw struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("client: decode response (status=%d): %w", resp.StatusCode, err)
	}

	if len(raw.Errors) > 0 {
		slog.Error("github: graphql returned errors",
			"first_error", raw.Errors[0].Message,
			"type", raw.Errors[0].Type,
			"total_errors", len(raw.Errors),
		)
		return fmt.Errorf("client: graphql error: %s (type=%s)", raw.Errors[0].Message, raw.Errors[0].Type)
	}

	// Extract rateLimit to update token pool.
	// BUG FIX: old code skipped update when remaining==0 (`> 0` check).
	// Now we always update so we correctly detect exhaustion.
	var withRL struct {
		RateLimit RateLimitInfo `json:"rateLimit"`
	}
	if err := json.Unmarshal(raw.Data, &withRL); err == nil {
		c.pool.UpdateFromGraphQL(token, withRL.RateLimit.Remaining, withRL.RateLimit.ResetAt)
		slog.Debug("github: rate limit status",
			"remaining", withRL.RateLimit.Remaining,
			"cost", withRL.RateLimit.Cost,
			"reset_at", withRL.RateLimit.ResetAt.Format(time.RFC3339),
		)
	}

	if err := json.Unmarshal(raw.Data, dest); err != nil {
		return fmt.Errorf("client: unmarshal data: %w", err)
	}

	return nil
}

// DoREST executes a REST GET request against the GitHub API.
// Logs every request with URL and timing.
func (c *Client) DoREST(ctx context.Context, url string, dest any) (*http.Response, error) {
	token, err := c.pool.GetToken()
	if err != nil {
		return nil, fmt.Errorf("client: get token: %w", err)
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("client: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		slog.Error("github: rest request failed",
			"url", url,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return nil, fmt.Errorf("client: do request: %w", err)
	}
	defer resp.Body.Close()

	slog.Debug("github: rest response",
		"url", url,
		"status", resp.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	// Update rate limit from response headers.
	if rs := resp.Header.Get("X-RateLimit-Remaining"); rs != "" {
		if remaining, err := strconv.Atoi(rs); err == nil {
			if rt := resp.Header.Get("X-RateLimit-Reset"); rt != "" {
				if resetUnix, err := strconv.ParseInt(rt, 10, 64); err == nil {
					c.pool.UpdateFromRESTHeaders(token, remaining, resetUnix)
				}
			}
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return resp, ErrNotFound
	}

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		slog.Error("github: rest error response",
			"url", url,
			"status", resp.StatusCode,
			"body", string(bodyBytes),
		)
		return resp, fmt.Errorf("client: HTTP %d: %s", resp.StatusCode, url)
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return resp, fmt.Errorf("client: decode response: %w", err)
		}
	} else {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
	}

	return resp, nil
}
