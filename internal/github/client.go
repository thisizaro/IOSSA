package github

import (
        "bytes"
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
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

// NewClient creates a Client with a 30-second timeout.
func NewClient(pool *TokenPool) *Client {
        return &Client{
                http: &http.Client{Timeout: 30 * time.Second},
                pool: pool,
        }
}

// DoGraphQL executes a GraphQL query against the GitHub API.
// Automatically selects a token, sets auth headers, decodes the response,
// and updates the token pool from rateLimit fields in the response.
// Retries once on 429 or transient 5xx after exponential backoff.
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
                return fmt.Errorf("client: do request: %w", err)
        }
        defer resp.Body.Close()

        if (resp.StatusCode == 429 || resp.StatusCode >= 500) && attemptsLeft > 1 {
                io.Copy(io.Discard, resp.Body) //nolint:errcheck
                time.Sleep(2 * time.Second)
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
                return fmt.Errorf("client: decode response: %w", err)
        }

        if len(raw.Errors) > 0 {
                return fmt.Errorf("client: graphql error: %s (type=%s)", raw.Errors[0].Message, raw.Errors[0].Type)
        }

        // Extract rateLimit to update token pool.
        var withRL struct {
                RateLimit RateLimitInfo `json:"rateLimit"`
        }
        if err := json.Unmarshal(raw.Data, &withRL); err == nil && withRL.RateLimit.Remaining > 0 {
                c.pool.UpdateFromGraphQL(token, withRL.RateLimit.Remaining, withRL.RateLimit.ResetAt)
        }

        if err := json.Unmarshal(raw.Data, dest); err != nil {
                return fmt.Errorf("client: unmarshal data: %w", err)
        }

        return nil
}

// DoREST executes a REST GET request against the GitHub API.
// Updates the token pool from X-RateLimit-Remaining and X-RateLimit-Reset headers.
// Returns the response (headers accessible) with the body already read and closed.
// If dest is non-nil, the response body is decoded into it.
func (c *Client) DoREST(ctx context.Context, url string, dest any) (*http.Response, error) {
        token, err := c.pool.GetToken()
        if err != nil {
                return nil, fmt.Errorf("client: get token: %w", err)
        }

        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
                return nil, fmt.Errorf("client: create request: %w", err)
        }
        req.Header.Set("Authorization", "Bearer "+token)
        req.Header.Set("Accept", "application/vnd.github+json")
        req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

        resp, err := c.http.Do(req)
        if err != nil {
                return nil, fmt.Errorf("client: do request: %w", err)
        }
        defer resp.Body.Close()

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
                io.Copy(io.Discard, resp.Body) //nolint:errcheck
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
