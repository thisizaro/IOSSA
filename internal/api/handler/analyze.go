package handler

import (
        "context"
        "encoding/json"
        "errors"
        "net/http"
        "regexp"
        "time"

        "iossa/internal/analyzer"
        "iossa/internal/db/sqlcgen"
        gh "iossa/internal/github"
        "iossa/internal/models"
)

var validTimeframe = regexp.MustCompile(`^(7d|30d|90d|\d{4}-\d{2}-\d{2}:\d{4}-\d{2}-\d{2})$`)

// Analyze returns a handler for POST /api/v1/analyze.
func Analyze(queries *sqlcgen.Queries, client *gh.Client) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")

                ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
                defer cancel()

                var req models.AnalysisRequest
                if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                        writeError(w, http.StatusBadRequest, "invalid JSON body", "INVALID_INPUT")
                        return
                }

                if req.RepoURL == "" {
                        writeError(w, http.StatusBadRequest, "repo_url is required", "INVALID_INPUT")
                        return
                }
                if req.Timeframe == "" {
                        writeError(w, http.StatusBadRequest, "timeframe is required", "INVALID_INPUT")
                        return
                }
                if !validTimeframe.MatchString(req.Timeframe) {
                        writeError(w, http.StatusUnprocessableEntity, "timeframe must be 7d, 30d, 90d, or YYYY-MM-DD:YYYY-MM-DD", "INVALID_TIMEFRAME")
                        return
                }

                resp, err := analyzer.Analyze(ctx, queries, client, req)
                if err != nil {
                        var pe *analyzer.ParseError
                        if errors.As(err, &pe) {
                                status := http.StatusUnprocessableEntity
                                if pe.Code == "INVALID_REPO_URL" {
                                        status = http.StatusUnprocessableEntity
                                }
                                writeError(w, status, pe.Message, pe.Code)
                                return
                        }
                        if errors.Is(err, gh.ErrNotFound) {
                                writeError(w, http.StatusNotFound, "repository not found on GitHub", "REPO_NOT_FOUND")
                                return
                        }
                        if errors.Is(err, context.DeadlineExceeded) {
                                writeError(w, http.StatusGatewayTimeout, "analysis timed out", "INTERNAL_ERROR")
                                return
                        }
                        writeError(w, http.StatusInternalServerError, "internal server error", "INTERNAL_ERROR")
                        return
                }

                w.WriteHeader(http.StatusOK)
                json.NewEncoder(w).Encode(resp) //nolint:errcheck
        }
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message, code string) {
        w.WriteHeader(status)
        json.NewEncoder(w).Encode(models.ErrorResponse{Error: message, Code: code}) //nolint:errcheck
}

