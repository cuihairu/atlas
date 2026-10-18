package config

import (
	"testing"
	"time"
)

// setenv sets a batch of variables via t.Setenv (auto-restored on cleanup).
func setenv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoad_HADefaults(t *testing.T) {
	cfg := Load()
	if cfg.RedisMasterName != "mymaster" {
		t.Errorf("RedisMasterName = %q, want mymaster", cfg.RedisMasterName)
	}
	if cfg.RedisSentinelAddrs != "" || cfg.RedisClusterAddrs != "" {
		t.Errorf("sentinel/cluster should default to single-node (empty)")
	}
	if cfg.RedisPoolSize != 0 {
		t.Errorf("RedisPoolSize = %d, want 0 (library default)", cfg.RedisPoolSize)
	}
	if cfg.PGPoolMaxConns != 0 || cfg.PGPoolMinConns != 0 {
		t.Errorf("pg pool overrides should default to 0")
	}
	if cfg.PGPoolMaxConnLifetime != 0 || cfg.PGPoolMaxConnIdleTime != 0 || cfg.PGPoolHealthCheckPeriod != 0 {
		t.Errorf("pg pool durations should default to 0")
	}
}

func TestLoad_HAEnvParsing(t *testing.T) {
	setenv(t, map[string]string{
		"ATLAS_REDIS_SENTINELS":             "s1:26379,s2:26379",
		"ATLAS_REDIS_MASTER_NAME":           "atlas",
		"ATLAS_REDIS_CLUSTER":               "c1:6379,c2:6379",
		"ATLAS_REDIS_POOL_SIZE":             "64",
		"ATLAS_PG_POOL_MAX_CONNS":           "128",
		"ATLAS_PG_POOL_MIN_CONNS":           "8",
		"ATLAS_PG_POOL_MAX_CONN_LIFETIME":   "30m",
		"ATLAS_PG_POOL_MAX_CONN_IDLE_TIME":  "5m",
		"ATLAS_PG_POOL_HEALTH_CHECK_PERIOD": "15s",
	})
	cfg := Load()
	if cfg.RedisSentinelAddrs != "s1:26379,s2:26379" {
		t.Errorf("RedisSentinelAddrs = %q", cfg.RedisSentinelAddrs)
	}
	if cfg.RedisMasterName != "atlas" {
		t.Errorf("RedisMasterName = %q, want atlas", cfg.RedisMasterName)
	}
	if cfg.RedisClusterAddrs != "c1:6379,c2:6379" {
		t.Errorf("RedisClusterAddrs = %q", cfg.RedisClusterAddrs)
	}
	if cfg.RedisPoolSize != 64 {
		t.Errorf("RedisPoolSize = %d, want 64", cfg.RedisPoolSize)
	}
	if cfg.PGPoolMaxConns != 128 || cfg.PGPoolMinConns != 8 {
		t.Errorf("pg pool conns = %d/%d, want 128/8", cfg.PGPoolMaxConns, cfg.PGPoolMinConns)
	}
	if cfg.PGPoolMaxConnLifetime != 30*time.Minute {
		t.Errorf("PGPoolMaxConnLifetime = %v", cfg.PGPoolMaxConnLifetime)
	}
	if cfg.PGPoolMaxConnIdleTime != 5*time.Minute {
		t.Errorf("PGPoolMaxConnIdleTime = %v", cfg.PGPoolMaxConnIdleTime)
	}
	if cfg.PGPoolHealthCheckPeriod != 15*time.Second {
		t.Errorf("PGPoolHealthCheckPeriod = %v", cfg.PGPoolHealthCheckPeriod)
	}
}

func TestLoad_RoutingMaintenanceLead(t *testing.T) {
	// Not set: zero value, the routing service applies its 5m default.
	if cfg := Load(); cfg.RoutingMaintenanceLead != 0 {
		t.Errorf("RoutingMaintenanceLead default = %v, want 0", cfg.RoutingMaintenanceLead)
	}
	setenv(t, map[string]string{"ATLAS_ROUTING_MAINTENANCE_LEAD": "2m"})
	if cfg := Load(); cfg.RoutingMaintenanceLead != 2*time.Minute {
		t.Errorf("RoutingMaintenanceLead = %v, want 2m", cfg.RoutingMaintenanceLead)
	}
	// Invalid values are ignored, leaving the zero default.
	setenv(t, map[string]string{"ATLAS_ROUTING_MAINTENANCE_LEAD": "soon"})
	if cfg := Load(); cfg.RoutingMaintenanceLead != 0 {
		t.Errorf("invalid lead should be ignored, got %v", cfg.RoutingMaintenanceLead)
	}
	setenv(t, map[string]string{"ATLAS_ROUTING_MAINTENANCE_LEAD": "0s"})
	if cfg := Load(); cfg.RoutingMaintenanceLead != 0 {
		t.Errorf("0s lead should be ignored (stay 0), got %v", cfg.RoutingMaintenanceLead)
	}
}

