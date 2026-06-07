package analyzer

import (
	// "fmt"
	"iossa/internal/github"
	"iossa/internal/models"
	"math"
	"time"
)

// ComputeGSoCSignals builds the GSoCSignals struct.
// prMergeRate and issueCloseRate are computed from the sliced window and passed in.
// avgFirstResponseHrs is computed in analyzer.go via sampled REST calls; pass -1 if unavailable.
func ComputeGSoCSignals(
	community github.CommunityData,
	raw *github.RawSnapshot,
	avgFirstResponseHrs float64,
	prMergeRate float64,
	issueCloseRate float64,
) models.GSoCSignals {
	// RecentlyActive: any commit in the last 7 days (uses full 90-day snapshot, not windowed).
	sevenDaysAgo := time.Now().UTC().AddDate(0, 0, -7)
	recentlyActive := false
	for _, c := range raw.Commits {
		if c.Commit.Author.Date.After(sevenDaysAgo) {
			recentlyActive = true
			break
		}
	}

	responsiveEnough := avgFirstResponseHrs >= 0 && avgFirstResponseHrs < 72

	// MentorActivityScore: 5 binary components, each worth 0.2.
	mentorScore := 0.0
	if community.HasContribGuide {
		mentorScore += 0.2
	}
	if community.HasCodeOfConduct {
		mentorScore += 0.2
	}
	if recentlyActive {
		mentorScore += 0.2
	}
	if community.BeginnerIssueCount > 0 {
		mentorScore += 0.2
	}
	if responsiveEnough {
		mentorScore += 0.2
	}

	// GSoCFriendlyScore (0–100).
	beginnerFraction := math.Min(float64(community.BeginnerIssueCount), 20) / 20.0
	raw100 := mentorScore*40 + prMergeRate*30 + beginnerFraction*20 + issueCloseRate*10
	gsocScore := math.Round(raw100*10) / 10 // 1 decimal place

	return models.GSoCSignals{
		BeginnerIssues:      community.BeginnerIssueCount,
		AvgFirstResponseHrs: avgFirstResponseHrs,
		MentorActivityScore: mentorScore,
		HasContribGuide:     community.HasContribGuide,
		HasCodeOfConduct:    community.HasCodeOfConduct,
		RecentlyActive:      recentlyActive,
		GSoCFriendlyScore:   gsocScore,
	}
}
