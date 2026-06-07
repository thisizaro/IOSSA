package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	// "fmt"
	"net/http"
	"strings"
	"time"

	"iossa/internal/db/sqlcgen"
	gh "iossa/internal/github"
)

// Status returns a handler for GET /api/v1/status?repo=owner/repo.
func Status(queries *sqlcgen.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		repoParam := r.URL.Query().Get("repo")
		if repoParam == "" {
			writeError(w, http.StatusBadRequest, "missing repo query param", "INVALID_INPUT")
			return
		}

		parts := strings.SplitN(repoParam, "/", 2)
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "repo must be owner/repo", "INVALID_INPUT")
			return
		}
		fullName := parts[0] + "/" + parts[1]

		// Look up repo by full name (read-only — do NOT upsert here).
		repo, err := queries.GetRepoByFullName(r.Context(), fullName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				json.NewEncoder(w).Encode(map[string]any{"cached": false}) //nolint:errcheck
				return
			}
			writeError(w, http.StatusInternalServerError, "db error", "INTERNAL_ERROR")
			return
		}

		snap, err := queries.GetSnapshot(r.Context(), sqlcgen.GetSnapshotParams{
			RepoID:    repo.ID,
			Timeframe: "90d",
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				json.NewEncoder(w).Encode(map[string]any{"cached": false}) //nolint:errcheck
				return
			}
			writeError(w, http.StatusInternalServerError, "db error", "INTERNAL_ERROR")
			return
		}

		ttl := gh.SnapshotTTLHours * time.Hour
		remaining := ttl - time.Since(snap.FetchedAt)
		if remaining < 0 {
			remaining = 0
		}

		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"cached":             true,
			"fetched_at":         snap.FetchedAt.UTC().Format(time.RFC3339),
			"expires_in_seconds": int(remaining.Seconds()),
			"fresh":              remaining > 0,
		})
	}
}
