package github

import (
        "context"
        "fmt"
        "time"
)

// repoAnalysisQuery fetches repository metadata, issues, and pull requests.
// Issues are filtered server-side by $since; PRs are filtered client-side.
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
// since `since` are collected. Returns repo metadata and all collected nodes.
//
// Pagination: loops while issues.pageInfo.hasNextPage OR prs.pageInfo.hasNextPage.
// PRs are filtered client-side (createdAt >= since); pagination stops when
// a PR's createdAt falls before `since`.
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

        for !issuesDone || !prsDone {
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

                var result struct {
                        Repository GHRepository `json:"repository"`
                }
                if err := client.DoGraphQL(ctx, repoAnalysisQuery, vars, &result); err != nil {
                        return nil, nil, nil, fmt.Errorf("graphql: fetch page: %w", err)
                }

                repo := result.Repository

                // Capture metadata once from the first page.
                if !metaCollected {
                        repoMeta = repo
                        metaCollected = true
                }

                // Collect issues (only while not done; avoid re-adding from repeated first pages).
                if !issuesDone {
                        allIssues = append(allIssues, repo.Issues.Nodes...)
                        if repo.Issues.PageInfo.HasNextPage {
                                issuesCursor = repo.Issues.PageInfo.EndCursor
                        } else {
                                issuesDone = true
                                issuesCursor = ""
                        }
                }

                // Collect PRs, filtering client-side to [since, ∞).
                if !prsDone {
                        stopPRs := false
                        for _, pr := range repo.PullRequests.Nodes {
                                if pr.CreatedAt.Before(since) {
                                        stopPRs = true
                                        break
                                }
                                allPRs = append(allPRs, pr)
                        }
                        if stopPRs || !repo.PullRequests.PageInfo.HasNextPage {
                                prsDone = true
                                prsCursor = ""
                        } else {
                                prsCursor = repo.PullRequests.PageInfo.EndCursor
                        }
                }
        }

        return &repoMeta, allIssues, allPRs, nil
}
