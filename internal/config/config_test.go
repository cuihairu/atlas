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
