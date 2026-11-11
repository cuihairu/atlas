// Package redisstore tests: the runtime (heartbeat) contract runs against a
// real Redis when ATLAS_TEST_REDIS_URL is set; without the variable it runs
// against embedded miniredis (Redis-compatible, dependency-free) so the
// contract and the failure-injection cases execute in every `go test ./...`.
package redisstore

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/storetest"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	url := os.Getenv("ATLAS_TEST_REDIS_URL")
	var client *redis.Client
	if url == "" {
		client = redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	} else {
		opts, err := redis.ParseURL(url)
		if err != nil {
			t.Fatalf("parse redis URL: %v", err)
		}
		client = redis.NewClient(opts)
	}
	t.Cleanup(func() { client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	if url == "" {
		return New(client)
	}
	// Real Redis isolation: the runtime keys carry no TTL-independent
	// namespace, so flush the logical database (a throwaway test DB is
	// expected, like 15).
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	return New(client)
}

// TestDataLossFlushAll_HeartbeatRefills automates the「Redis 数据丢失」
// row of the failure-injection matrix (docs/performance.md §7): after
// FLUSHALL the runtime keys are gone — 判活依据在 PG 档案与 last_seen_at
// 年龄,不依赖 Redis 键存活 — no stale view leaks through the batched
// read, and the next heartbeat refills that server's data only.
func TestDataLossFlushAll_HeartbeatRefills(t *testing.T) {
	mr := miniredis.RunT(t)
	st := New(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()

	hb := model.Heartbeat{Status: model.StatusOnline, Players: 42, Load: 0.5}
	for _, id := range []string{"game-1001", "game-1002"} {
		if err := st.RecordHeartbeat(ctx, id, hb); err != nil {
			t.Fatalf("record heartbeat %s: %v", id, err)
		}
	}

	// Data loss: the runtime keys vanish wholesale.
	mr.FlushAll()

	// Single-key read reports not-found; the batched read shows no stale
	// entries (缺失即缺席 — 客户端列表不会看到残留的玩家/负载).
	if _, err := st.GetRuntime(ctx, "game-1001"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after flushall = %v, want ErrNotFound", err)
	}
	got, err := st.GetRuntimes(ctx, []string{"game-1001", "game-1002"})
	if err != nil {
		t.Fatalf("get runtimes after flushall: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("stale runtime view after flushall: %v", got)
	}

	// Heartbeat refills: a live server's next heartbeat recreates its row;
	// a silent server stays absent (其失联结论由巡检验证,键恢复不代劳).
	if err := st.RecordHeartbeat(ctx, "game-1001", hb); err != nil {
		t.Fatalf("re-heartbeat: %v", err)
	}
	rt, err := st.GetRuntime(ctx, "game-1001")
	if err != nil {
		t.Fatalf("runtime after refill: %v", err)
	}
	if rt.Players != 42 || rt.Status != model.StatusOnline {
		t.Errorf("refilled runtime = %+v, want players=42 online", rt)
	}
	if _, err := st.GetRuntime(ctx, "game-1002"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("game-1002 runtime after refill = %v, want ErrNotFound", err)
	}
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
