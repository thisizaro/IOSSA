// Package analyzer provides repository statistics computation.
package analyzer

import (
	// "fmt"
	"iossa/internal/github"
	"time"
)

// ToWindow filters a RawSnapshot to only include events within [from, to].
// Returns a new RawSnapshot with only matching items.
//   - Issues: included if createdAt is in [from, to].
//   - PRs: included if createdAt is in [from, to].
//   - Commits: included if authored date is in [from, to].
func ToWindow(raw *github.RawSnapshot, from, to time.Time) *github.RawSnapshot {
	sliced := &github.RawSnapshot{
		Repo:      raw.Repo,
		Community: raw.Community,
		FetchedAt: raw.FetchedAt,
	}

	for _, issue := range raw.Issues {
		if inWindow(issue.CreatedAt, from, to) {
			sliced.Issues = append(sliced.Issues, issue)
		}
	}

	for _, pr := range raw.PRs {
		if inWindow(pr.CreatedAt, from, to) {
			sliced.PRs = append(sliced.PRs, pr)
		}
	}

	for _, commit := range raw.Commits {
		if inWindow(commit.Commit.Author.Date, from, to) {
			sliced.Commits = append(sliced.Commits, commit)
		}
	}

	return sliced
}

// inWindow returns true if t is within [from, to] (inclusive).
func inWindow(t, from, to time.Time) bool {
	return !t.Before(from) && !t.After(to)
}
