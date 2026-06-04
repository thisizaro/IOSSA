package github

import (
        "context"
        "fmt"
        "log/slog"
        "sync"
        "time"
)

// CommunityData holds community health indicators for a repository.
type CommunityData struct {
        HasCodeOfConduct   bool `json:"has_code_of_conduct"`
        BeginnerIssueCount int  `json:"beginner_issue_count"`
        // HasContribGuide is true when the repo contains a CONTRIBUTING.md file.
        HasContribGuide bool `json:"has_contrib_guide"`
}

// RawSnapshot is the normalized event list stored in the database.
// It always covers 90 days of data and is sliced at query time.
type RawSnapshot struct {
        Repo      GHRepository `json:"repo"`
        Issues    []GHIssue    `json:"issues"`
        PRs       []GHPR       `json:"pull_requests"`
        Commits   []GHCommit   `json:"commits"`
        Community CommunityData `json:"community"`
        FetchedAt time.Time    `json:"fetched_at"`
}

// FetchAll fetches 90 days of data for a repository.
// It calls FetchRepoGraphQL and FetchCommits concurrently,
// then checks for CONTRIBUTING.md and builds a RawSnapshot.
func FetchAll(ctx context.Context, client *Client, owner, name string) (*RawSnapshot, error) {
        since := time.Now().UTC().AddDate(0, 0, -90)

        var (
                repo     *GHRepository
                issues   []GHIssue
                prs      []GHPR
                commits  []GHCommit
                gqlErr   error
                commErr  error
        )

        var wg sync.WaitGroup
        wg.Add(2)

        go func() {
                defer wg.Done()
                repo, issues, prs, gqlErr = FetchRepoGraphQL(ctx, client, owner, name, since)
        }()

        go func() {
                defer wg.Done()
                commits, commErr = FetchCommits(ctx, client, owner, name, since)
        }()

        wg.Wait()

        if gqlErr != nil {
                return nil, fmt.Errorf("fetcher: graphql: %w", gqlErr)
        }
        if commErr != nil {
                return nil, fmt.Errorf("fetcher: commits: %w", commErr)
        }

        // Check for CONTRIBUTING.md via REST (404 = not found, non-error).
        contribURL := fmt.Sprintf("%s/repos/%s/%s/contents/CONTRIBUTING.md", RESTBaseURL, owner, name)
        hasContrib := false
        if _, err := client.DoREST(ctx, contribURL, nil); err == nil {
                hasContrib = true
        } else if err != ErrNotFound {
                slog.Warn("fetcher: checking CONTRIBUTING.md", "err", err)
        }

        community := CommunityData{
                HasCodeOfConduct:   repo.CodeOfConduct != nil,
                BeginnerIssueCount: repo.BeginnerIssues.TotalCount,
                HasContribGuide:    hasContrib,
        }

        return &RawSnapshot{
                Repo:      *repo,
                Issues:    issues,
                PRs:       prs,
                Commits:   commits,
                Community: community,
                FetchedAt: time.Now().UTC(),
        }, nil
}
