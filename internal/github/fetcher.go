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
	HasContribGuide    bool `json:"has_contrib_guide"`
}

// RawSnapshot is the normalized event list stored in the database.
// It always covers 90 days of data and is sliced at query time.
type RawSnapshot struct {
	Repo      GHRepository  `json:"repo"`
	Issues    []GHIssue     `json:"issues"`
	PRs       []GHPR        `json:"pull_requests"`
	Commits   []GHCommit    `json:"commits"`
	Community CommunityData `json:"community"`
	FetchedAt time.Time     `json:"fetched_at"`
}

// FetchAll fetches 90 days of data for a repository.
// Runs GraphQL (issues+PRs+meta) and REST commits concurrently for speed.
// Logs every step so you can see exactly where time is spent.
func FetchAll(ctx context.Context, client *Client, owner, name string) (*RawSnapshot, error) {
	since := time.Now().UTC().AddDate(0, 0, -90)
	fullName := owner + "/" + name

	slog.Info("github: starting FetchAll",
		"repo", fullName,
		"since", since.Format("2006-01-02"),
	)
	fetchStart := time.Now()

	var (
		repo    *GHRepository
		issues  []GHIssue
		prs     []GHPR
		commits []GHCommit
		gqlErr  error
		commErr error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: GraphQL — repo metadata + issues + PRs.
	go func() {
		defer wg.Done()
		slog.Info("github: starting graphql fetch", "repo", fullName)
		gqlStart := time.Now()
		repo, issues, prs, gqlErr = FetchRepoGraphQL(ctx, client, owner, name, since)
		if gqlErr != nil {
			slog.Error("github: graphql fetch failed",
				"repo", fullName,
				"duration_ms", time.Since(gqlStart).Milliseconds(),
				"err", gqlErr,
			)
		} else {
			slog.Info("github: graphql fetch complete",
				"repo", fullName,
				"issues", len(issues),
				"prs", len(prs),
				"stars", repo.StargazerCount,
				"duration_ms", time.Since(gqlStart).Milliseconds(),
			)
		}
	}()

	// Goroutine 2: REST commits.
	go func() {
		defer wg.Done()
		slog.Info("github: starting commits fetch", "repo", fullName)
		commStart := time.Now()
		commits, commErr = FetchCommits(ctx, client, owner, name, since)
		if commErr != nil {
			slog.Error("github: commits fetch failed",
				"repo", fullName,
				"duration_ms", time.Since(commStart).Milliseconds(),
				"err", commErr,
			)
		} else {
			slog.Info("github: commits fetch complete",
				"repo", fullName,
				"commits", len(commits),
				"duration_ms", time.Since(commStart).Milliseconds(),
			)
		}
	}()

	wg.Wait()

	if gqlErr != nil {
		return nil, fmt.Errorf("fetcher: graphql: %w", gqlErr)
	}
	if commErr != nil {
		return nil, fmt.Errorf("fetcher: commits: %w", commErr)
	}

	// Check for CONTRIBUTING.md via REST (404 = not found, not an error).
	slog.Debug("github: checking CONTRIBUTING.md", "repo", fullName)
	contribURL := fmt.Sprintf("%s/repos/%s/%s/contents/CONTRIBUTING.md", RESTBaseURL, owner, name)
	hasContrib := false
	if _, err := client.DoREST(ctx, contribURL, nil); err == nil {
		hasContrib = true
		slog.Debug("github: CONTRIBUTING.md found", "repo", fullName)
	} else if err != ErrNotFound {
		slog.Warn("github: CONTRIBUTING.md check error", "repo", fullName, "err", err)
	}

	community := CommunityData{
		HasCodeOfConduct:   repo.CodeOfConduct != nil,
		BeginnerIssueCount: repo.BeginnerIssues.TotalCount,
		HasContribGuide:    hasContrib,
	}

	slog.Info("github: FetchAll complete",
		"repo", fullName,
		"total_duration_ms", time.Since(fetchStart).Milliseconds(),
		"issues", len(issues),
		"prs", len(prs),
		"commits", len(commits),
		"beginner_issues", community.BeginnerIssueCount,
		"has_contrib_guide", community.HasContribGuide,
		"has_coc", community.HasCodeOfConduct,
	)

	return &RawSnapshot{
		Repo:      *repo,
		Issues:    issues,
		PRs:       prs,
		Commits:   commits,
		Community: community,
		FetchedAt: time.Now().UTC(),
	}, nil
}
