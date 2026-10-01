package redis

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/event"
)

func newTestAdapter(t *testing.T, opts Options) (*Adapter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return New(client, opts), mr
}

func TestPublishConsumeAck(t *testing.T) {
	a, _ := newTestAdapter(t, Options{Block: 50 * time.Millisecond})
	ctx := context.Background()

	got := make(chan *event.Event, 1)
	if err := a.Subscribe(ctx, event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	in := &event.Event{
		Type:        event.EventCharacterCreated,
		AccountID:   10001,
		ServerID:    "game-1001",
		CharacterID: 823712,
		Name:        "剑无尘",
	}
	if err := a.Publish(ctx, in); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if in.ID == "" {
		t.Fatalf("expected stream entry id stamped on publish")
	}

	select {
	case e := <-got:
		if e.Type != in.Type || e.AccountID != in.AccountID ||
			e.ServerID != in.ServerID || e.CharacterID != in.CharacterID {
			t.Fatalf("event mismatch: %+v", e)
		}
		if e.ID != in.ID {
			t.Fatalf("delivery id %q != published id %q", e.ID, in.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event")
	}

	// Handler succeeded: the entry must be acked shortly after.
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending, err := a.client.XPending(ctx, event.TopicCharacters, a.group).Result()
		if err != nil {
			t.Fatalf("xpending: %v", err)
		}
		if pending.Count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected empty PEL, still %d entries", pending.Count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFailedHandlerStaysPending(t *testing.T) {
	a, _ := newTestAdapter(t, Options{Block: 50 * time.Millisecond})
	ctx := context.Background()

	want := errors.New("boom")
	var mu sync.Mutex
	var calls int
	if err := a.Subscribe(ctx, event.TopicCharacters, func(_ context.Context, _ *event.Event) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return want
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := a.Publish(ctx, &event.Event{Type: event.EventCharacterLogin, CharacterID: 1}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := calls
		mu.Unlock()
		if c > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	pending, err := a.client.XPending(ctx, event.TopicCharacters, a.group).Result()
	if err != nil {
		t.Fatalf("xpending: %v", err)
	}
	if pending.Count != 1 {
		t.Fatalf("expected failed event to stay pending, PEL=%d", pending.Count)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestPublishUnknownType(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	err := a.Publish(context.Background(), &event.Event{Type: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestAckExplicit(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	ctx := context.Background()

	// Group exists but the entry was never delivered through Subscribe:
	// Ack must still succeed (XACK of an unknown ID is a no-op).
	if err := a.client.XGroupCreateMkStream(ctx, event.TopicCharacters, a.group, "0").Err(); err != nil {
		t.Fatalf("xgroup: %v", err)
	}
	e := &event.Event{Type: event.EventCharacterDeleted, CharacterID: 5}
	if err := a.Publish(ctx, e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := a.Ack(ctx, e); err != nil {
		t.Fatalf("ack: %v", err)
	}
}
