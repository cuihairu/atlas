// Counting adapter: wraps any EventAdapter with per-topic publish / consume
// / in-flight counters — the data face behind the 消息总线 observability
// panel (TODO 观测深化). Works for the in-process http adapter and the queue
// adapters alike: consumption is counted where handlers actually run.
package event

import (
	"context"
	"sort"
	"sync"
)

// TopicStats is one topic's counters.
type TopicStats struct {
	Topic     string `json:"topic"`
	Published int64  `json:"published"`
	Consumed  int64  `json:"consumed"`
	// InFlight is the momentary publish concurrency — the backlog depth a
	// synchronous (in-process) pipeline can show. Queue adapters surface
	// their own lag surfaces; this wrapper reports what passed through.
	InFlight int64 `json:"in_flight"`
}

// CountingAdapter decorates an EventAdapter with per-topic counters.
type CountingAdapter struct {
	EventAdapter

	mu        sync.Mutex
	published map[string]int64
	consumed  map[string]int64
	inFlight  map[string]int64
}

// WrapCounting wraps inner with per-topic counters. Publish counting is
// exact; consume counting happens in the Subscribe wrapper, so both the
// synchronous (in-process) and the queue adapters count once per handler
// invocation — never twice.
func WrapCounting(inner EventAdapter) *CountingAdapter {
	return &CountingAdapter{
		EventAdapter: inner,
		published:    map[string]int64{},
		consumed:     map[string]int64{},
		inFlight:     map[string]int64{},
	}
}

func topicOf(e *Event) string {
	topic, err := TopicFor(e.Type)
	if err != nil {
		return "atlas.unknown"
	}
	return topic
}

func (c *CountingAdapter) Publish(ctx context.Context, e *Event) error {
	topic := topicOf(e)
	c.bump(c.inFlight, topic, 1)
	defer c.bump(c.inFlight, topic, -1)
	c.bump(c.published, topic, 1)
	return c.EventAdapter.Publish(ctx, e)
}

func (c *CountingAdapter) Subscribe(ctx context.Context, topic string, h Handler) error {
	return c.EventAdapter.Subscribe(ctx, topic, func(ctx context.Context, e *Event) error {
		err := h(ctx, e)
		if err == nil {
			c.bump(c.consumed, topic, 1)
		}
		return err
	})
}

func (c *CountingAdapter) bump(m map[string]int64, topic string, delta int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m[topic] += delta
}

// Stats snapshots the per-topic counters, topics sorted.
func (c *CountingAdapter) Stats() []TopicStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	topics := make(map[string]bool, len(c.published))
	for t := range c.published {
		topics[t] = true
	}
	for t := range c.consumed {
		topics[t] = true
	}
	out := make([]TopicStats, 0, len(topics))
	for t := range topics {
		out = append(out, TopicStats{
			Topic: t, Published: c.published[t], Consumed: c.consumed[t], InFlight: c.inFlight[t],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Topic < out[j].Topic })
	return out
}
