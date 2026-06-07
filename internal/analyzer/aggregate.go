package analyzer

import (
	"fmt"
	"iossa/internal/github"
	"iossa/internal/models"
	"sort"
	"time"
)

// BuildResponse computes all statistics from a (possibly sliced) RawSnapshot.
func BuildResponse(
	raw *github.RawSnapshot,
	repoInfo models.RepoInfo,
	tf models.TimeframeInfo,
	signals models.GSoCSignals,
	cached bool,
) *models.AnalysisResponse {
	stats, contributors := computeStats(raw, tf)
	freq := computeFrequency(raw, tf)

	return &models.AnalysisResponse{
		Repo:          repoInfo,
		Timeframe:     tf,
		Cached:        cached,
		FetchedAt:     raw.FetchedAt,
		Stats:         stats,
		Contributors:  contributors,
		GSoCSignals:   signals,
		FrequencyData: freq,
	}
}

func computeStats(raw *github.RawSnapshot, tf models.TimeframeInfo) (models.RepoStats, []models.Contributor) {
	var (
		newIssues, closedIssues, openIssues           int
		newPRs, mergedPRs, openPRs, closedUnmergedPRs int
		issueCloseTimes, prMergeTimes                 []float64
	)

	// Issue stats.
	for _, issue := range raw.Issues {
		newIssues++
		if issue.ClosedAt != nil {
			closedIssues++
			issueCloseTimes = append(issueCloseTimes, issue.ClosedAt.Sub(issue.CreatedAt).Hours())
		} else {
			openIssues++
		}
	}

	// PR stats.
	for _, pr := range raw.PRs {
		newPRs++
		if pr.MergedAt != nil {
			mergedPRs++
			prMergeTimes = append(prMergeTimes, pr.MergedAt.Sub(pr.CreatedAt).Hours())
		} else if pr.ClosedAt != nil {
			closedUnmergedPRs++
		} else {
			openPRs++
		}
	}

	issueCloseRate := ratio(closedIssues, newIssues)
	prMergeRate := ratio(mergedPRs, newPRs)

	stats := models.RepoStats{
		TotalCommits:           len(raw.Commits),
		NewIssues:              newIssues,
		ClosedIssues:           closedIssues,
		OpenIssues:             openIssues,
		NewPRs:                 newPRs,
		MergedPRs:              mergedPRs,
		OpenPRs:                openPRs,
		ClosedUnmergedPRs:      closedUnmergedPRs,
		IssueCloseRate:         issueCloseRate,
		PRMergeRate:            prMergeRate,
		AvgPRMergeTimeHours:    mean(prMergeTimes),
		AvgIssueCloseTimeHours: mean(issueCloseTimes),
	}

	contributors := buildContributors(raw)
	stats.ActiveContributors = len(contributors)

	return stats, contributors
}

// buildContributors aggregates per-contributor activity and returns top 20 by score.
func buildContributors(raw *github.RawSnapshot) []models.Contributor {
	type entry struct {
		models.Contributor
	}
	m := make(map[string]*models.Contributor)

	get := func(login, avatarURL string) *models.Contributor {
		if c, ok := m[login]; ok {
			return c
		}
		c := &models.Contributor{
			Login:      login,
			AvatarURL:  avatarURL,
			ProfileURL: fmt.Sprintf("https://github.com/%s", login),
		}
		m[login] = c
		return c
	}

	for _, commit := range raw.Commits {
		if commit.Author == nil {
			continue
		}
		c := get(commit.Author.Login, commit.Author.AvatarUrl)
		c.Commits++
	}

	for _, pr := range raw.PRs {
		if pr.Author == nil {
			continue
		}
		c := get(pr.Author.Login, pr.Author.AvatarUrl)
		c.PRsOpened++
		if pr.MergedAt != nil {
			c.PRsMerged++
		}
	}

	for _, issue := range raw.Issues {
		if issue.Author == nil {
			continue
		}
		c := get(issue.Author.Login, issue.Author.AvatarUrl)
		c.IssuesOpened++
		if issue.ClosedAt != nil {
			c.IssuesClosed++
		}
	}

	// Compute TotalScore and collect into slice.
	result := make([]models.Contributor, 0, len(m))
	for _, c := range m {
		c.TotalScore = c.Commits*1 + c.PRsOpened*3 + c.PRsMerged*2 + c.IssuesOpened*1 + c.IssuesClosed*2
		result = append(result, *c)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalScore > result[j].TotalScore
	})

	if len(result) > 20 {
		result = result[:20]
	}

	return result
}

// computeFrequency builds weekly event counts bucketed by ISO week start (Monday).
func computeFrequency(raw *github.RawSnapshot, tf models.TimeframeInfo) models.FrequencyData {
	issuesOpened := make(map[string]int)
	issuesClosed := make(map[string]int)
	prsOpened := make(map[string]int)
	prsMerged := make(map[string]int)

	for _, issue := range raw.Issues {
		issuesOpened[isoWeek(issue.CreatedAt)]++
		if issue.ClosedAt != nil {
			issuesClosed[isoWeek(*issue.ClosedAt)]++
		}
	}
	for _, pr := range raw.PRs {
		prsOpened[isoWeek(pr.CreatedAt)]++
		if pr.MergedAt != nil {
			prsMerged[isoWeek(*pr.MergedAt)]++
		}
	}

	weeks := allWeeks(tf.From, tf.To)

	return models.FrequencyData{
		IssuesOpenedPerWeek: toWeeklyCounts(weeks, issuesOpened),
		IssuesClosedPerWeek: toWeeklyCounts(weeks, issuesClosed),
		PRsOpenedPerWeek:    toWeeklyCounts(weeks, prsOpened),
		PRsMergedPerWeek:    toWeeklyCounts(weeks, prsMerged),
	}
}

// isoWeek returns the Monday of the ISO week containing t, formatted YYYY-MM-DD.
func isoWeek(t time.Time) string {
	return mondayOf(t).Format("2006-01-02")
}

// mondayOf returns the Monday of the week containing t (UTC).
func mondayOf(t time.Time) time.Time {
	t = t.UTC()
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7 // Sunday → 7
	}
	mon := t.AddDate(0, 0, -(wd - 1))
	return time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.UTC)
}

// allWeeks returns all ISO week starts (Mondays) that overlap [from, to].
func allWeeks(from, to time.Time) []string {
	var weeks []string
	cur := mondayOf(from)
	end := mondayOf(to)
	for !cur.After(end) {
		weeks = append(weeks, cur.Format("2006-01-02"))
		cur = cur.AddDate(0, 0, 7)
	}
	return weeks
}

// toWeeklyCounts converts a week→count map to a sorted []WeeklyCount for the given weeks.
func toWeeklyCounts(weeks []string, m map[string]int) []models.WeeklyCount {
	out := make([]models.WeeklyCount, len(weeks))
	for i, w := range weeks {
		out[i] = models.WeeklyCount{WeekStart: w, Count: m[w]}
	}
	return out
}

// ratio computes numerator/max(denominator,1) as float64.
func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

// mean returns the arithmetic mean of a float64 slice, or 0 if empty.
func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
