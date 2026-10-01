// Package redis implements the Redis Streams event adapter (docs/sync.md §4).
//
// Publish XADDs to the topic's stream; Subscribe starts a consumer group
// (XREADGROUP) that applies events and acknowledges them (XACK). Delivery is
// at-least-once: entries whose handler fails stay in the pending entries
// list and are re-claimed after a retry interval, so handlers must be
// idempotent — Directory.ApplyEvent is.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/event"
)

const (
	// DefaultGroup is the consumer group name.
	DefaultGroup = "atlas"
	// retryAfter is how long a pending entry must be idle before it is
	// re-claimed from its consumer.
	retryAfter = 30 * time.Second
	// maxRedelivery bounds re-claiming so a permanently failing event is
	// logged and dead-lettered instead of looping forever.
	maxRedelivery = 5
)

// Options tunes the adapter. Zero values select defaults.
type Options struct {
	// Group overrides the consumer group name (default "atlas").
	Group string
	// Consumer overrides this process's consumer name
	// (default "hostname:pid").
	Consumer string
	// Block is the XREADGROUP blocking interval (default 5s).
	Block time.Duration
	// Logger receives consume failures (default slog.Default()).
	Logger *slog.Logger
}

// Adapter delivers events over Redis Streams.
type Adapter struct {
	client   *redis.Client
	group    string
	consumer string
	block    time.Duration
	logger   *slog.Logger

	mu      sync.Mutex
	cancels []context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a Redis Streams adapter. The client is not owned by the
// adapter: Close stops consumption but leaves the connection to the caller.
func New(client *redis.Client, opts Options) *Adapter {
	a := &Adapter{
		client:   client,
		group:    opts.Group,
		consumer: opts.Consumer,
		block:    opts.Block,
		logger:   opts.Logger,
	}
	if a.group == "" {
		a.group = DefaultGroup
	}
	if a.consumer == "" {
		host, _ := os.Hostname()
		a.consumer = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	if a.block <= 0 {
		a.block = 5 * time.Second
	}
	if a.logger == nil {
		a.logger = slog.Default()
	}
	return a
}

// Name implements event.EventAdapter.
func (a *Adapter) Name() string { return "redis" }

// Synchronous implements event.EventAdapter.
func (a *Adapter) Synchronous() bool { return false }

// Publish XADDs the event to its topic's stream and stamps the entry ID.
func (a *Adapter) Publish(ctx context.Context, e *event.Event) error {
	topic, err := event.TopicFor(e.Type)
	if err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("event: marshal: %w", err)
	}
	id, err := a.client.XAdd(ctx, &redis.XAddArgs{
		Stream: topic,
		Values: map[string]any{"type": string(e.Type), "data": data},
	}).Result()
	if err != nil {
		return fmt.Errorf("event: xadd: %w", err)
	}
	e.ID = id
	return nil
}

// Subscribe ensures the consumer group exists and starts consuming in the
// background until ctx is cancelled.
func (a *Adapter) Subscribe(ctx context.Context, topic string, h event.Handler) error {
	err := a.client.XGroupCreateMkStream(ctx, topic, a.group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("event: xgroup: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.mu.Lock()
	a.cancels = append(a.cancels, cancel)
	a.mu.Unlock()
	a.wg.Add(1)
	go a.consume(runCtx, topic, h)
	return nil
}

// Ack implements event.EventAdapter: XACKs the event's stream entry.
func (a *Adapter) Ack(ctx context.Context, e *event.Event) error {
	topic, err := event.TopicFor(e.Type)
	if err != nil {
		return err
	}
	return a.client.XAck(ctx, topic, a.group, e.ID).Err()
}

// Close stops all consume loops and waits for in-flight handlers.
func (a *Adapter) Close() error {
	a.mu.Lock()
	cancels := a.cancels
	a.cancels = nil
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	a.wg.Wait()
	return nil
}

func (a *Adapter) consume(ctx context.Context, topic string, h event.Handler) {
	defer a.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := a.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    a.group,
			Consumer: a.consumer,
			Streams:  []string{topic, ">"},
			Count:    16,
			Block:    a.block,
		}).Result()
		switch {
		case err == nil:
			for _, stream := range res {
				for _, msg := range stream.Messages {
					a.handle(ctx, topic, h, msg)
				}
			}
		case errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled):
			// Block timeout or shutdown — just re-check ctx.
			time.Sleep(10 * time.Millisecond)
			continue
		case ctx.Err() != nil:
			return
		default:
			a.logger.Error("event: read group failed", "topic", topic, "error", err)
			time.Sleep(a.block)
		}

		a.claimStale(ctx, topic, h)
	}
}

// claimStale re-claims entries left pending by failed handlers or dead
// consumers and replays them. Entries exceeding maxRedelivery are acked and
// logged (dead-letter by log).
func (a *Adapter) claimStale(ctx context.Context, topic string, h event.Handler) {
	a.deadLetter(ctx, topic)

	claimed, _, err := a.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   topic,
		Group:    a.group,
		Consumer: a.consumer,
		MinIdle:  retryAfter,
		Start:    "0-0",
		Count:    16,
	}).Result()
	if err != nil {
		if !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			a.logger.Error("event: auto claim failed", "topic", topic, "error", err)
		}
		return
	}
	for _, msg := range claimed {
		a.handle(ctx, topic, h, msg)
	}
}

// deadLetter acks pending entries that exhausted maxRedelivery so a
// permanently failing event cannot loop forever.
func (a *Adapter) deadLetter(ctx context.Context, topic string) {
	pending, err := a.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: topic,
		Group:  a.group,
		Idle:   retryAfter,
		Start:  "-",
		End:    "+",
		Count:  16,
	}).Result()
	if err != nil {
		return
	}
	for _, p := range pending {
		if p.RetryCount < maxRedelivery {
			continue
		}
		a.logger.Error("event: dead-letter after max redelivery",
			"topic", topic, "id", p.ID, "deliveries", p.RetryCount)
		a.client.XAck(ctx, topic, a.group, p.ID)
	}
}

func (a *Adapter) handle(ctx context.Context, topic string, h event.Handler, msg redis.XMessage) {
	raw, _ := msg.Values["data"].(string)
	var e event.Event
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		// Poison payload: it can never succeed, so ack it and move on.
		a.logger.Error("event: undecodable payload, dropping", "topic", topic, "id", msg.ID, "error", err)
		a.client.XAck(ctx, topic, a.group, msg.ID)
		return
	}
	e.ID = msg.ID

	if err := h(ctx, &e); err != nil {
		a.logger.Error("event: handler failed, left pending",
			"topic", topic, "id", msg.ID, "type", string(e.Type), "error", err)
		return
	}
	if err := a.client.XAck(ctx, topic, a.group, msg.ID).Err(); err != nil {
		a.logger.Error("event: ack failed", "topic", topic, "id", msg.ID, "error", err)
	}
}
