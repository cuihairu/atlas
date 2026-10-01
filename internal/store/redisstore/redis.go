// Package redisstore provides a Redis implementation of store.RuntimeStore
// using go-redis.
package redisstore

import (
	"context"
	"fmt"
	"strconv"
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
	client *redis.Client
}

// New creates a new Redis-backed runtime store.
func New(client *redis.Client) *Store {
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

	return rt, nil
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