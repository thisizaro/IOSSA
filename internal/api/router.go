// Package api wires up the HTTP router and middleware stack.
package api

import (
        "net/http"

        "github.com/go-chi/chi/v5"
        chimiddleware "github.com/go-chi/chi/v5/middleware"
        "iossa/internal/api/handler"
        "iossa/internal/api/middleware"
        "iossa/internal/db/sqlcgen"
        gh "iossa/internal/github"
)

// NewRouter creates and returns the application HTTP handler.
func NewRouter(queries *sqlcgen.Queries, client *gh.Client) http.Handler {
        r := chi.NewRouter()

        r.Use(middleware.RequestLogger)
        r.Use(middleware.CORS)
        r.Use(chimiddleware.Recoverer)
        r.Use(chimiddleware.RequestID)

        r.Get("/health", handler.Health())
        r.Method("POST", "/api/v1/analyze", middleware.RateLimit(handler.Analyze(queries, client)))
        r.Get("/api/v1/status", handler.Status(queries))

        // Serve frontend static files for everything else.
        r.Handle("/*", http.FileServer(http.Dir("./frontend")))

        return r
}
