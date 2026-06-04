// Package github provides GitHub API client functionality.
package github

// Label strings used for beginner-friendly issue filtering.
const (
	LabelGoodFirstIssue = "good first issue"
	LabelHelpWanted     = "help wanted"
)

// GitHub API endpoints.
const (
	GraphQLEndpoint = "https://api.github.com/graphql"
	RESTBaseURL     = "https://api.github.com"
)

// Snapshot TTL: 6 hours before a 90-day snapshot is considered stale.
const SnapshotTTLHours = 6

// FetchLockTTLMinutes is how long a fetch lock is considered active.
const FetchLockTTLMinutes = 5