func TestLoad_HAInvalidValuesIgnored(t *testing.T) {
	setenv(t, map[string]string{
		"ATLAS_REDIS_POOL_SIZE":           "not-a-number",
		"ATLAS_PG_POOL_MAX_CONNS":         "-5",
		"ATLAS_PG_POOL_MAX_CONN_LIFETIME": "soon",
	})
	cfg := Load()
	if cfg.RedisPoolSize != 0 {
		t.Errorf("invalid RedisPoolSize should be ignored, got %d", cfg.RedisPoolSize)
	}
	if cfg.PGPoolMaxConns != 0 {
		t.Errorf("negative PGPoolMaxConns should be ignored, got %d", cfg.PGPoolMaxConns)
	}
	if cfg.PGPoolMaxConnLifetime != 0 {
		t.Errorf("invalid duration should be ignored, got %v", cfg.PGPoolMaxConnLifetime)
	}
}

func TestLoad_ScalarOverrides(t *testing.T) {
	// Every documented env var with a valid value must land in its field —
	// this pins the env→field mapping the deployment reference documents.
	setenv(t, map[string]string{
		"ATLAS_HTTP_ADDR":             ":18080",
		"ATLAS_REGISTRY_ADDR":         ":18081",
		"ATLAS_ADMIN_ADDR":            ":18082",
		"ATLAS_GRPC_ADDR":             ":19090",
		"ATLAS_DATABASE_URL":          "postgres://p:p@db:5432/x",
		"ATLAS_REDIS_URL":             "redis://r:6379/2",
		"ATLAS_STORE":                 "postgres",
		"ATLAS_SUSPECT_AFTER":         "45s",
		"ATLAS_OFFLINE_AFTER":         "90s",
		"ATLAS_HEALTH_INTERVAL":       "7s",
		"ATLAS_ADMIN_API_KEYS":        "k1,k2",
		"ATLAS_ADMIN_IP_WHITELIST":    "10.0.0.0/8",
		"ATLAS_CORS_ORIGINS":          "https://a.example",
		"ATLAS_REGISTRY_TOKENS":       "t1,t2",
		"ATLAS_REGISTRY_IP_WHITELIST": "192.168.0.0/16",
		"ATLAS_EVENT_ADAPTER":         "redis",
		"ATLAS_KAFKA_BROKERS":         "b1:9092,b2:9092",
		"ATLAS_NATS_URL":              "nats://n:4222",
		"ATLAS_RABBITMQ_URL":          "amqp://q:5672/",
		"ATLAS_PUBLIC_URL":            "https://atlas.example",
		"ATLAS_ALERT_SUSPECT_RATIO":   "0.5",
		"ATLAS_ALERT_OFFLINE_RATIO":   "0.4",
		"ATLAS_ALERT_WEBHOOK_URL":     "https://hook.example/alert",
		"ATLAS_MAINTENANCE_ENFORCE":   "warn",
		"ATLAS_CHAR_SHARDS":           "4",
		"ATLAS_REGISTRY_TLS_CERT":     "/c/server.pem",
		"ATLAS_REGISTRY_TLS_KEY":      "/c/server.key",
		"ATLAS_REGISTRY_CLIENT_CA":    "/c/ca.pem",
		"ATLAS_GRPC_TLS_CERT":         "/g/server.pem",
		"ATLAS_GRPC_TLS_KEY":          "/g/server.key",
		"ATLAS_GRPC_TLS_CLIENT_CA":    "/g/ca.pem",
		"ATLAS_ADMIN_TLS_CERT":        "/a/server.pem",
		"ATLAS_ADMIN_TLS_KEY":         "/a/server.key",
		"ATLAS_ADMIN_TLS_CLIENT_CA":   "/a/ca.pem",
		"ATLAS_ADMIN_ROLES":           "k1:viewer,k2:operator",
		"ATLAS_AUDIT_ENABLED":         "0",
		"ATLAS_RATE_LIMITS":           "/v1/admin/=5:10",
		"ATLAS_RATE_LIMIT_DEFAULT":    "100:200",
		"ATLAS_SERVERS_CONFIG":        "/etc/atlas/servers.json",
		"ATLAS_SERVERS_PROFILE":       "prod",
		"ATLAS_OTLP_ENDPOINT":         "http://otel:4318",
		"ATLAS_TRACING_SAMPLE_RATIO":  "0.5",
	})
	cfg := Load()

	if cfg.HTTPAddr != ":18080" || cfg.RegistryAddr != ":18081" ||
		cfg.AdminAddr != ":18082" || cfg.GRPCAddr != ":19090" {
		t.Errorf("listen addrs = %q %q %q %q", cfg.HTTPAddr, cfg.RegistryAddr, cfg.AdminAddr, cfg.GRPCAddr)
	}
	if cfg.DatabaseURL != "postgres://p:p@db:5432/x" || cfg.RedisURL != "redis://r:6379/2" || cfg.StoreType != "postgres" {
		t.Errorf("store config = %q %q %q", cfg.DatabaseURL, cfg.RedisURL, cfg.StoreType)
	}
	if cfg.SuspectAfter != 45*time.Second || cfg.OfflineAfter != 90*time.Second || cfg.HealthInterval != 7*time.Second {
		t.Errorf("health timings = %v %v %v", cfg.SuspectAfter, cfg.OfflineAfter, cfg.HealthInterval)
	}
	if cfg.AdminAPIKeys != "k1,k2" || cfg.AdminIPWhitelist != "10.0.0.0/8" || cfg.AdminRoles != "k1:viewer,k2:operator" {
		t.Errorf("admin auth = %q %q %q", cfg.AdminAPIKeys, cfg.AdminIPWhitelist, cfg.AdminRoles)
	}
	if cfg.AuditEnabled {
		t.Errorf("ATLAS_AUDIT_ENABLED=0 must disable audit")
	}
	if cfg.CORSAllowedOrigins != "https://a.example" {
		t.Errorf("CORSAllowedOrigins = %q", cfg.CORSAllowedOrigins)
	}
	if cfg.RegistryTokens != "t1,t2" || cfg.RegistryIPWhitelist != "192.168.0.0/16" {
		t.Errorf("registry auth = %q %q", cfg.RegistryTokens, cfg.RegistryIPWhitelist)
	}
	if cfg.EventAdapter != "redis" || cfg.KafkaBrokers != "b1:9092,b2:9092" ||
		cfg.NATSURL != "nats://n:4222" || cfg.RabbitURL != "amqp://q:5672/" {
		t.Errorf("event transport = %q %q %q %q", cfg.EventAdapter, cfg.KafkaBrokers, cfg.NATSURL, cfg.RabbitURL)
	}
	if cfg.PublicURL != "https://atlas.example" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
	if cfg.AlertSuspectRatio != 0.5 || cfg.AlertOfflineRatio != 0.4 {
		t.Errorf("alert ratios = %v %v", cfg.AlertSuspectRatio, cfg.AlertOfflineRatio)
	}
	if cfg.AlertWebhookURL != "https://hook.example/alert" {
		t.Errorf("AlertWebhookURL = %q", cfg.AlertWebhookURL)
	}
	if cfg.MaintenanceEnforce != "warn" {
		t.Errorf("MaintenanceEnforce = %q", cfg.MaintenanceEnforce)
	}
	if cfg.CharShards != 4 {
		t.Errorf("CharShards = %d, want 4", cfg.CharShards)
	}
	if cfg.RegistryTLSCert != "/c/server.pem" || cfg.RegistryTLSKey != "/c/server.key" || cfg.RegistryClientCA != "/c/ca.pem" {
		t.Errorf("registry TLS = %q %q %q", cfg.RegistryTLSCert, cfg.RegistryTLSKey, cfg.RegistryClientCA)
	}
	if cfg.GRPCTLSCert != "/g/server.pem" || cfg.GRPCTLSKey != "/g/server.key" || cfg.GRPCTLSClientCA != "/g/ca.pem" {
		t.Errorf("grpc TLS = %q %q %q", cfg.GRPCTLSCert, cfg.GRPCTLSKey, cfg.GRPCTLSClientCA)
	}
	if cfg.AdminTLSCert != "/a/server.pem" || cfg.AdminTLSKey != "/a/server.key" || cfg.AdminTLSClientCA != "/a/ca.pem" {
		t.Errorf("admin TLS = %q %q %q", cfg.AdminTLSCert, cfg.AdminTLSKey, cfg.AdminTLSClientCA)
	}
	if cfg.RateLimits != "/v1/admin/=5:10" || cfg.RateLimitDefault != "100:200" {
		t.Errorf("rate limits = %q %q", cfg.RateLimits, cfg.RateLimitDefault)
	}
	if cfg.ServersConfigFile != "/etc/atlas/servers.json" || cfg.ServersProfile != "prod" {
		t.Errorf("servers config = %q %q", cfg.ServersConfigFile, cfg.ServersProfile)
	}
	if cfg.OTLPEndpoint != "http://otel:4318" || cfg.TraceSampleRatio != 0.5 {
		t.Errorf("tracing = %q %v", cfg.OTLPEndpoint, cfg.TraceSampleRatio)
	}
}

