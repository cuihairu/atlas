package redis

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
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

// TestAdapterBasics pins the adapter identity: async by definition (the
// consumer loop owns delivery) and named for the bus selection env.
func TestAdapterBasics(t *testing.T) {
	a := &Adapter{}
	if a.Name() != "redis" {
		t.Errorf("Name() = %q, want redis", a.Name())
	}
	if a.Synchronous() {
		t.Error("Synchronous() = true, want false (delivery is async)")
	}
}

// syncBuffer captures log output for assertions.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func pendingCount(t *testing.T, a *Adapter) int64 {
	t.Helper()
	p, err := a.client.XPending(context.Background(), event.TopicCharacters, a.group).Result()
	if err != nil {
		t.Fatalf("xpending: %v", err)
	}
	return p.Count
}

// fastAdapter returns an adapter whose dialer fails within milliseconds, so
// tests that need broker errors do not wait for the connection pool to
// exhaust its retries.
func fastAdapter(t *testing.T, buf *syncBuffer) (*Adapter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: time.Millisecond}
			return d.DialContext(ctx, network, addr)
		},
		MaxRetries:         -1,
		MinRetryBackoff:    time.Millisecond,
		MaxRetryBackoff:    time.Millisecond,
		DialerRetryBackoff: func(int) time.Duration { return time.Millisecond },
	})
	t.Cleanup(func() { client.Close() })
	return New(client, Options{Block: 10 * time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(buf, nil))}), mr
}

func TestStaleClaimReplaysAfterRetryInterval(t *testing.T) {
	// A failed handler leaves the entry pending; once its idle time passes
	// retryAfter, claimStale re-claims it and the replay succeeds — delivery
	// is at-least-once, not exactly-once.
	var buf syncBuffer
	a, mr := newTestAdapter(t, Options{Block: 30 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	ctx := context.Background()

	var calls atomic.Int32
	if err := a.Subscribe(ctx, event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		if calls.Add(1) == 1 {
			return errors.New("transient failure")
		}
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := a.Publish(ctx, &event.Event{Type: event.EventCharacterCreated, AccountID: 1, CharacterID: 500}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitFor(t, "first failed delivery pending", func() bool { return pendingCount(t, a) == 1 })

	// Age the pending entry past retryAfter (30s).
	mr.SetTime(time.Now().Add(31 * time.Second))
	waitFor(t, "replayed entry acked", func() bool { return pendingCount(t, a) == 0 })

	if got := calls.Load(); got != 2 {
		t.Errorf("handler calls = %d, want 2 (fail, then replay success)", got)
	}
	if s := buf.String(); strings.Contains(s, "dead-letter") {
		t.Errorf("first retry must not dead-letter: %q", s)
	}
}

func TestDeadLetterAfterMaxRedelivery(t *testing.T) {
	// A pending entry whose delivery count reached maxRedelivery is acked
	// with a dead-letter log on the next sweep — a poison event must not
	// loop forever. The delivery count is seeded with XCLAIM RETRYCOUNT
	// (what a real broker accumulates across re-claims).
	var buf syncBuffer
	a, mr := newTestAdapter(t, Options{Block: 30 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	ctx := context.Background()

	var calls atomic.Int32
	if err := a.Subscribe(ctx, event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		calls.Add(1)
		return errors.New("poison handler")
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	in := &event.Event{Type: event.EventCharacterCreated, AccountID: 1, CharacterID: 501}
	if err := a.Publish(ctx, in); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitFor(t, "failed delivery pending", func() bool { return pendingCount(t, a) == 1 })

	// Seed the delivery count past maxRedelivery (5), claimed by another
	// consumer the way a broker-side redelivery chain would.
	if err := a.client.Do(ctx, "XCLAIM", event.TopicCharacters, a.group,
		"dead-claimed", "0", in.ID, "RETRYCOUNT", "5").Err(); err != nil {
		t.Fatalf("xclaim: %v", err)
	}

	// Age the entry past retryAfter so the sweep's XPENDING IDLE filter
	// picks it up; deadLetter runs before claimStale, so the ack wins the
	// race against another replay.
	mr.SetTime(time.Now().Add(31 * time.Second))
	waitFor(t, "dead-letter log", func() bool {
		return strings.Contains(buf.String(), "dead-letter after max redelivery")
	})
	waitFor(t, "dead-letter acked away", func() bool { return pendingCount(t, a) == 0 })

	// Dead-lettering must end the retries.
	time.Sleep(200 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("handler calls = %d, want 1 (no replay after dead-letter)", got)
	}
}

func TestBrokerErrors(t *testing.T) {
	// Ack with an unknown event type cannot even resolve a stream.
	a, mr := newTestAdapter(t, Options{})
	if err := a.Ack(context.Background(), &event.Event{Type: "bogus", ID: "1-1"}); err == nil {
		t.Error("Ack with unknown type must fail")
	}
	if err := a.Close(); err != nil {
		t.Errorf("close: %v", err)
	}

	// With the broker gone: XAdd fails, group creation fails with a
	// non-BUSYGROUP error, and the consume loop logs read and claim
	// failures instead of dying silently.
	mr.Close()
	ctx := context.Background()
	if err := a.Publish(ctx, &event.Event{Type: event.EventCharacterCreated, CharacterID: 1}); err == nil {
		t.Error("Publish without broker must fail")
	}
	if err := a.Subscribe(ctx, event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err == nil {
		t.Error("Subscribe without broker must fail")
	}

	// Consume-loop resilience against a dead broker: read-group and
	// auto-claim failures are logged, the loop keeps running until Close.
	var buf syncBuffer
	c, mr2 := fastAdapter(t, &buf)
	if err := c.Subscribe(ctx, event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	mr2.Close()
	waitFor(t, "read group failure logged", func() bool {
		return strings.Contains(buf.String(), "event: read group failed")
	})
	waitFor(t, "auto claim failure logged", func() bool {
		return strings.Contains(buf.String(), "event: auto claim failed")
	})
	if err := c.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}
