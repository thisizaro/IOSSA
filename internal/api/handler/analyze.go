package handler

import (
	"context"
	"encoding/json"
	"errors"
	// "fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"iossa/internal/analyzer"
	"iossa/internal/db/sqlcgen"
	gh "iossa/internal/github"
	"iossa/internal/models"
)

var validTimeframe = regexp.MustCompile(`^(7d|30d|90d|\d{4}-\d{2}-\d{2}:\d{4}-\d{2}-\d{2})$`)

// Analyze returns the handler for POST /api/v1/analyze.
func Analyze(queries *sqlcgen.Queries, client *gh.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 90s hard timeout for the entire analysis pipeline.
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()

		// Decode and validate request body.
		var req models.AnalysisRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Warn("handler: invalid JSON body", "err", err)
			writeError(w, http.StatusBadRequest, "invalid JSON body", "INVALID_INPUT")
			return
		}

		if req.RepoURL == "" {
			slog.Warn("handler: missing repo_url")
			writeError(w, http.StatusBadRequest, "repo_url is required", "INVALID_INPUT")
			return
		}
		if req.Timeframe == "" {
			slog.Warn("handler: missing timeframe")
			writeError(w, http.StatusBadRequest, "timeframe is required", "INVALID_INPUT")
			return
		}
		if !validTimeframe.MatchString(req.Timeframe) {
			slog.Warn("handler: invalid timeframe format", "timeframe", req.Timeframe)
			writeError(w, http.StatusUnprocessableEntity,
				`timeframe must be "7d", "30d", "90d", or "YYYY-MM-DD:YYYY-MM-DD"`,
				"INVALID_TIMEFRAME")
			return
		}

		slog.Info("handler: dispatching to analyzer", "repo_url", req.RepoURL, "timeframe", req.Timeframe)

		resp, err := analyzer.Analyze(ctx, queries, client, req)
		if err != nil {
			// Map structured errors to the right HTTP status codes.
			var pe *analyzer.ParseError
			switch {
			case errors.As(err, &pe):
				status := http.StatusUnprocessableEntity
				slog.Warn("handler: parse/validation error",
					"code", pe.Code, "message", pe.Message)
				writeError(w, status, pe.Message, pe.Code)
			case errors.Is(err, gh.ErrNotFound):
				slog.Warn("handler: repo not found on GitHub", "repo_url", req.RepoURL)
				writeError(w, http.StatusNotFound, "repository not found on GitHub", "REPO_NOT_FOUND")
			case errors.Is(err, context.DeadlineExceeded):
				slog.Error("handler: analysis timed out",
					"repo_url", req.RepoURL,
					"timeframe", req.Timeframe,
				)
				writeError(w, http.StatusGatewayTimeout,
					"analysis timed out — the repository may be very large, try again",
					"TIMEOUT")
			default:
				slog.Error("handler: unexpected error from analyzer",
					"repo_url", req.RepoURL,
					"err", err,
				)
				writeError(w, http.StatusInternalServerError, "internal server error", "INTERNAL_ERROR")
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			slog.Error("handler: failed to encode response", "err", encErr)
		}
	}
}

// writeError writes a standard JSON error envelope.
func writeError(w http.ResponseWriter, status int, message, code string) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(models.ErrorResponse{Error: message, Code: code}); err != nil {
		slog.Error("handler: failed to write error response", "err", err)
	}
}