func TestLoad_InvalidValuesKeepDefaults(t *testing.T) {
	// Malformed or out-of-range numerics fall back to the documented
	// defaults rather than poisoning startup — each case here pins one
	// guard the health of the fleet depends on (a bogus offline threshold
	// must not silently become zero).
	setenv(t, map[string]string{
		"ATLAS_SUSPECT_AFTER":               "later",
		"ATLAS_OFFLINE_AFTER":               "yesterday",
		"ATLAS_HEALTH_INTERVAL":             "sometimes",
		"ATLAS_ALERT_SUSPECT_RATIO":         "not-a-float",
		"ATLAS_ALERT_OFFLINE_RATIO":         "way-out",
		"ATLAS_TRACING_SAMPLE_RATIO":        "1.5", // outside (0,1]
		"ATLAS_CHAR_SHARDS":                 "-3",
		"ATLAS_REDIS_POOL_SIZE":             "many",
		"ATLAS_PG_POOL_MIN_CONNS":           "-1",
		"ATLAS_PG_POOL_MAX_CONN_IDLE_TIME":  "soon",
		"ATLAS_PG_POOL_HEALTH_CHECK_PERIOD": "eventually",
	})
	cfg := Load()

	if cfg.SuspectAfter != 30*time.Second {
		t.Errorf("SuspectAfter = %v, want default 30s", cfg.SuspectAfter)
	}
	if cfg.OfflineAfter != 60*time.Second {
		t.Errorf("OfflineAfter = %v, want default 60s", cfg.OfflineAfter)
	}
	if cfg.HealthInterval != 10*time.Second {
		t.Errorf("HealthInterval = %v, want default 10s", cfg.HealthInterval)
	}
	if cfg.AlertSuspectRatio != 0.3 {
		t.Errorf("AlertSuspectRatio = %v, want default 0.3", cfg.AlertSuspectRatio)
	}
	if cfg.AlertOfflineRatio != 0.2 {
		t.Errorf("AlertOfflineRatio = %v, want default 0.2", cfg.AlertOfflineRatio)
	}
	if cfg.TraceSampleRatio != 1.0 {
		t.Errorf("TraceSampleRatio = %v, want default 1.0 (out-of-range rejected)", cfg.TraceSampleRatio)
	}
	if cfg.CharShards != 1 {
		t.Errorf("CharShards = %d, want default 1 (non-positive rejected)", cfg.CharShards)
	}
	if cfg.RedisPoolSize != 0 || cfg.PGPoolMinConns != 0 {
		t.Errorf("pool sizes = %d/%d, want defaults", cfg.RedisPoolSize, cfg.PGPoolMinConns)
	}
	if cfg.PGPoolMaxConnIdleTime != 0 || cfg.PGPoolHealthCheckPeriod != 0 {
		t.Errorf("pool durations = %v %v, want defaults", cfg.PGPoolMaxConnIdleTime, cfg.PGPoolHealthCheckPeriod)
	}

	// Boundary values that ARE accepted: sample ratio 1.0 stays valid, and a
	// zero-connection floor is legal for the min-conns knob.
	setenv(t, map[string]string{
		"ATLAS_TRACING_SAMPLE_RATIO": "1",
		"ATLAS_PG_POOL_MIN_CONNS":    "0",
	})
	cfg = Load()
	if cfg.TraceSampleRatio != 1.0 || cfg.PGPoolMinConns != 0 {
		t.Errorf("boundary values rejected: ratio=%v minconns=%d", cfg.TraceSampleRatio, cfg.PGPoolMinConns)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("ATLAS_TEST_ENVOR", "set")
	if got := envOr("ATLAS_TEST_ENVOR", "fallback"); got != "set" {
		t.Errorf("envOr with value = %q, want set", got)
	}
	// Empty (but present) counts as unset — same contract Load relies on.
	t.Setenv("ATLAS_TEST_ENVOR", "")
	if got := envOr("ATLAS_TEST_ENVOR", "fallback"); got != "fallback" {
		t.Errorf("envOr with empty value = %q, want fallback", got)
	}
	if got := envOr("ATLAS_TEST_ENVOR_MISSING", "fallback"); got != "fallback" {
		t.Errorf("envOr missing = %q, want fallback", got)
	}
}
