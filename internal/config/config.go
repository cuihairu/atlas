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

	// PublicURL is the externally reachable base URL of the public API
	// (e.g. "http://atlas.internal:8080"). The config center echoes
	// PublicURL + /v1/crossserver/config in callback notifications so
	// receivers know where to pull without hardcoding a second address.
	// Env: ATLAS_PUBLIC_URL, default "".
	PublicURL string

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

	// MaintenanceEnforce controls what happens to a new-character request
	// (创角) aimed at a server that is in maintenance — status or 维护中 tag:
	// "block" rejects with 403 SERVER_IN_MAINTENANCE, "warn" lets it through
	// and attaches a Warning header. 禁止注册 tags always reject regardless
	// of this setting.
	// Env: ATLAS_MAINTENANCE_ENFORCE, default "block".
	MaintenanceEnforce string

	// RoutingMaintenanceLead is the pre-maintenance steering horizon
	// (维护前引导): a server whose maintenance window starts within this lead
	// is excluded from routing recommendations even while still "online" —
	// players must not land on a server about to enter maintenance. Zero
	// falls back to the routing default (5m); active windows always block.
	// Env: ATLAS_ROUTING_MAINTENANCE_LEAD, default 5m.
	RoutingMaintenanceLead time.Duration

	// CharShards is the logical shard count for the character index
	// (TODO v0.1.16). Values > 1 wrap the backing character store in an
	// account-hash sharded composite (store/sharded): key-routed operations
	// cost one shard hop, global lookups and admin search fan out. With a
	// single physical store this exercises the routing paths; embedding Atlas
	// with per-shard stores splits storage physically.
	// Env: ATLAS_CHAR_SHARDS, default 1.
	CharShards int

	// RegistryTLSCert / RegistryTLSKey enable TLS on the Registry listener
	// (server certificate + private key, PEM). TODO v0.1.17.
	// Env: ATLAS_REGISTRY_TLS_CERT / ATLAS_REGISTRY_TLS_KEY, default "".
	RegistryTLSCert string
	RegistryTLSKey  string

	// RegistryClientCA upgrades Registry TLS to mutual TLS: clients must
	// present a certificate signed by this CA bundle. Requires the server
	// cert/key above.
	// Env: ATLAS_REGISTRY_CLIENT_CA, default "".
	RegistryClientCA string

	// GRPCTLSCert / GRPCTLSKey enable TLS on the gRPC listener (:9090)
	// — same material semantics as the Registry pair above. Unset =
	// plaintext, the historical behavior.
	// Env: ATLAS_GRPC_TLS_CERT / ATLAS_GRPC_TLS_KEY, default "".
	GRPCTLSCert string
	GRPCTLSKey  string

	// GRPCTLSClientCA upgrades gRPC TLS to mutual TLS: callers must
	// present a certificate signed by this CA bundle. Requires the
	// gRPC cert/key above.
	// Env: ATLAS_GRPC_TLS_CLIENT_CA, default "".
	GRPCTLSClientCA string

	// AdminRoles configures RBAC as "key:role" pairs (admin/operator/viewer,
	// comma-separated). Keys not listed here default to admin. TODO v0.1.17.
	// Env: ATLAS_ADMIN_ROLES, default "".
	AdminRoles string

	// AuditEnabled toggles the Admin API audit log (structured log + in-memory
	// ring served at GET /v1/admin/audit). TODO v0.1.17.
	// Env: ATLAS_AUDIT_ENABLED, default true.
	AuditEnabled bool

	// RateLimits configures per-endpoint token buckets as
	// "prefix=rps[:burst]" pairs (semicolon-separated), matched by longest
	// prefix, bucketed per client IP. Empty disables rate limiting.
	// Env: ATLAS_RATE_LIMITS, default "".
	RateLimits string

	// RateLimitDefault is the fallback "rps[:burst]" rule for paths no
	// ATLAS_RATE_LIMITS prefix matches. Only applies when rate limiting is
	// enabled via ATLAS_RATE_LIMITS or this field.
	// Env: ATLAS_RATE_LIMIT_DEFAULT, default "".
	RateLimitDefault string

	// RedisSentinelAddrs is a comma-separated list of Sentinel host:port
	// addresses. When set, Redis clients use failover mode (monitoring
	// RedisMasterName) instead of connecting to RedisURL directly. TODO
	// v0.1.19.
	// Env: ATLAS_REDIS_SENTINELS, default "".
	RedisSentinelAddrs string

	// RedisMasterName is the master set name the Sentinels monitor. Only
	// used with ATLAS_REDIS_SENTINELS.
	// Env: ATLAS_REDIS_MASTER_NAME, default "mymaster".
	RedisMasterName string

	// RedisClusterAddrs is a comma-separated list of cluster seed nodes
	// (host:port). Takes precedence over Sentinel and single-node config.
	// TODO v0.1.19.
	// Env: ATLAS_REDIS_CLUSTER, default "".
	RedisClusterAddrs string

	// RedisPoolSize caps the per-instance Redis connection pool. 0 keeps the
	// go-redis default (10 × GOMAXPROCS).
	// Env: ATLAS_REDIS_POOL_SIZE, default 0.
	RedisPoolSize int

	// PostgreSQL connection-pool tuning layered on pgxpool.ParseConfig.
	// Zero values keep the DSN / library defaults (MaxConns = max(4,
	// NumCPU)). TODO v0.1.19.
	// Env: ATLAS_PG_POOL_MAX_CONNS / ATLAS_PG_POOL_MIN_CONNS /
	// ATLAS_PG_POOL_MAX_CONN_LIFETIME / ATLAS_PG_POOL_MAX_CONN_IDLE_TIME /
	// ATLAS_PG_POOL_HEALTH_CHECK_PERIOD.
	PGPoolMaxConns          int
	PGPoolMinConns          int
	PGPoolMaxConnLifetime   time.Duration
	PGPoolMaxConnIdleTime   time.Duration
	PGPoolHealthCheckPeriod time.Duration

	// ServersConfigFile points at a JSON file declaring config-managed
	// servers (docs/server-config.md). When set, the declared fleet is
	// upserted at startup — no register call needed — and the register API
	// rejects updates for declared IDs. Empty disables the feature.
	// Env: ATLAS_SERVERS_CONFIG, default "".
	ServersConfigFile string

	// ServersProfile selects the active profile when the servers config file
	// declares multiple profiles. Overrides the file's own "profile" field;
	// leaving both empty selects the "default" profile when present.
	// Env: ATLAS_SERVERS_PROFILE, default "".
	ServersProfile string

	// OTLPEndpoint enables OpenTelemetry trace export to an OTLP/HTTP
	// receiver (docs/api.md §请求追踪): "http://host:4318" for plaintext,
	// "https://…" for TLS. Unset keeps the no-op tracer provider — spans
	// are non-recording and export costs nothing (roadmap 可观测性深化).
	// Env: ATLAS_OTLP_ENDPOINT, default "".
	OTLPEndpoint string

	// TraceSampleRatio is the parent-based TraceIDRatio sampling rate for
	// root spans when OTLPEndpoint is configured; child spans follow their
	// parent. Values outside (0,1] fall back to 1.0.
	// Env: ATLAS_TRACING_SAMPLE_RATIO, default 1.0.
	TraceSampleRatio float64
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
		PublicURL:           envOr("ATLAS_PUBLIC_URL", ""),
		AlertSuspectRatio:   0.3,
		AlertOfflineRatio:   0.2,
		AlertWebhookURL:     os.Getenv("ATLAS_ALERT_WEBHOOK_URL"),
		MaintenanceEnforce:  envOr("ATLAS_MAINTENANCE_ENFORCE", "block"),
		CharShards:          1,
		RegistryTLSCert:     os.Getenv("ATLAS_REGISTRY_TLS_CERT"),
		RegistryTLSKey:      os.Getenv("ATLAS_REGISTRY_TLS_KEY"),
		RegistryClientCA:    os.Getenv("ATLAS_REGISTRY_CLIENT_CA"),
		GRPCTLSCert:         os.Getenv("ATLAS_GRPC_TLS_CERT"),
		GRPCTLSKey:          os.Getenv("ATLAS_GRPC_TLS_KEY"),
		GRPCTLSClientCA:     os.Getenv("ATLAS_GRPC_TLS_CLIENT_CA"),
		AdminRoles:          os.Getenv("ATLAS_ADMIN_ROLES"),
		AuditEnabled:        os.Getenv("ATLAS_AUDIT_ENABLED") != "0",
		RateLimits:          os.Getenv("ATLAS_RATE_LIMITS"),
		RateLimitDefault:    os.Getenv("ATLAS_RATE_LIMIT_DEFAULT"),
		OTLPEndpoint:        os.Getenv("ATLAS_OTLP_ENDPOINT"),
		TraceSampleRatio:    1.0,
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
	if v := os.Getenv("ATLAS_ROUTING_MAINTENANCE_LEAD"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.RoutingMaintenanceLead = d
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
	if v := os.Getenv("ATLAS_TRACING_SAMPLE_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			cfg.TraceSampleRatio = f
		}
	}
	if v := os.Getenv("ATLAS_CHAR_SHARDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.CharShards = n
		}
	}
	cfg.RedisSentinelAddrs = os.Getenv("ATLAS_REDIS_SENTINELS")
	cfg.RedisMasterName = envOr("ATLAS_REDIS_MASTER_NAME", "mymaster")
	cfg.RedisClusterAddrs = os.Getenv("ATLAS_REDIS_CLUSTER")
	if v := os.Getenv("ATLAS_REDIS_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RedisPoolSize = n
		}
	}
	if v := os.Getenv("ATLAS_PG_POOL_MAX_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PGPoolMaxConns = n
		}
	}
	if v := os.Getenv("ATLAS_PG_POOL_MIN_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.PGPoolMinConns = n
		}
	}
	if v := os.Getenv("ATLAS_PG_POOL_MAX_CONN_LIFETIME"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PGPoolMaxConnLifetime = d
		}
	}
	if v := os.Getenv("ATLAS_PG_POOL_MAX_CONN_IDLE_TIME"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PGPoolMaxConnIdleTime = d
		}
	}
	if v := os.Getenv("ATLAS_PG_POOL_HEALTH_CHECK_PERIOD"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PGPoolHealthCheckPeriod = d
		}
	}

	cfg.ServersConfigFile = os.Getenv("ATLAS_SERVERS_CONFIG")
	cfg.ServersProfile = os.Getenv("ATLAS_SERVERS_PROFILE")

	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
