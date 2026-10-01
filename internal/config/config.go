// Package config loads Atlas configuration from environment variables.
package config

import (
	"os"
	"time"
)

// Config holds all Atlas configuration values.
type Config struct {
	// HTTPAddr is the address the HTTP server listens on.
	// Env: ATLAS_HTTP_ADDR, default ":8080".
	HTTPAddr string

	// DatabaseURL is the PostgreSQL connection string.
	// Env: ATLAS_DATABASE_URL, default "postgres://atlas:atlas@localhost:5432/atlas?sslmode=disable".
	DatabaseURL string

	// RedisURL is the Redis connection string.
	// Env: ATLAS_REDIS_URL, default "redis://localhost:6379/0".
	RedisURL string

	// StoreType selects the backing store: "memory" or "postgres".
	// Env: ATLAS_STORE, default "memory".
	StoreType string

	// SuspectAfter is the duration after which a missed heartbeat transitions
	// a server to "suspect" status.
	// Env: ATLAS_SUSPECT_AFTER, default 30s.
	SuspectAfter time.Duration

	// OfflineAfter is the duration after which a missed heartbeat transitions
	// a server to "offline" status.
	// Env: ATLAS_OFFLINE_AFTER, default 60s.
	OfflineAfter time.Duration

	// HealthInterval is how often the health monitor sweeps.
	// Env: ATLAS_HEALTH_INTERVAL, default 10s.
	HealthInterval time.Duration
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	cfg := Config{
		HTTPAddr:       envOr("ATLAS_HTTP_ADDR", ":8080"),
		DatabaseURL:    envOr("ATLAS_DATABASE_URL", "postgres://atlas:atlas@localhost:5432/atlas?sslmode=disable"),
		RedisURL:       envOr("ATLAS_REDIS_URL", "redis://localhost:6379/0"),
		StoreType:      envOr("ATLAS_STORE", "memory"),
		SuspectAfter:   30 * time.Second,
		OfflineAfter:   60 * time.Second,
		HealthInterval: 10 * time.Second,
	}

	if v := os.Getenv("ATLAS_SUSPECT_AFTER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SuspectAfter = d
		}
	}
	if v := os.Getenv("ATLAS_OFFLINE_AFTER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.OfflineAfter = d
		}
	}
	if v := os.Getenv("ATLAS_HEALTH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.HealthInterval = d
		}
	}

	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}