// Package redisstore tests: the runtime (heartbeat) contract runs against a
// real Redis when ATLAS_TEST_REDIS_URL is set. Without the variable the
// suite skips — `go test ./...` stays dependency-free.
package redisstore

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/storetest"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	url := os.Getenv("ATLAS_TEST_REDIS_URL")
	if url == "" {
		t.Skip("ATLAS_TEST_REDIS_URL not set; skipping redis contract")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis URL: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	// Isolate: the runtime keys carry no TTL-independent namespace, so flush
	// the logical database (a throwaway test DB is expected, like 15).
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	return New(client)
}

func TestRuntimeContract(t *testing.T) {
	storetest.RunRuntime(t, openTestStore(t))
}

func TestListRuntimes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	all, err := s.ListRuntimes(ctx)
	if err != nil || len(all) != 0 {
		t.Fatalf("empty ListRuntimes = %v err=%v", all, err)
	}

	for _, spec := range []struct {
		id      string
		players int
	}{{"game-1", 100}, {"game-2", 250}} {
		if err := s.RecordHeartbeat(ctx, spec.id, model.Heartbeat{Players: spec.players, Load: 0.4}); err != nil {
			t.Fatalf("heartbeat %s: %v", spec.id, err)
		}
	}

	all, err = s.ListRuntimes(ctx)
	if err != nil {
		t.Fatalf("ListRuntimes: %v", err)
	}
	if len(all) != 2 || all["game-1"].Players != 100 || all["game-2"].Players != 250 {
		t.Fatalf("ListRuntimes = %+v", all)
	}
}
