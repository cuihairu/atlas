// Package http implements the synchronous in-process event adapter.
//
// It preserves the v0.1 behavior described in docs/sync.md §5: a published
// event is dispatched to its subscriber on the calling goroutine, so the
// write lands in the store before Publish returns and errors propagate to
// the caller. There is no bus, no buffering, no redelivery.
package http

import (
	"context"
	"fmt"
	"sync"

	"github.com/cuihairu/atlas/internal/event"
)

// Adapter dispatches events to in-process handlers.
type Adapter struct {
	mu       sync.RWMutex
	handlers map[string]event.Handler
}

// New creates an empty synchronous adapter.
func New() *Adapter {
	return &Adapter{handlers: make(map[string]event.Handler)}
}

// Name implements event.EventAdapter.
func (a *Adapter) Name() string { return "http" }

// Synchronous implements event.EventAdapter.
func (a *Adapter) Synchronous() bool { return true }

// Publish invokes the topic subscriber inline and returns its error.
// Events for topics without a subscriber are dropped silently: the HTTP
// transport is the write path itself, and reads need no fan-out.
func (a *Adapter) Publish(ctx context.Context, e *event.Event) error {
	topic, err := event.TopicFor(e.Type)
	if err != nil {
		return err
	}
	h := a.subscriber(topic)
	if h == nil {
		return fmt.Errorf("%w: %s", event.ErrNoSubscriber, topic)
	}
	return h(ctx, e)
}

// Subscribe implements event.EventAdapter. The context is accepted for
// interface parity; in-process handlers live as long as the adapter.
func (a *Adapter) Subscribe(_ context.Context, topic string, h event.Handler) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers[topic] = h
	return nil
}

// Ack implements event.EventAdapter. In-process dispatch is acknowledged
// on return.
func (a *Adapter) Ack(_ context.Context, _ *event.Event) error { return nil }

// Close implements event.EventAdapter. Nothing to release.
func (a *Adapter) Close() error { return nil }

func (a *Adapter) subscriber(topic string) event.Handler {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.handlers[topic]
}
