package main

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/config"
)

func TestBuildRedisClient_Topology(t *testing.T) {
	t.Run("single node by default", func(t *testing.T) {
		cfg := config.Config{RedisURL: "redis://:secret@redis-host:6380/2"}
		c := buildRedisClient(cfg)
		defer c.Close()
		client, ok := c.(*redis.Client)
		if !ok {
			t.Fatalf("expected *redis.Client, got %T", c)
		}
		if client.Options().Addr != "redis-host:6380" {
			t.Errorf("Addr = %q, want redis-host:6380", client.Options().Addr)
		}
		if client.Options().Password != "secret" {
			t.Errorf("Password not parsed from URL")
		}
		if client.Options().DB != 2 {
			t.Errorf("DB = %d, want 2", client.Options().DB)
		}
	})

	t.Run("sentinel failover", func(t *testing.T) {
		topo := resolveRedisTopology(config.Config{
			RedisURL:           "redis://:secret@redis-host:6379/1",
			RedisSentinelAddrs: "s1:26379, s2:26379,s3:26379",
			RedisMasterName:    "atlas",
		})
		if topo.mode != "sentinel" {
			t.Fatalf("mode = %q, want sentinel", topo.mode)
		}
		if len(topo.seeds) != 3 {
			t.Errorf("seeds = %v, want 3 entries", topo.seeds)
		}
		if topo.master != "atlas" {
			t.Errorf("master = %q, want atlas", topo.master)
		}
		if topo.password != "secret" || topo.db != 1 {
			t.Errorf("URL credentials not carried over: pass=%q db=%d", topo.password, topo.db)
		}
		// and it must build a live client
		c := buildRedisClient(config.Config{
			RedisURL:           "redis://:secret@redis-host:6379/1",
			RedisSentinelAddrs: "s1:26379,s2:26379,s3:26379",
			RedisMasterName:    "atlas",
		})
		defer c.Close()
		if c == nil {
			t.Fatal("nil client")
		}
	})

	t.Run("cluster takes precedence", func(t *testing.T) {
		topo := resolveRedisTopology(config.Config{
			RedisURL:           "redis://redis-host:6379/0",
			RedisSentinelAddrs: "s1:26379",
			RedisClusterAddrs:  "c1:6379,c2:6379,c3:6379",
		})
		if topo.mode != "cluster" {
			t.Fatalf("mode = %q, want cluster (cluster > sentinel > single)", topo.mode)
		}
		if len(topo.seeds) != 3 {
			t.Errorf("seeds = %v, want 3 entries", topo.seeds)
		}
		c := buildRedisClient(config.Config{RedisClusterAddrs: "c1:6379,c2:6379,c3:6379"})
		defer c.Close()
		if _, ok := c.(*redis.ClusterClient); !ok {
			t.Fatalf("expected *redis.ClusterClient, got %T", c)
		}
	})

	t.Run("pool size propagates", func(t *testing.T) {
		topo := resolveRedisTopology(config.Config{RedisURL: "redis://localhost:6379/0", RedisPoolSize: 42})
		if topo.poolSize != 42 {
			t.Errorf("poolSize = %d, want 42", topo.poolSize)
		}
		client := buildRedisClient(config.Config{RedisURL: "redis://localhost:6379/0", RedisPoolSize: 42}).(*redis.Client)
		defer client.Close()
		if client.Options().PoolSize != 42 {
			t.Errorf("Options().PoolSize = %d, want 42", client.Options().PoolSize)
		}
	})
}

func TestRedisURLParts(t *testing.T) {
	tt := []struct {
		name             string
		raw              string
		addr, user, pass string
		db               int
	}{
		{"default", "redis://localhost:6379/0", "localhost:6379", "", "", 0},
		{"custom host", "redis://r1.internal:7001", "r1.internal:7001", "", "", 0},
		{"password + db", "redis://:hunter2@r1:6379/3", "r1:6379", "", "hunter2", 3},
		{"user + password", "redis://atlas:pw@r1:6379/5", "r1:6379", "atlas", "pw", 5},
		{"no path", "redis://r1:6379", "r1:6379", "", "", 0},
		{"garbage", "::::", "localhost:6379", "", "", 0},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			addr, user, pass, db := redisURLParts(tc.raw)
			if addr != tc.addr {
				t.Errorf("addr = %q, want %q", addr, tc.addr)
			}
			if user != tc.user {
				t.Errorf("user = %q, want %q", user, tc.user)
			}
			if pass != tc.pass {
				t.Errorf("pass = %q, want %q", pass, tc.pass)
			}
			if db != tc.db {
				t.Errorf("db = %d, want %d", db, tc.db)
			}
		})
	}
}

func TestSplitAddrs(t *testing.T) {
	got := splitAddrs("a:1, b:2 ,,c:3,")
	if len(got) != 3 || got[0] != "a:1" || got[1] != "b:2" || got[2] != "c:3" {
		t.Errorf("splitAddrs = %v, want [a:1 b:2 c:3]", got)
	}
	if splitAddrs("") != nil {
		t.Errorf("splitAddrs(\"\") should be nil")
	}
}

func TestApplyPGPoolOptions(t *testing.T) {
	t.Run("zero config keeps DSN defaults", func(t *testing.T) {
		pc, err := pgxpool.ParseConfig("postgres://atlas:atlas@localhost:5432/atlas")
		if err != nil {
			t.Fatal(err)
		}
		applyPGPoolOptions(pc, config.Config{})
		if pc.MaxConns < 1 {
			t.Errorf("MaxConns = %d, expected DSN/library default", pc.MaxConns)
		}
	})

	t.Run("overrides applied and clamped", func(t *testing.T) {
		pc, err := pgxpool.ParseConfig("postgres://atlas:atlas@localhost:5432/atlas")
		if err != nil {
			t.Fatal(err)
		}
		applyPGPoolOptions(pc, config.Config{
			PGPoolMaxConns:          64,
			PGPoolMinConns:          128, // > max → clamped down
			PGPoolMaxConnLifetime:   30 * time.Minute,
			PGPoolMaxConnIdleTime:   5 * time.Minute,
			PGPoolHealthCheckPeriod: 15 * time.Second,
		})
		if pc.MaxConns != 64 {
			t.Errorf("MaxConns = %d, want 64", pc.MaxConns)
		}
		if pc.MinConns != 64 {
			t.Errorf("MinConns = %d, want clamped to 64", pc.MinConns)
		}
		if pc.MaxConnLifetime != 30*time.Minute {
			t.Errorf("MaxConnLifetime = %v", pc.MaxConnLifetime)
		}
		if pc.MaxConnIdleTime != 5*time.Minute {
			t.Errorf("MaxConnIdleTime = %v", pc.MaxConnIdleTime)
		}
		if pc.HealthCheckPeriod != 15*time.Second {
			t.Errorf("HealthCheckPeriod = %v", pc.HealthCheckPeriod)
		}
	})
}
