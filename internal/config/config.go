// Package config loads Atlas configuration from environment variables.
package config

import (
	"os"
	"strconv"
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

	// GRPCAddr is the address the gRPC API listens on (all services).
	// Env: ATLAS_GRPC_ADDR, default ":9090".
	GRPCAddr string

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
	// (docs/sync.md §4): "http" (synchronous in-process, the v0.1 behavior),
	// "redis" (Redis Streams with a consumer group), "kafka", "nats"
	// (JetStream) or "rabbitmq".
	// Env: ATLAS_EVENT_ADAPTER, default "http".
	EventAdapter string

	// KafkaBrokers is a comma-separated list of seed brokers for the Kafka
	// event adapter.
	// Env: ATLAS_KAFKA_BROKERS, default "localhost:9092".
	KafkaBrokers string

	// NATSURL is the NATS server URL for the JetStream event adapter.
	// Env: ATLAS_NATS_URL, default "nats://localhost:4222".
	NATSURL string

	// RabbitURL is the AMQP endpoint for the RabbitMQ event adapter.
	// Env: ATLAS_RABBITMQ_URL, default "amqp://localhost:5672/".
	RabbitURL string

	// AlertSuspectRatio fires a health alert when the fraction of auto-managed
	// servers in "suspect" status reaches this value. 0 disables the alert.
	// Env: ATLAS_ALERT_SUSPECT_RATIO, default 0.3.
	AlertSuspectRatio float64

	// AlertOfflineRatio fires a health alert when the fraction of auto-managed
	// servers in "offline" status reaches this value. 0 disables the alert.
	// Env: ATLAS_ALERT_OFFLINE_RATIO, default 0.2.
	AlertOfflineRatio float64

	// AlertWebhookURL optionally receives a JSON POST when a health alert
	// fires or recovers. Empty disables webhook delivery.
	// Env: ATLAS_ALERT_WEBHOOK_URL, default "".
	AlertWebhookURL string

	// CharShards is the logical shard count for the character index
	// (TODO v0.1.16). Values > 1 wrap the backing character store in an
	// account-hash sharded composite (store/sharded): key-routed operations
	// cost one shard hop, global lookups and admin search fan out. With a
	// single physical store this exercises the routing paths; embedding Atlas
	// with per-shard stores splits storage physically.
	// Env: ATLAS_CHAR_SHARDS, default 1.
	CharShards int
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	cfg := Config{
		HTTPAddr:            envOr("ATLAS_HTTP_ADDR", ":8080"),
		RegistryAddr:        envOr("ATLAS_REGISTRY_ADDR", ":8081"),
		AdminAddr:           envOr("ATLAS_ADMIN_ADDR", ":8082"),
		GRPCAddr:            envOr("ATLAS_GRPC_ADDR", ":9090"),
		DatabaseURL:         envOr("ATLAS_DATABASE_URL", "postgres://atlas:atlas@localhost:5432/atlas?sslmode=disable"),
		RedisURL:            envOr("ATLAS_REDIS_URL", "redis://localhost:6379/0"),
		StoreType:           envOr("ATLAS_STORE", "memory"),
		SuspectAfter:        30 * time.Second,
		OfflineAfter:        60 * time.Second,
		HealthInterval:      10 * time.Second,
		AdminAPIKeys:        os.Getenv("ATLAS_ADMIN_API_KEYS"),
		AdminIPWhitelist:    os.Getenv("ATLAS_ADMIN_IP_WHITELIST"),
		CORSAllowedOrigins:  envOr("ATLAS_CORS_ORIGINS", ""),
		RegistryTokens:      os.Getenv("ATLAS_REGISTRY_TOKENS"),
		RegistryIPWhitelist: os.Getenv("ATLAS_REGISTRY_IP_WHITELIST"),
		EventAdapter:        envOr("ATLAS_EVENT_ADAPTER", "http"),
		KafkaBrokers:        envOr("ATLAS_KAFKA_BROKERS", "localhost:9092"),
		NATSURL:             envOr("ATLAS_NATS_URL", "nats://localhost:4222"),
		RabbitURL:           envOr("ATLAS_RABBITMQ_URL", "amqp://localhost:5672/"),
		AlertSuspectRatio:   0.3,
		AlertOfflineRatio:   0.2,
		AlertWebhookURL:     os.Getenv("ATLAS_ALERT_WEBHOOK_URL"),
		CharShards:          1,
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
	if v := os.Getenv("ATLAS_ALERT_SUSPECT_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.AlertSuspectRatio = f
		}
	}
	if v := os.Getenv("ATLAS_ALERT_OFFLINE_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.AlertOfflineRatio = f
		}
	}
	if v := os.Getenv("ATLAS_CHAR_SHARDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.CharShards = n
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
