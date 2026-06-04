package github

import (
        "context"
        "fmt"
        "strings"
        "time"
)

// FetchCommits fetches commits via the REST API since `since`.
// Endpoint: GET /repos/{owner}/{name}/commits?since={RFC3339}&per_page=100
// Paginates via Link header until no next page.
func FetchCommits(ctx context.Context, client *Client, owner, name string, since time.Time) ([]GHCommit, error) {
        var commits []GHCommit
        url := fmt.Sprintf(
                "%s/repos/%s/%s/commits?since=%s&per_page=100",
                RESTBaseURL, owner, name, since.UTC().Format(time.RFC3339),
        )

        for url != "" {
                var page []GHCommit
                resp, err := client.DoREST(ctx, url, &page)
                if err != nil {
                        return nil, fmt.Errorf("rest: fetch commits: %w", err)
                }
                commits = append(commits, page...)
                url = parseLinkNext(resp.Header.Get("Link"))
        }

        return commits, nil
}

// FetchFirstIssueComment fetches the first comment on an issue.
// Endpoint: GET /repos/{owner}/{name}/issues/{number}/comments?per_page=1
// Returns nil if the issue has no comments.
func FetchFirstIssueComment(ctx context.Context, client *Client, owner, name string, issueNumber int) (*GHComment, error) {
        url := fmt.Sprintf(
                "%s/repos/%s/%s/issues/%d/comments?per_page=1",
                RESTBaseURL, owner, name, issueNumber,
        )

        var comments []GHComment
        if _, err := client.DoREST(ctx, url, &comments); err != nil {
                return nil, fmt.Errorf("rest: fetch comment on issue %d: %w", issueNumber, err)
        }

        if len(comments) == 0 {
                return nil, nil
        }
        return &comments[0], nil
}

// parseLinkNext parses the RFC 5988 Link header and returns the "next" URL, or "".
// Example: <https://api.github.com/...?page=2>; rel="next", <...>; rel="last"
func parseLinkNext(link string) string {
        for _, part := range strings.Split(link, ",") {
                part = strings.TrimSpace(part)
                sections := strings.Split(part, ";")
                if len(sections) < 2 {
                        continue
                }
                if strings.TrimSpace(sections[1]) == `rel="next"` {
                        u := strings.TrimSpace(sections[0])
                        u = strings.Trim(u, "<>")
                        return u
                }
        }
        return ""
}
