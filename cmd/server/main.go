// Command server is the IOSSA HTTP server entry point.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"iossa/internal/api"
	"iossa/internal/config"
	"iossa/internal/db"
	"iossa/internal/db/sqlcgen"
	gh "iossa/internal/github"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func main() {
	// 1. Load configuration.
	_ = godotenv.Load() // Load .env file if it exists, but ignore errors.
	cfg := config.Load()

	slog.Info("starting IOSSA", "port", cfg.Port, "env", cfg.Env, "tokens", len(cfg.GithubTokens))

	// 2. Connect to database and run migrations.
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool); err != nil {
		slog.Error("db migrations failed", "err", err)
		os.Exit(1)
	}
	slog.Info("database ready")

	// 3. Create sqlc query layer (stdlib adapter bridges pgxpool → database/sql DBTX).
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	queries := sqlcgen.New(sqlDB)

	// 4. Create GitHub client with token pool.
	tokenPool := gh.NewTokenPool(cfg.GithubTokens)
	client := gh.NewClient(tokenPool)

	// 5. Build HTTP router.
	router := api.NewRouter(queries, client)

	// 6–7. Start HTTP server.
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// 8–9. Graceful shutdown on SIGTERM / SIGINT.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	slog.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	slog.Info("server stopped")
}
