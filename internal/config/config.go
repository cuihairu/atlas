// Package config loads Atlas configuration from environment variables.
package config

import (
	"os"
	"time"
)

// Config holds all Atlas configuration values.
type Config struct {
	// HTTPAddr is the address the public API listens on (Discovery / Directory).
	// Env: ATLAS_HTTP_ADDR, default ":8080".
	HTTPAddr string

	// RegistryAddr is the address the Registry API listens on (server
	// registration / heartbeat / unregister). Should be bound to the internal
	// network interface only.
	// Env: ATLAS_REGISTRY_ADDR, default ":8081".
	RegistryAddr string

	// AdminAddr is the address the Admin API listens on (lifecycle / stats /
	// search / migrations). Should be bound to the management network only.
	// Env: ATLAS_ADMIN_ADDR, default ":8082".
	AdminAddr string

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

	// AdminAPIKeys is a comma-separated list of valid API keys for Admin
	// endpoints. Empty means Admin API is open (development only).
	// Env: ATLAS_ADMIN_API_KEYS, default "".
	AdminAPIKeys string

	// AdminIPWhitelist is a comma-separated list of CIDR ranges allowed to
	// call Admin endpoints. Empty means no IP restriction.
	// Env: ATLAS_ADMIN_IP_WHITELIST, default "".
	AdminIPWhitelist string

	// CORSAllowedOrigins is a comma-separated list of allowed origins for
	// CORS. Empty means no CORS headers. Use "*" for development.
	// Env: ATLAS_CORS_ORIGINS, default "".
	CORSAllowedOrigins string

	// RegistryTokens is a comma-separated list of valid service tokens for
	// Registry endpoints (register/heartbeat/unregister). Game servers on the
	// internal network must present one of these tokens. Empty means Registry
	// is open (development only).
	// Env: ATLAS_REGISTRY_TOKENS, default "".
	RegistryTokens string

	// RegistryIPWhitelist is a comma-separated list of CIDR ranges allowed to
	// call Registry endpoints. Empty means no IP restriction (useful when the
	// gateway already restricts access).
	// Env: ATLAS_REGISTRY_IP_WHITELIST, default "".
	RegistryIPWhitelist string

	// EventAdapter selects the transport for character index events
	// (docs/sync.md §4): "http" (synchronous in-process, the v0.1 behavior)
	// or "redis" (Redis Streams with a consumer group).
	// Env: ATLAS_EVENT_ADAPTER, default "http".
	EventAdapter string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	cfg := Config{
		HTTPAddr:            envOr("ATLAS_HTTP_ADDR", ":8080"),
		RegistryAddr:        envOr("ATLAS_REGISTRY_ADDR", ":8081"),
		AdminAddr:           envOr("ATLAS_ADMIN_ADDR", ":8082"),
		DatabaseURL:        envOr("ATLAS_DATABASE_URL", "postgres://atlas:atlas@localhost:5432/atlas?sslmode=disable"),
		RedisURL:           envOr("ATLAS_REDIS_URL", "redis://localhost:6379/0"),
		StoreType:          envOr("ATLAS_STORE", "memory"),
		SuspectAfter:       30 * time.Second,
		OfflineAfter:       60 * time.Second,
		HealthInterval:     10 * time.Second,
		AdminAPIKeys:        os.Getenv("ATLAS_ADMIN_API_KEYS"),
		AdminIPWhitelist:    os.Getenv("ATLAS_ADMIN_IP_WHITELIST"),
		CORSAllowedOrigins:  envOr("ATLAS_CORS_ORIGINS", ""),
		RegistryTokens:      os.Getenv("ATLAS_REGISTRY_TOKENS"),
		RegistryIPWhitelist: os.Getenv("ATLAS_REGISTRY_IP_WHITELIST"),
		EventAdapter:        envOr("ATLAS_EVENT_ADAPTER", "http"),
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