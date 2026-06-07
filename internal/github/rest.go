package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// FetchCommits fetches commits via the REST API since `since`, paginating via Link header.
// Logs each page so you can see how many pages it takes for a given repo.
func FetchCommits(ctx context.Context, client *Client, owner, name string, since time.Time) ([]GHCommit, error) {
	var commits []GHCommit
	url := fmt.Sprintf(
		"%s/repos/%s/%s/commits?since=%s&per_page=100",
		RESTBaseURL, owner, name, since.UTC().Format(time.RFC3339),
	)
	page := 0
	for url != "" {
		page++
		slog.Debug("github: fetching commits page",
			"repo", owner+"/"+name,
			"page", page,
			"so_far", len(commits),
		)
		var batch []GHCommit
		resp, err := client.DoREST(ctx, url, &batch)
		if err != nil {
			return nil, fmt.Errorf("rest: fetch commits page %d: %w", page, err)
		}
		commits = append(commits, batch...)
		url = parseLinkNext(resp.Header.Get("Link"))
	}
	slog.Debug("github: commits fetch done",
		"repo", owner+"/"+name,
		"total_pages", page,
		"total_commits", len(commits),
	)
	return commits, nil
}

// FetchFirstIssueComment fetches only the first comment on an issue.
// Returns nil if the issue has no comments.
func FetchFirstIssueComment(ctx context.Context, client *Client, owner, name string, issueNumber int) (*GHComment, error) {
	url := fmt.Sprintf(
		"%s/repos/%s/%s/issues/%d/comments?per_page=1",
		RESTBaseURL, owner, name, issueNumber,
	)
	var comments []GHComment
	if _, err := client.DoREST(ctx, url, &comments); err != nil {
		return nil, fmt.Errorf("rest: fetch first comment on issue #%d: %w", issueNumber, err)
	}
	if len(comments) == 0 {
		return nil, nil
	}
	return &comments[0], nil
}

// parseLinkNext parses the RFC 5988 Link header and returns the "next" URL, or "".
// Example header: <https://api.github.com/...?page=2>; rel="next", <...>; rel="last"
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
