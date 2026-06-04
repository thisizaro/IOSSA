package analyzer

import (
        "context"
        "database/sql"
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
)

// repoURLPattern matches "owner/repo" after stripping github.com prefix.
var repoURLPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

// customRangePattern matches "YYYY-MM-DD:YYYY-MM-DD".
var customRangePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}:\d{4}-\d{2}-\d{2}$`)

// Analyze is the main orchestrator: parses the request, checks the cache,
// fetches fresh data if needed, and returns a full AnalysisResponse.
func Analyze(
        ctx context.Context,
        queries *sqlcgen.Queries,
        client *gh.Client,
        req models.AnalysisRequest,
) (*models.AnalysisResponse, error) {
        // 1. Parse repo URL → owner, name.
        owner, name, err := parseRepoURL(req.RepoURL)
        if err != nil {
                return nil, err
        }

        // 2. Parse timeframe → TimeframeInfo.
        tf, err := parseTimeframe(req.Timeframe)
        if err != nil {
                return nil, err
        }

        // 3. GetOrCreateRepo — we need the DB id for snapshot lookups.
        repo, err := queries.GetOrCreateRepo(ctx, sqlcgen.GetOrCreateRepoParams{
                Owner:    owner,
                Name:     name,
                FullName: owner + "/" + name,
        })
        if err != nil {
                return nil, fmt.Errorf("analyzer: get or create repo: %w", err)
        }

        // 4. Check for a fresh 90-day snapshot.
        raw, cached, fetchedAt, err := tryCache(ctx, queries, repo.ID, tf)
        if err != nil {
                return nil, err
        }

        if cached {
                repoInfo := repoInfoFrom(repo)
                sliced := ToWindow(raw, tf.From, tf.To)
                stats, _ := computeStats(sliced, tf)
                signals := ComputeGSoCSignals(raw.Community, raw, -1, stats.PRMergeRate, stats.IssueCloseRate)
                resp := BuildResponse(sliced, repoInfo, tf, signals, true)
                resp.FetchedAt = fetchedAt
                return resp, nil
        }

        // 5–6. Fetch lock: wait if another goroutine is already fetching.
        if err := waitForLock(ctx, queries, repo.ID); err != nil {
                return nil, err
        }

        // Acquire the fetch lock.
        if err := queries.AcquireFetchLock(ctx, sqlcgen.AcquireFetchLockParams{
                RepoID:    repo.ID,
                Timeframe: "90d",
        }); err != nil {
                return nil, fmt.Errorf("analyzer: acquire lock: %w", err)
        }
        defer func() {
                if err := queries.ReleaseFetchLock(context.Background(), sqlcgen.ReleaseFetchLockParams{
                        RepoID:    repo.ID,
                        Timeframe: "90d",
                }); err != nil {
                        slog.Error("analyzer: release lock", "err", err)
                }
        }()

        // 7. Fetch 90 days of data from GitHub.
        rawSnap, err := gh.FetchAll(ctx, client, owner, name)
        if err != nil {
                return nil, fmt.Errorf("analyzer: fetch all: %w", err)
        }

        // 8. Update repo metadata with fresh values from GitHub.
        if err := queries.UpdateRepo(ctx, sqlcgen.UpdateRepoParams{
                ID:          repo.ID,
                Description: rawSnap.Repo.Description,
                Stars:       int32(rawSnap.Repo.StargazerCount),
                Forks:       int32(rawSnap.Repo.ForkCount),
        }); err != nil {
                slog.Warn("analyzer: update repo meta", "err", err)
        }

        // 9. Sample up to 15 open issues with comments to compute avg first response time.
        avgFirstResponseHrs := sampleFirstResponseHrs(ctx, client, owner, name, rawSnap.Issues)

        // 10. Upsert snapshot.
        rawJSON, err := json.Marshal(rawSnap)
        if err != nil {
                return nil, fmt.Errorf("analyzer: marshal snapshot: %w", err)
        }
        if err := queries.UpsertSnapshot(ctx, sqlcgen.UpsertSnapshotParams{
                RepoID:    repo.ID,
                Timeframe: "90d",
                Data:      json.RawMessage(rawJSON),
        }); err != nil {
                slog.Warn("analyzer: upsert snapshot", "err", err)
        }

        // 11. Slice to requested window and build response.
        repoInfo := models.RepoInfo{
                Owner:       owner,
                Name:        name,
                FullName:    owner + "/" + name,
                Description: rawSnap.Repo.Description,
                Stars:       rawSnap.Repo.StargazerCount,
                Forks:       rawSnap.Repo.ForkCount,
        }

        sliced := ToWindow(rawSnap, tf.From, tf.To)
        stats, _ := computeStats(sliced, tf)
        signals := ComputeGSoCSignals(rawSnap.Community, rawSnap, avgFirstResponseHrs, stats.PRMergeRate, stats.IssueCloseRate)
        return BuildResponse(sliced, repoInfo, tf, signals, false), nil
}

// tryCache checks for a fresh 90-day snapshot. Returns (nil, false, zero, nil) on miss.
func tryCache(ctx context.Context, queries *sqlcgen.Queries, repoID int32, tf models.TimeframeInfo) (*gh.RawSnapshot, bool, time.Time, error) {
        snap, err := queries.GetSnapshot(ctx, sqlcgen.GetSnapshotParams{
                RepoID:    repoID,
                Timeframe: "90d",
        })
        if err != nil {
                if errors.Is(err, sql.ErrNoRows) {
                        return nil, false, time.Time{}, nil
                }
                return nil, false, time.Time{}, fmt.Errorf("analyzer: get snapshot: %w", err)
        }

        if time.Since(snap.FetchedAt) >= gh.SnapshotTTLHours*time.Hour {
                return nil, false, time.Time{}, nil
        }

        var raw gh.RawSnapshot
        if err := json.Unmarshal(snap.Data, &raw); err != nil {
                return nil, false, time.Time{}, fmt.Errorf("analyzer: unmarshal snapshot: %w", err)
        }
        return &raw, true, snap.FetchedAt, nil
}

// waitForLock polls until the fetch lock is released (up to 30 seconds).
func waitForLock(ctx context.Context, queries *sqlcgen.Queries, repoID int32) error {
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
                select {
                case <-ctx.Done():
                        return ctx.Err()
                case <-time.After(2 * time.Second):
                }
        }
        return nil // Give up waiting — proceed and let the fetch compete.
}

// sampleFirstResponseHrs samples up to 15 open issues with comments and
// returns the average hours between issue creation and first comment.
// Returns -1 if no data is available.
func sampleFirstResponseHrs(ctx context.Context, client *gh.Client, owner, name string, issues []gh.GHIssue) float64 {
        const maxSample = 15
        var sampled []gh.GHIssue
        for _, issue := range issues {
                if issue.State == "OPEN" && issue.Comments.TotalCount > 0 {
                        sampled = append(sampled, issue)
                        if len(sampled) >= maxSample {
                                break
                        }
                }
        }
        if len(sampled) == 0 {
                return -1
        }

        var totalHrs float64
        var count int
        for _, issue := range sampled {
                comment, err := gh.FetchFirstIssueComment(ctx, client, owner, name, issue.Number)
                if err != nil || comment == nil {
                        continue
                }
                hrs := comment.CreatedAt.Sub(issue.CreatedAt).Hours()
                if hrs >= 0 {
                        totalHrs += hrs
                        count++
                }
        }
        if count == 0 {
                return -1
        }
        return totalHrs / float64(count)
}

// repoInfoFrom converts a sqlcgen.Repo to models.RepoInfo.
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

// parseRepoURL parses "https://github.com/owner/repo", "github.com/owner/repo",
// or "owner/repo" and returns (owner, name, nil) or an error.
func parseRepoURL(raw string) (string, string, error) {
        raw = strings.TrimSpace(raw)
        raw = strings.TrimPrefix(raw, "https://")
        raw = strings.TrimPrefix(raw, "http://")
        raw = strings.TrimPrefix(raw, "github.com/")

        if !repoURLPattern.MatchString(raw) {
                return "", "", &ParseError{Code: "INVALID_REPO_URL", Message: "cannot parse as GitHub URL: " + raw}
        }
        parts := strings.SplitN(raw, "/", 2)
        return parts[0], parts[1], nil
}

// parseTimeframe parses a timeframe string into a TimeframeInfo.
func parseTimeframe(tf string) (models.TimeframeInfo, error) {
        now := time.Now().UTC()

        switch tf {
        case "7d":
                return models.TimeframeInfo{Label: "7d", From: now.AddDate(0, 0, -7), To: now, Days: 7}, nil
        case "30d":
                return models.TimeframeInfo{Label: "30d", From: now.AddDate(0, 0, -30), To: now, Days: 30}, nil
        case "90d":
                return models.TimeframeInfo{Label: "90d", From: now.AddDate(0, 0, -90), To: now, Days: 90}, nil
        }

        if customRangePattern.MatchString(tf) {
                parts := strings.SplitN(tf, ":", 2)
                from, err1 := time.Parse("2006-01-02", parts[0])
                to, err2 := time.Parse("2006-01-02", parts[1])
                if err1 != nil || err2 != nil {
                        return models.TimeframeInfo{}, &ParseError{Code: "INVALID_TIMEFRAME", Message: "invalid date in custom range: " + tf}
                }
                to = to.Add(24*time.Hour - time.Second) // end of the day
                if from.After(to) {
                        return models.TimeframeInfo{}, &ParseError{Code: "INVALID_TIMEFRAME", Message: "from must be before to"}
                }
                days := int(to.Sub(from).Hours()/24) + 1
                if days > 90 {
                        return models.TimeframeInfo{}, &ParseError{Code: "RANGE_TOO_LARGE", Message: "custom range must be ≤ 90 days"}
                }
                return models.TimeframeInfo{Label: tf, From: from.UTC(), To: to.UTC(), Days: days}, nil
        }

        return models.TimeframeInfo{}, &ParseError{Code: "INVALID_TIMEFRAME", Message: "timeframe must be 7d, 30d, 90d, or YYYY-MM-DD:YYYY-MM-DD"}
}

// ParseError is a structured validation error with a machine-readable code.
type ParseError struct {
        Code    string
        Message string
}

func (e *ParseError) Error() string {
        return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
