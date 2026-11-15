package config

import (
	"strings"
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

// mustLoad wraps Load for the happy-path tests: any validation error fails
// the test immediately.
func mustLoad(t *testing.T) Config {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	return cfg
}

func TestLoad_HADefaults(t *testing.T) {
	cfg := mustLoad(t)
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
	cfg := mustLoad(t)
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
	if cfg := mustLoad(t); cfg.RoutingMaintenanceLead != 0 {
		t.Errorf("RoutingMaintenanceLead default = %v, want 0", cfg.RoutingMaintenanceLead)
	}
	setenv(t, map[string]string{"ATLAS_ROUTING_MAINTENANCE_LEAD": "2m"})
	if cfg := mustLoad(t); cfg.RoutingMaintenanceLead != 2*time.Minute {
		t.Errorf("RoutingMaintenanceLead = %v, want 2m", cfg.RoutingMaintenanceLead)
	}
	// Invalid or non-positive values are rejected at startup.
	setenv(t, map[string]string{"ATLAS_ROUTING_MAINTENANCE_LEAD": "soon"})
	if _, err := Load(); err == nil {
		t.Errorf("invalid lead should be rejected, got nil error")
	}
	setenv(t, map[string]string{"ATLAS_ROUTING_MAINTENANCE_LEAD": "0s"})
	if _, err := Load(); err == nil {
		t.Errorf("0s lead should be rejected (must be positive), got nil error")
	}
}

func TestLoad_HAInvalidValuesRejected(t *testing.T) {
	setenv(t, map[string]string{
		"ATLAS_REDIS_POOL_SIZE":           "not-a-number",
		"ATLAS_PG_POOL_MAX_CONNS":         "-5",
		"ATLAS_PG_POOL_MAX_CONN_LIFETIME": "soon",
	})
	_, err := Load()
	if err == nil {
		t.Fatalf("invalid pool settings should be rejected, got nil error")
	}
	for _, key := range []string{"ATLAS_REDIS_POOL_SIZE", "ATLAS_PG_POOL_MAX_CONNS", "ATLAS_PG_POOL_MAX_CONN_LIFETIME"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error should name %s, got: %v", key, err)
		}
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
		"ATLAS_EVENT_MAX_DELIVERY":    "7",
		"ATLAS_AUDIT_SINK":            "jsonl",
		"ATLAS_AUDIT_SINK_PATH":       "/var/log/atlas/audit.jsonl",
		"ATLAS_AUDIT_RETENTION":       "720h",
		"ATLAS_AUDIT_MASK_KEYS":       "token,secret",
		"ATLAS_AUDIT_QUEUE_SIZE":      "256",
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
	cfg := mustLoad(t)

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
	if cfg.EventMaxDelivery != 7 {
		t.Errorf("EventMaxDelivery = %d, want 7 (ATLAS_EVENT_MAX_DELIVERY)", cfg.EventMaxDelivery)
	}
	if cfg.AuditSink != "jsonl" || cfg.AuditSinkPath != "/var/log/atlas/audit.jsonl" {
		t.Errorf("audit sink = %q %q", cfg.AuditSink, cfg.AuditSinkPath)
	}
	if cfg.AuditRetention != 720*time.Hour {
		t.Errorf("AuditRetention = %v, want 720h", cfg.AuditRetention)
	}
	if cfg.AuditMaskKeys != "token,secret" {
		t.Errorf("AuditMaskKeys = %q", cfg.AuditMaskKeys)
	}
	if cfg.AuditQueueSize != 256 {
		t.Errorf("AuditQueueSize = %d, want 256", cfg.AuditQueueSize)
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

func TestLoad_InvalidValuesRejected(t *testing.T) {
	// Malformed or out-of-domain numerics must fail startup instead of
	// silently falling back to defaults — each case here pins one guard the
	// health of the fleet depends on (a bogus offline threshold must not
	// quietly become the default while the operator believes it applied).
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
	_, err := Load()
	if err == nil {
		t.Fatalf("invalid values should be rejected, got nil error")
	}
	for _, key := range []string{
		"ATLAS_SUSPECT_AFTER", "ATLAS_OFFLINE_AFTER", "ATLAS_HEALTH_INTERVAL",
		"ATLAS_ALERT_SUSPECT_RATIO", "ATLAS_ALERT_OFFLINE_RATIO",
		"ATLAS_TRACING_SAMPLE_RATIO", "ATLAS_CHAR_SHARDS", "ATLAS_REDIS_POOL_SIZE",
		"ATLAS_PG_POOL_MIN_CONNS", "ATLAS_PG_POOL_MAX_CONN_IDLE_TIME",
		"ATLAS_PG_POOL_HEALTH_CHECK_PERIOD",
	} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error should name %s, got: %v", key, err)
		}
	}
}

func TestLoad_ValidBoundaryValues(t *testing.T) {
	// Boundary values that ARE accepted: 0 keeps "no cap / immediate"
	// semantics for the HA thresholds and pg pool lifetimes, a zero-connection
	// floor is legal, sample ratio 1.0 stays valid, "off" is the documented
	// audit-sink disable, and an alert ratio above 1 stays legal (enabled but
	// never fires).
	setenv(t, map[string]string{
		"ATLAS_SUSPECT_AFTER":               "0s",
		"ATLAS_OFFLINE_AFTER":               "0s",
		"ATLAS_HEALTH_INTERVAL":             "0s",
		"ATLAS_PG_POOL_MIN_CONNS":           "0",
		"ATLAS_PG_POOL_MAX_CONN_LIFETIME":   "0s",
		"ATLAS_PG_POOL_MAX_CONN_IDLE_TIME":  "0s",
		"ATLAS_PG_POOL_HEALTH_CHECK_PERIOD": "0s",
		"ATLAS_TRACING_SAMPLE_RATIO":        "1",
		"ATLAS_AUDIT_SINK":                  "off",
		"ATLAS_ALERT_SUSPECT_RATIO":         "2",
	})
	cfg := mustLoad(t)
	if cfg.SuspectAfter != 0 || cfg.OfflineAfter != 0 || cfg.HealthInterval != 0 {
		t.Errorf("zero durations should be accepted, got %v %v %v", cfg.SuspectAfter, cfg.OfflineAfter, cfg.HealthInterval)
	}
	if cfg.PGPoolMinConns != 0 {
		t.Errorf("PGPoolMinConns = %d, want 0 (zero floor is legal)", cfg.PGPoolMinConns)
	}
	if cfg.PGPoolMaxConnLifetime != 0 || cfg.PGPoolMaxConnIdleTime != 0 || cfg.PGPoolHealthCheckPeriod != 0 {
		t.Errorf("zero pg durations should be accepted (no cap)")
	}
	if cfg.TraceSampleRatio != 1.0 {
		t.Errorf("TraceSampleRatio = %v, want 1.0", cfg.TraceSampleRatio)
	}
	if cfg.AuditSink != "" {
		t.Errorf("AuditSink = %q, want \"\" for off", cfg.AuditSink)
	}
	if cfg.AlertSuspectRatio != 2 {
		t.Errorf("AlertSuspectRatio = %v, want 2 (above 1 stays legal)", cfg.AlertSuspectRatio)
	}
}

func TestLoad_AggregatesAllErrors(t *testing.T) {
	// One restart should surface every typo, not just the first one hit.
	setenv(t, map[string]string{
		"ATLAS_SUSPECT_AFTER": "later",
		"ATLAS_CHAR_SHARDS":   "0",
		"ATLAS_AUDIT_SINK":    "clickhouse",
	})
	_, err := Load()
	if err == nil {
		t.Fatalf("invalid values should be rejected, got nil error")
	}
	for _, key := range []string{"ATLAS_SUSPECT_AFTER", "ATLAS_CHAR_SHARDS", "ATLAS_AUDIT_SINK"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error should name %s, got: %v", key, err)
		}
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

func TestLoad_AuditSinkDefaults(t *testing.T) {
	// Unset: sink off (today's behavior), 90-day retention window, the
	// documented default mask list, library-default queue.
	cfg := mustLoad(t)
	if cfg.AuditSink != "" {
		t.Errorf("AuditSink default = %q, want \"\" (off)", cfg.AuditSink)
	}
	if cfg.AuditRetention != 90*24*time.Hour {
		t.Errorf("AuditRetention default = %v, want 90d", cfg.AuditRetention)
	}
	if cfg.AuditMaskKeys != "token,secret,password,key" {
		t.Errorf("AuditMaskKeys default = %q", cfg.AuditMaskKeys)
	}
	if cfg.AuditQueueSize != 0 {
		t.Errorf("AuditQueueSize default = %d, want 0 (library default)", cfg.AuditQueueSize)
	}

	// "off" normalizes to "" so the wiring reads one value.
	setenv(t, map[string]string{"ATLAS_AUDIT_SINK": "off"})
	if cfg := mustLoad(t); cfg.AuditSink != "" {
		t.Errorf("AuditSink off = %q, want \"\"", cfg.AuditSink)
	}

	// Invalid values are rejected at startup instead of silently reverting
	// to the defaults — an operator who typos the retention window must not
	// quietly get 90 days.
	setenv(t, map[string]string{
		"ATLAS_AUDIT_RETENTION":  "soon",
		"ATLAS_AUDIT_QUEUE_SIZE": "-5",
	})
	if _, err := Load(); err == nil {
		t.Fatalf("invalid audit envs should be rejected, got nil error")
	}

	// A zero window is accepted: it means "never expire" and must survive.
	// Reset the queue size too — setenv above would otherwise leak "-5" into
	// this block and fail the happy-path load.
	setenv(t, map[string]string{"ATLAS_AUDIT_RETENTION": "0", "ATLAS_AUDIT_QUEUE_SIZE": ""})
	if cfg := mustLoad(t); cfg.AuditRetention != 0 {
		t.Errorf("AuditRetention 0 = %v, want 0 (never expire)", cfg.AuditRetention)
	}
}
