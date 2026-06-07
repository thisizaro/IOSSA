package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// repoAnalysisQuery fetches repository metadata, issues, and pull requests.
const repoAnalysisQuery = `
query RepoAnalysis(
  $owner: String!
  $name: String!
  $since: DateTime!
  $issuesCursor: String
  $prsCursor: String
) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    description
    stargazerCount
    forkCount
    updatedAt
    hasIssuesEnabled
    codeOfConduct { name }
    beginnerIssues: issues(
      states: [OPEN]
      labels: ["good first issue", "help wanted"]
      first: 1
    ) { totalCount }
    issues(
      first: 100
      after: $issuesCursor
      orderBy: { field: CREATED_AT, direction: DESC }
      filterBy: { since: $since }
    ) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number state createdAt closedAt
        author { login avatarUrl }
        labels(first: 10) { nodes { name } }
        comments { totalCount }
      }
    }
    pullRequests(
      first: 100
      after: $prsCursor
      orderBy: { field: CREATED_AT, direction: DESC }
    ) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number state createdAt closedAt mergedAt
        author { login avatarUrl }
      }
    }
  }
  rateLimit { remaining resetAt cost }
}
`

// FetchRepoGraphQL runs paginated GraphQL fetches until all issues and PRs
// since `since` are collected. Logs every page fetch with counts so you can
// follow pagination progress in real time.
func FetchRepoGraphQL(
	ctx context.Context,
	client *Client,
	owner, name string,
	since time.Time,
) (*GHRepository, []GHIssue, []GHPR, error) {
	var allIssues []GHIssue
	var allPRs []GHPR
	var repoMeta GHRepository
	metaCollected := false

	issuesDone := false
	prsDone := false
	issuesCursor := ""
	prsCursor := ""
	page := 0

	for !issuesDone || !prsDone {
		page++
		vars := map[string]any{
			"owner": owner,
			"name":  name,
			"since": since.UTC().Format(time.RFC3339),
		}
		if issuesCursor != "" {
			vars["issuesCursor"] = issuesCursor
		}
		if prsCursor != "" {
			vars["prsCursor"] = prsCursor
		}

		slog.Info("github: graphql page fetch",
			"repo", owner+"/"+name,
			"page", page,
			"issues_done", issuesDone,
			"prs_done", prsDone,
			"issues_cursor", issuesCursor != "",
			"prs_cursor", prsCursor != "",
		)

		var result struct {
			Repository GHRepository `json:"repository"`
		}
		if err := client.DoGraphQL(ctx, repoAnalysisQuery, vars, &result); err != nil {
			return nil, nil, nil, fmt.Errorf("graphql: fetch page %d: %w", page, err)
		}

		repo := result.Repository

		if !metaCollected {
			repoMeta = repo
			metaCollected = true
			slog.Info("github: repo metadata collected",
				"repo", repo.NameWithOwner,
				"stars", repo.StargazerCount,
				"forks", repo.ForkCount,
				"has_issues", repo.HasIssuesEnabled,
			)
		}

		// Collect issues.
		if !issuesDone {
			allIssues = append(allIssues, repo.Issues.Nodes...)
			if repo.Issues.PageInfo.HasNextPage {
				issuesCursor = repo.Issues.PageInfo.EndCursor
				slog.Debug("github: issues paginating", "total_so_far", len(allIssues))
			} else {
				issuesDone = true
				issuesCursor = ""
				slog.Info("github: issues pagination complete", "total", len(allIssues))
			}
		}

		// Collect PRs, filtering client-side to [since, ∞).
		if !prsDone {
			stopPRs := false
			pageCount := 0
			for _, pr := range repo.PullRequests.Nodes {
				if pr.CreatedAt.Before(since) {
					stopPRs = true
					slog.Debug("github: pr before since window, stopping pagination",
						"pr_number", pr.Number,
						"pr_created_at", pr.CreatedAt.Format(time.RFC3339),
						"since", since.Format(time.RFC3339),
					)
					break
				}
				allPRs = append(allPRs, pr)
				pageCount++
			}
			if stopPRs || !repo.PullRequests.PageInfo.HasNextPage {
				prsDone = true
				prsCursor = ""
				slog.Info("github: prs pagination complete", "total", len(allPRs))
			} else {
				prsCursor = repo.PullRequests.PageInfo.EndCursor
				slog.Debug("github: prs paginating", "total_so_far", len(allPRs))
			}
		}

		slog.Info("github: graphql page complete",
			"repo", owner+"/"+name,
			"page", page,
			"issues_collected", len(allIssues),
			"prs_collected", len(allPRs),
		)
	}

	slog.Info("github: graphql all pages complete",
		"repo", owner+"/"+name,
		"total_pages", page,
		"total_issues", len(allIssues),
		"total_prs", len(allPRs),
	)

	return &repoMeta, allIssues, allPRs, nil
}
