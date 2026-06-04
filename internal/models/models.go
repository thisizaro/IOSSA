// Package models defines domain structs shared across the application.
package models

import "time"

// AnalysisRequest is the request body for POST /api/v1/analyze.
type AnalysisRequest struct {
	RepoURL   string `json:"repo_url"`
	Timeframe string `json:"timeframe"` // "7d", "30d", "90d", or "YYYY-MM-DD:YYYY-MM-DD"
}

// AnalysisResponse is the full response for a repository analysis.
type AnalysisResponse struct {
	Repo          RepoInfo      `json:"repo"`
	Timeframe     TimeframeInfo `json:"timeframe"`
	Cached        bool          `json:"cached"`
	FetchedAt     time.Time     `json:"fetched_at"`
	Stats         RepoStats     `json:"stats"`
	Contributors  []Contributor `json:"contributors"`
	GSoCSignals   GSoCSignals   `json:"gsoc_signals"`
	FrequencyData FrequencyData `json:"frequency_data"`
}

// RepoInfo holds basic repository metadata.
type RepoInfo struct {
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Forks       int    `json:"forks"`
}

// TimeframeInfo describes the analysis window.
type TimeframeInfo struct {
	Label string    `json:"label"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Days  int       `json:"days"`
}

// RepoStats holds computed statistics over the analysis window.
type RepoStats struct {
	TotalCommits           int     `json:"total_commits"`
	NewIssues              int     `json:"new_issues"`
	ClosedIssues           int     `json:"closed_issues"`
	OpenIssues             int     `json:"open_issues"`
	NewPRs                 int     `json:"new_prs"`
	MergedPRs              int     `json:"merged_prs"`
	OpenPRs                int     `json:"open_prs"`
	ClosedUnmergedPRs      int     `json:"closed_unmerged_prs"`
	IssueCloseRate         float64 `json:"issue_close_rate"`
	PRMergeRate            float64 `json:"pr_merge_rate"`
	AvgPRMergeTimeHours    float64 `json:"avg_pr_merge_time_hours"`
	AvgIssueCloseTimeHours float64 `json:"avg_issue_close_time_hours"`
	ActiveContributors     int     `json:"active_contributors"`
}

// Contributor holds per-contributor activity statistics.
type Contributor struct {
	Login        string `json:"login"`
	AvatarURL    string `json:"avatar_url"`
	ProfileURL   string `json:"profile_url"`
	Commits      int    `json:"commits"`
	PRsOpened    int    `json:"prs_opened"`
	PRsMerged    int    `json:"prs_merged"`
	IssuesOpened int    `json:"issues_opened"`
	IssuesClosed int    `json:"issues_closed"`
	TotalScore   int    `json:"total_score"`
}

// GSoCSignals holds signals relevant to Google Summer of Code / LFX applicants.
type GSoCSignals struct {
	BeginnerIssues      int     `json:"beginner_issues"`
	AvgFirstResponseHrs float64 `json:"avg_first_response_hrs"` // -1 if no data
	MentorActivityScore float64 `json:"mentor_activity_score"`  // 0.0–1.0
	HasContribGuide     bool    `json:"has_contrib_guide"`
	HasCodeOfConduct    bool    `json:"has_code_of_conduct"`
	RecentlyActive      bool    `json:"recently_active"`
	GSoCFriendlyScore   float64 `json:"gsoc_friendly_score"` // 0–100
}

// FrequencyData holds weekly event counts for charting.
type FrequencyData struct {
	IssuesOpenedPerWeek []WeeklyCount `json:"issues_opened_per_week"`
	IssuesClosedPerWeek []WeeklyCount `json:"issues_closed_per_week"`
	PRsOpenedPerWeek    []WeeklyCount `json:"prs_opened_per_week"`
	PRsMergedPerWeek    []WeeklyCount `json:"prs_merged_per_week"`
}

// WeeklyCount represents an event count for a single ISO week.
type WeeklyCount struct {
	WeekStart string `json:"week_start"` // "YYYY-MM-DD" (Monday)
	Count     int    `json:"count"`
}

// ErrorResponse is the standard error envelope for all API errors.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
