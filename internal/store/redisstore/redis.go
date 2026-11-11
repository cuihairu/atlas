// Package redisstore provides a Redis implementation of store.RuntimeStore
// using go-redis.
package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// compile-time interface check
var _ store.RuntimeStore = (*Store)(nil)

const runtimeTTL = 120 * time.Second

// Store implements store.RuntimeStore on Redis.
type Store struct {
	client redis.UniversalClient
}

// New creates a new Redis-backed runtime store. Any go-redis topology works
// (single node, Sentinel failover, cluster).
func New(client redis.UniversalClient) *Store {
	return &Store{client: client}
}

// Close closes the underlying Redis client.
func (s *Store) Close() error {
	return s.client.Close()
}

// Ping checks the Redis connection.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// runtimeKey returns the Redis hash key for a server's runtime data.
func runtimeKey(id string) string {
	return fmt.Sprintf("atlas:server:%s:runtime", id)
}

// RecordHeartbeat writes the runtime snapshot for a server.
func (s *Store) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	key := runtimeKey(id)
	now := time.Now()

	fields := map[string]any{
		"status":       string(hb.Status),
		"players":      strconv.Itoa(hb.Players),
		"load":         strconv.FormatFloat(hb.Load, 'f', -1, 64),
		"last_seen_at": now.Format(time.RFC3339),
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, runtimeTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("record heartbeat for %s: %w", id, err)
	}
	return nil
}

// GetRuntime returns the latest runtime snapshot for a server.
func (s *Store) GetRuntime(ctx context.Context, id string) (*model.Runtime, error) {
	key := runtimeKey(id)

	vals, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("get runtime for %s: %w", id, err)
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("runtime %s: %w", id, store.ErrNotFound)
	}

	return decodeRuntime(vals), nil
}

// GetRuntimes reads every snapshot in a single pipeline exec — one network
// round trip on a single node (cluster mode batches per slot). Keys that
// don't exist (or expire between queueing and exec) are absent from the
// result, matching ListRuntimes' scan tolerance. This is the read-path
// batching that turns discovery's N single-key reads into 1–2 round trips
// (docs/performance.md §2 known boundary, P1).
func (s *Store) GetRuntimes(ctx context.Context, ids []string) (map[string]model.Runtime, error) {
	if len(ids) == 0 {
		return map[string]model.Runtime{}, nil
	}

	pipe := s.client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, runtimeKey(id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("get runtimes: %w", err)
	}

	out := make(map[string]model.Runtime, len(ids))
	for i, id := range ids {
		vals, err := cmds[i].Result()
		if err != nil {
			continue // backend-level error on this key; degrade, don't fail the fleet
		}
		if len(vals) == 0 {
			continue // expired between queueing and exec
		}
		out[id] = *decodeRuntime(vals)
	}
	return out, nil
}

// decodeRuntime builds a Runtime from the Redis hash fields. Parse errors on
// individual fields degrade to zero values: a partial heartbeat is better
// than a failed read, matching GetRuntime's historical tolerance.
func decodeRuntime(vals map[string]string) *model.Runtime {
	rt := &model.Runtime{
		Status: model.ServerStatus(vals["status"]),
	}

	if v, ok := vals["players"]; ok {
		rt.Players, _ = strconv.Atoi(v)
	}
	if v, ok := vals["load"]; ok {
		rt.Load, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := vals["last_seen_at"]; ok {
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			rt.LastSeenAt = t
		}
	}
	return rt
}

// ListRuntimes returns every runtime snapshot currently stored, keyed by
// server ID. Fleet-wide stats (TotalPlayers) aggregate through this: the
// persistent StatsStore runs in PostgreSQL and cannot see Redis state.
func (s *Store) ListRuntimes(ctx context.Context) (map[string]model.Runtime, error) {
	const prefix, suffix = "atlas:server:", ":runtime"
	out := make(map[string]model.Runtime)

	iter := s.client.Scan(ctx, 0, prefix+"*"+suffix, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		vals, err := s.client.HGetAll(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("list runtimes (%s): %w", key, err)
		}
		if len(vals) == 0 {
			continue // expired between SCAN and HGETALL
		}
		id := strings.TrimSuffix(strings.TrimPrefix(key, prefix), suffix)
		out[id] = *decodeRuntime(vals)
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("list runtimes scan: %w", err)
	}
	return out, nil
}

// DeleteRuntime removes the runtime data for a server.
func (s *Store) DeleteRuntime(ctx context.Context, id string) error {
	key := runtimeKey(id)

	n, err := s.client.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("delete runtime for %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("runtime %s: %w", id, store.ErrNotFound)
	}
	return nil
}
