package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"iossa/internal/db/sqlcgen"
	gh "iossa/internal/github"
	"iossa/internal/models"

	"github.com/jackc/pgx/v5"
)

// repoURLPattern matches "owner/repo" after stripping github.com prefix.
var repoURLPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

// customRangePattern matches "YYYY-MM-DD:YYYY-MM-DD".
var customRangePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}:\d{4}-\d{2}-\d{2}$`)

// Analyze is the main orchestrator. Every step is logged so you can follow the
// full pipeline in your terminal. When something fails the log tells you exactly
// which step and why.
func Analyze(
	ctx context.Context,
	queries *sqlcgen.Queries,
	client *gh.Client,
	req models.AnalysisRequest,
) (*models.AnalysisResponse, error) {
	analyzeStart := time.Now()

	// ── Step 1: Parse the repo URL ────────────────────────────────────────────
	owner, name, err := parseRepoURL(req.RepoURL)
	if err != nil {
		slog.Warn("analyzer: invalid repo URL", "raw_input", req.RepoURL, "err", err)
		return nil, err
	}
	fullName := owner + "/" + name

	// ── Step 2: Parse timeframe ───────────────────────────────────────────────
	tf, err := parseTimeframe(req.Timeframe)
	if err != nil {
		slog.Warn("analyzer: invalid timeframe", "timeframe", req.Timeframe, "err", err)
		return nil, err
	}

	slog.Info("analyzer: request received",
		"repo", fullName,
		"timeframe", req.Timeframe,
		"window_from", tf.From.Format("2006-01-02"),
		"window_to", tf.To.Format("2006-01-02"),
	)

	// ── Step 3: Upsert repo row (with Stars=0 placeholder) ───────────────────
	// Stars/forks are 0 here intentionally — real values come from GitHub in
	// step 7. Step 8 (UpdateRepo) writes the real values immediately after.
	slog.Debug("analyzer: upserting repo placeholder in DB", "repo", fullName)
	repo, err := queries.GetOrCreateRepo(ctx, sqlcgen.GetOrCreateRepoParams{
		Owner:       owner,
		Name:        name,
		FullName:    fullName,
		Description: "",
		Stars:       0,
		Forks:       0,
	})
	if err != nil {
		slog.Error("analyzer: DB upsert repo failed", "repo", fullName, "err", err)
		return nil, fmt.Errorf("analyzer: get or create repo: %w", err)
	}
	slog.Debug("analyzer: repo DB record ready", "repo", fullName, "db_id", repo.ID)

	// ── Step 4: Cache check ───────────────────────────────────────────────────
	slog.Debug("analyzer: checking cache", "repo", fullName, "db_id", repo.ID)
	raw, cached, fetchedAt, err := tryCache(ctx, queries, repo.ID, fullName)
	if err != nil {
		return nil, err
	}

	if cached {
		slog.Info("analyzer: CACHE HIT — serving from DB",
			"repo", fullName,
			"fetched_at", fetchedAt.Format(time.RFC3339),
			"age_minutes", int(time.Since(fetchedAt).Minutes()),
		)
		repoInfo := repoInfoFrom(repo)
		sliced := ToWindow(raw, tf.From, tf.To)
		slog.Debug("analyzer: sliced cached snapshot",
			"issues_in_window", len(sliced.Issues),
			"prs_in_window", len(sliced.PRs),
			"commits_in_window", len(sliced.Commits),
		)
		stats, _ := computeStats(sliced, tf)
		signals := ComputeGSoCSignals(raw.Community, raw, -1, stats.PRMergeRate, stats.IssueCloseRate)
		resp := BuildResponse(sliced, repoInfo, tf, signals, true)
		resp.FetchedAt = fetchedAt
		slog.Info("analyzer: done (cached)",
			"repo", fullName,
			"duration_ms", time.Since(analyzeStart).Milliseconds(),
		)
		return resp, nil
	}

	slog.Info("analyzer: CACHE MISS — will fetch from GitHub", "repo", fullName)

	// ── Step 5: Wait for fetch lock (prevents duplicate concurrent fetches) ───
	if err := waitForLock(ctx, queries, repo.ID, fullName); err != nil {
		return nil, err
	}

	// ── Step 6: Acquire fetch lock ────────────────────────────────────────────
	if err := queries.AcquireFetchLock(ctx, sqlcgen.AcquireFetchLockParams{
		RepoID:    repo.ID,
		Timeframe: "90d",
	}); err != nil {
		slog.Warn("analyzer: could not acquire fetch lock — another fetch may have started",
			"repo", fullName, "err", err)
	} else {
		slog.Debug("analyzer: fetch lock acquired", "repo", fullName)
	}
	defer func() {
		if releaseErr := queries.ReleaseFetchLock(context.Background(), sqlcgen.ReleaseFetchLockParams{
			RepoID:    repo.ID,
			Timeframe: "90d",
		}); releaseErr != nil {
			slog.Error("analyzer: failed to release fetch lock", "repo", fullName, "err", releaseErr)
		} else {
			slog.Debug("analyzer: fetch lock released", "repo", fullName)
		}
	}()

	// ── Step 7: Fetch 90 days from GitHub ────────────────────────────────────
	slog.Info("analyzer: calling FetchAll", "repo", fullName)
	fetchStart := time.Now()
	rawSnap, err := gh.FetchAll(ctx, client, owner, name)
	if err != nil {
		slog.Error("analyzer: FetchAll FAILED",
			"repo", fullName,
			"duration_ms", time.Since(fetchStart).Milliseconds(),
			"err", err,
		)
		return nil, fmt.Errorf("analyzer: fetch: %w", err)
	}
	slog.Info("analyzer: FetchAll succeeded",
		"repo", fullName,
		"duration_ms", time.Since(fetchStart).Milliseconds(),
		"issues", len(rawSnap.Issues),
		"prs", len(rawSnap.PRs),
		"commits", len(rawSnap.Commits),
		"stars", rawSnap.Repo.StargazerCount,
		"forks", rawSnap.Repo.ForkCount,
	)

	// ── Step 8: Update repo metadata with real values from GitHub ────────────
	// BUG FIX: this is what stores the correct stars/forks in the DB.
	// Previously if stars=0 appeared in DB it was because this step was
	// being skipped on timeout or error.
	if updateErr := queries.UpdateRepo(ctx, sqlcgen.UpdateRepoParams{
		ID:          repo.ID,
		Description: rawSnap.Repo.Description,
		Stars:       int32(rawSnap.Repo.StargazerCount),
		Forks:       int32(rawSnap.Repo.ForkCount),
	}); updateErr != nil {
		// Non-fatal: we still have real values in memory for the response.
		slog.Warn("analyzer: failed to update repo meta in DB (non-fatal)",
			"repo", fullName,
			"stars", rawSnap.Repo.StargazerCount,
			"err", updateErr,
		)
	} else {
		slog.Debug("analyzer: repo meta updated in DB",
			"repo", fullName,
			"stars", rawSnap.Repo.StargazerCount,
			"forks", rawSnap.Repo.ForkCount,
		)
	}

	// ── Step 9: Save snapshot BEFORE sampling ────────────────────────────────
	// BUG FIX: Old code saved the snapshot AFTER sampling first response times.
	// If the 90s context fired during the sampling REST calls, the snapshot was
	// never saved and data was lost. Now we persist first — even if sampling
	// times out, the core data is safe in the DB.
	slog.Info("analyzer: saving snapshot to DB", "repo", fullName)
	rawJSON, err := json.Marshal(rawSnap)
	if err != nil {
		return nil, fmt.Errorf("analyzer: marshal snapshot: %w", err)
	}
	if upsertErr := queries.UpsertSnapshot(ctx, sqlcgen.UpsertSnapshotParams{
		RepoID:    repo.ID,
		Timeframe: "90d",
		Data:      json.RawMessage(rawJSON),
	}); upsertErr != nil {
		// Non-fatal: we can still return a good response from in-memory data.
		// But log it as ERROR so you know caching is broken.
		slog.Error("analyzer: failed to save snapshot to DB — caching broken for this request",
			"repo", fullName,
			"err", upsertErr,
		)
	} else {
		slog.Info("analyzer: snapshot saved to DB", "repo", fullName)
	}

	// ── Step 10: Sample first-response times (with its own timeout) ──────────
	// BUG FIX: Sampling uses a separate 20s sub-context so it can't blow the
	// whole 90s budget. If sampling times out, we return -1 (no data) and
	// still produce a complete response. The snapshot is already saved (step 9).
	slog.Info("analyzer: sampling first response times (max 20s)", "repo", fullName)
	samplingCtx, samplingCancel := context.WithTimeout(ctx, 20*time.Second)
	defer samplingCancel()
	samplingStart := time.Now()
	avgFirstResponseHrs := sampleFirstResponseHrs(samplingCtx, client, owner, name, rawSnap.Issues)
	slog.Info("analyzer: sampling complete",
		"repo", fullName,
		"avg_first_response_hrs", avgFirstResponseHrs,
		"duration_ms", time.Since(samplingStart).Milliseconds(),
	)

	// ── Step 11: Build and return response ───────────────────────────────────
	repoInfo := models.RepoInfo{
		Owner:       owner,
		Name:        name,
		FullName:    fullName,
		Description: rawSnap.Repo.Description,
		Stars:       rawSnap.Repo.StargazerCount,
		Forks:       rawSnap.Repo.ForkCount,
	}

	sliced := ToWindow(rawSnap, tf.From, tf.To)
	slog.Info("analyzer: window slice complete",
		"repo", fullName,
		"timeframe", req.Timeframe,
		"issues_in_window", len(sliced.Issues),
		"prs_in_window", len(sliced.PRs),
		"commits_in_window", len(sliced.Commits),
	)

	stats, _ := computeStats(sliced, tf)
	signals := ComputeGSoCSignals(rawSnap.Community, rawSnap, avgFirstResponseHrs, stats.PRMergeRate, stats.IssueCloseRate)

	slog.Info("analyzer: analysis complete (fresh fetch)",
		"repo", fullName,
		"total_duration_ms", time.Since(analyzeStart).Milliseconds(),
		"gsoc_score", signals.GSoCFriendlyScore,
		"new_issues", stats.NewIssues,
		"new_prs", stats.NewPRs,
		"merged_prs", stats.MergedPRs,
		"active_contributors", stats.ActiveContributors,
	)

	return BuildResponse(sliced, repoInfo, tf, signals, false), nil
}

// tryCache checks for a fresh 90-day snapshot.
// Returns (nil, false, zero, nil) on cache miss.
// Returns (nil, false, zero, err) only on a real DB error.
func tryCache(ctx context.Context, queries *sqlcgen.Queries, repoID int32, repoName string) (*gh.RawSnapshot, bool, time.Time, error) {
	snap, err := queries.GetSnapshot(ctx, sqlcgen.GetSnapshotParams{
		RepoID:    repoID,
		Timeframe: "90d",
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("analyzer: no snapshot in DB", "repo", repoName)
			return nil, false, time.Time{}, nil
		}
		slog.Error("analyzer: DB error reading snapshot", "repo", repoName, "err", err)
		return nil, false, time.Time{}, fmt.Errorf("analyzer: get snapshot: %w", err)
	}

	age := time.Since(snap.FetchedAt)
	ttl := time.Duration(gh.SnapshotTTLHours) * time.Hour
	slog.Debug("analyzer: snapshot found",
		"repo", repoName,
		"fetched_at", snap.FetchedAt.Format(time.RFC3339),
		"age_minutes", int(age.Minutes()),
		"ttl_hours", gh.SnapshotTTLHours,
		"is_fresh", age < ttl,
	)

	if age >= ttl {
		slog.Info("analyzer: snapshot is STALE — will re-fetch",
			"repo", repoName,
			"age_hours", int(age.Hours()),
			"ttl_hours", gh.SnapshotTTLHours,
		)
		return nil, false, time.Time{}, nil
	}

	var raw gh.RawSnapshot
	if err := json.Unmarshal(snap.Data, &raw); err != nil {
		// Corrupted snapshot — treat as miss so we overwrite with clean data.
		slog.Error("analyzer: snapshot data corrupt — treating as cache miss",
			"repo", repoName, "err", err)
		return nil, false, time.Time{}, nil
	}

	return &raw, true, snap.FetchedAt, nil
}

// waitForLock polls every 2s for up to 30s waiting for another in-progress fetch to finish.
func waitForLock(ctx context.Context, queries *sqlcgen.Queries, repoID int32, repoName string) error {
	for i := 0; i < 15; i++ {
		locked, err := queries.IsFetchLocked(ctx, sqlcgen.IsFetchLockedParams{
			RepoID:    repoID,
			Timeframe: "90d",
		})
		if err != nil {
			return fmt.Errorf("analyzer: check lock: %w", err)
		}
		if !locked {
			return nil
		}
		slog.Info("analyzer: fetch lock is held by another request — waiting 2s",
			"repo", repoName, "attempt", i+1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	slog.Warn("analyzer: lock wait timed out — proceeding anyway", "repo", repoName)
	return nil
}

// sampleFirstResponseHrs samples up to 15 open issues with comments and
// returns the average hours to first comment. Returns -1 if no data.
// Uses its own 20s context so it can't block the whole pipeline.
func sampleFirstResponseHrs(ctx context.Context, client *gh.Client, owner, name string, issues []gh.GHIssue) float64 {
	const maxSample = 15
	var candidates []gh.GHIssue
	for _, issue := range issues {
		if issue.State == "OPEN" && issue.Comments.TotalCount > 0 {
			candidates = append(candidates, issue)
			if len(candidates) >= maxSample {
				break
			}
		}
	}

	slog.Debug("analyzer: first-response sampling",
		"repo", owner+"/"+name,
		"candidates", len(candidates),
	)

	if len(candidates) == 0 {
		return -1
	}

	var totalHrs float64
	var count int
	for _, issue := range candidates {
		if ctx.Err() != nil {
			slog.Warn("analyzer: sampling context cancelled early",
				"sampled", count, "remaining", len(candidates)-count, "err", ctx.Err())
			break
		}
		comment, err := gh.FetchFirstIssueComment(ctx, client, owner, name, issue.Number)
		if err != nil {
			slog.Debug("analyzer: could not fetch comment",
				"issue", issue.Number, "err", err)
			continue
		}
		if comment == nil {
			continue
		}
		if hrs := comment.CreatedAt.Sub(issue.CreatedAt).Hours(); hrs >= 0 {
			totalHrs += hrs
			count++
		}
	}

	if count == 0 {
		return -1
	}
	return totalHrs / float64(count)
}

// repoInfoFrom converts a sqlcgen.Repo DB row to models.RepoInfo.
func repoInfoFrom(repo sqlcgen.Repo) models.RepoInfo {
	return models.RepoInfo{
		Owner:       repo.Owner,
		Name:        repo.Name,
		FullName:    repo.FullName,
		Description: repo.Description,
		Stars:       int(repo.Stars),
		Forks:       int(repo.Forks),
	}
}

// parseRepoURL parses any of these formats into (owner, name):
//
//	https://github.com/owner/repo
//	github.com/owner/repo
//	owner/repo
func parseRepoURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "www.")
	raw = strings.TrimPrefix(raw, "github.com/")
	raw = strings.TrimSuffix(raw, "/")

	if !repoURLPattern.MatchString(raw) {
		return "", "", &ParseError{
			Code:    "INVALID_REPO_URL",
			Message: `invalid GitHub repo format — use "owner/repo" or a full GitHub URL`,
		}
	}
	parts := strings.SplitN(raw, "/", 2)
	return parts[0], parts[1], nil
}

// parseTimeframe converts a timeframe string to a TimeframeInfo with From/To times.
func parseTimeframe(tf string) (models.TimeframeInfo, error) {
	now := time.Now().UTC()
	switch tf {
	case "7d":
		return models.TimeframeInfo{Label: "Last 7 days", From: now.AddDate(0, 0, -7), To: now, Days: 7}, nil
	case "30d":
		return models.TimeframeInfo{Label: "Last 30 days", From: now.AddDate(0, 0, -30), To: now, Days: 30}, nil
	case "90d":
		return models.TimeframeInfo{Label: "Last 90 days", From: now.AddDate(0, 0, -90), To: now, Days: 90}, nil
	}
	if customRangePattern.MatchString(tf) {
		parts := strings.SplitN(tf, ":", 2)
		from, err1 := time.Parse("2006-01-02", parts[0])
		to, err2 := time.Parse("2006-01-02", parts[1])
		if err1 != nil || err2 != nil {
			return models.TimeframeInfo{}, &ParseError{Code: "INVALID_TIMEFRAME", Message: "invalid date in custom range: " + tf}
		}
		to = to.Add(24*time.Hour - time.Second)
		if from.After(to) {
			return models.TimeframeInfo{}, &ParseError{Code: "INVALID_TIMEFRAME", Message: "from date must be before to date"}
		}
		days := int(to.Sub(from).Hours()/24) + 1
		if days > 90 {
			return models.TimeframeInfo{}, &ParseError{Code: "RANGE_TOO_LARGE", Message: "custom range must be 90 days or less"}
		}
		return models.TimeframeInfo{Label: tf, From: from.UTC(), To: to.UTC(), Days: days}, nil
	}
	return models.TimeframeInfo{}, &ParseError{
		Code:    "INVALID_TIMEFRAME",
		Message: `timeframe must be "7d", "30d", "90d", or "YYYY-MM-DD:YYYY-MM-DD"`,
	}
}

// ParseError is a structured validation error with a machine-readable code field.
type ParseError struct {
	Code    string
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
