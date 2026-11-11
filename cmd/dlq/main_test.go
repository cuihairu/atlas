package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/config"
	"github.com/cuihairu/atlas/internal/event"
)

func TestResolveTopicsDefaultsToAll(t *testing.T) {
	topics := resolveTopics(nil)
	if len(topics) != 2 || topics[0] != event.TopicCharacters || topics[1] != event.TopicConfig {
		t.Fatalf("default topics = %v", topics)
	}
	// The returned slice must not share backing storage with the default.
	topics[0] = "changed"
	if resolveTopics(nil)[0] != event.TopicCharacters {
		t.Fatal("default topic list was mutated")
	}
}

func TestResolveTopicsKeepsExplicitScope(t *testing.T) {
	topics := resolveTopics([]string{event.TopicConfig})
	if len(topics) != 1 || topics[0] != event.TopicConfig {
		t.Fatalf("explicit topics = %v", topics)
	}
}

func TestTopicsFlagAccumulatesAndSplits(t *testing.T) {
	var topics topicsFlag
	for _, v := range []string{event.TopicCharacters, event.TopicConfig + ", atlas.extra", "  ", "atlas.extra2"} {
		if err := topics.Set(v); err != nil {
			t.Fatalf("set %q: %v", v, err)
		}
	}
	want := []string{event.TopicCharacters, event.TopicConfig, "atlas.extra", "atlas.extra2"}
	if len(topics) != len(want) {
		t.Fatalf("topics = %v, want %v", topics, want)
	}
	for i := range want {
		if topics[i] != want[i] {
			t.Fatalf("topics[%d] = %q, want %q", i, topics[i], want[i])
		}
	}
}

func TestValidateAdapter(t *testing.T) {
	for _, ok := range []string{"redis", "kafka", "nats", "rabbitmq"} {
		if err := validateAdapter(ok); err != nil {
			t.Fatalf("validateAdapter(%q) = %v, want ok", ok, err)
		}
	}
	for _, name := range []string{"", "http"} {
		err := validateAdapter(name)
		if err == nil || !strings.Contains(err.Error(), "has no dead-letter channel") {
			t.Fatalf("validateAdapter(%q) = %v, want dead-letter message", name, err)
		}
	}
	err := validateAdapter("bogus")
	if err == nil || !strings.Contains(err.Error(), "unknown event adapter") {
		t.Fatalf("validateAdapter(bogus) = %v, want unknown-adapter message", err)
	}
}

func TestRedisClientSingleNode(t *testing.T) {
	client, err := redisClient(config.Config{RedisURL: "redis://user:pass@db:6379/3"})
	if err != nil {
		t.Fatalf("redisClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	c, ok := client.(*redis.Client)
	if !ok {
		t.Fatalf("client = %T, want single-node *redis.Client", client)
	}
	opts := c.Options()
	if opts.Addr != "db:6379" || opts.DB != 3 || opts.Username != "user" || opts.Password != "pass" {
		t.Fatalf("options = addr %q db %d user %q pass %q", opts.Addr, opts.DB, opts.Username, opts.Password)
	}
}

func TestRedisClientSentinel(t *testing.T) {
	client, err := redisClient(config.Config{
		RedisURL:           "redis://r:6379/2",
		RedisSentinelAddrs: "s1:26379, s2:26379",
		RedisMasterName:    "atlas",
	})
	if err != nil {
		t.Fatalf("redisClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	// go-redis v9 returns a sentinel client as *Client whose addr is the
	// "FailoverClient" marker; that is what separates failover from a plain
	// single-node client (both satisfy UniversalClient).
	c, ok := client.(*redis.Client)
	if !ok {
		t.Fatalf("client = %T, want *redis.Client", client)
	}
	opts := c.Options()
	if opts.Addr != "FailoverClient" || opts.DB != 2 {
		t.Fatalf("failover options = addr %q db %d, want the FailoverClient marker with db 2", opts.Addr, opts.DB)
	}
}

func TestRedisClientCluster(t *testing.T) {
	client, err := redisClient(config.Config{
		RedisURL:          "redis://u:p@r:6379/0",
		RedisClusterAddrs: "c1:6379,c2:6379",
	})
	if err != nil {
		t.Fatalf("redisClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	c, ok := client.(*redis.ClusterClient)
	if !ok {
		t.Fatalf("client = %T, want *redis.ClusterClient", client)
	}
	if got := c.Options().Addrs; len(got) != 2 || got[0] != "c1:6379" || got[1] != "c2:6379" {
		t.Fatalf("cluster addrs = %v", got)
	}
}

func TestRedisClientRejectsBadURL(t *testing.T) {
	if _, err := redisClient(config.Config{RedisURL: "http://not-redis:6379"}); err == nil {
		t.Fatal("expected an error for a non-redis URL scheme")
	}
}

func TestRunRejectsInvalidFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-adapter", "http"}, &out, &errb); code != 2 {
		t.Fatalf("run(exit) = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "has no dead-letter channel") {
		t.Fatalf("stderr = %q", errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", out.String())
	}

	var errb2 bytes.Buffer
	if code := run([]string{"-unknown-flag"}, &out, &errb2); code != 2 {
		t.Fatalf("run(exit) = %d, want 2 for an unknown flag", code)
	}
}

// TestRunSurfacesStoreFailure proves a store that cannot be reached fails
// the run (non-zero) instead of reporting an empty pass as success.
func TestRunSurfacesStoreFailure(t *testing.T) {
	t.Setenv("ATLAS_EVENT_ADAPTER", "redis")
	t.Setenv("ATLAS_REDIS_URL", "redis://127.0.0.1:1/0")

	var out, errb bytes.Buffer
	if code := run([]string{"-topic", event.TopicCharacters, "-dry-run"}, &out, &errb); code == 0 {
		t.Fatalf("run succeeded against an unreachable store; stderr = %q", errb.String())
	}
	if !strings.Contains(errb.String(), event.TopicCharacters) {
		t.Fatalf("stderr = %q, want the failing topic named", errb.String())
	}
}
