// Package config loads application configuration from environment variables.
package config

import (
        "log"
        "os"
        "strings"
)

// Config holds all application configuration.
type Config struct {
        Port          string
        DatabaseURL   string
        GithubTokens  []string
        Env           string
        AllowedOrigin string
}

// Load reads configuration from environment variables.
// Calls log.Fatal if any required variable is missing.
//
// PORT           → default "6000"
// DATABASE_URL   → required
// GH_TOKENS      → required, comma-separated list of PATs
// ENV            → default "development"
// ALLOWED_ORIGIN → default "*"
func Load() *Config {
        cfg := &Config{
                Port:          getEnv("PORT", "6000"),
                DatabaseURL:   requireEnv("DATABASE_URL"),
                Env:           getEnv("ENV", "development"),
                AllowedOrigin: getEnv("ALLOWED_ORIGIN", "*"),
        }

        tokensRaw := requireEnv("GH_TOKENS")
        for _, t := range strings.Split(tokensRaw, ",") {
                t = strings.TrimSpace(t)
                if t != "" {
                        cfg.GithubTokens = append(cfg.GithubTokens, t)
                }
        }

        if len(cfg.GithubTokens) == 0 {
                log.Fatal("GH_TOKENS must contain at least one non-empty token")
        }

        return cfg
}

func getEnv(key, fallback string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return fallback
}

func requireEnv(key string) string {
        v := os.Getenv(key)
        if v == "" {
                log.Fatalf("required environment variable %q is not set", key)
        }
        return v
}
