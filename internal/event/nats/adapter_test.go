package nats

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/cuihairu/atlas/internal/event"
)

// startServer boots an in-process NATS server with JetStream enabled —
// the same mechanism the NATS project ships for its own tests.
func startServer(t *testing.T) *natsserver.Server {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready")
	}
	return srv
}

func openAdapter(t *testing.T) *Adapter {
	t.Helper()
	a, err := Open(Options{URL: startServer(t).ClientURL()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestPublishStampsSequenceAndDelivers(t *testing.T) {
	a := openAdapter(t)

	got := make(chan *event.Event, 1)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	in := &event.Event{Type: event.EventCharacterCreated, AccountID: 42, CharacterID: 1001, Name: "Hero"}
	if err := a.Publish(context.Background(), in); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if in.ID == "" {
		t.Fatal("publish should stamp the stream sequence as event ID")
	}

	select {
	case e := <-got:
		if e.CharacterID != 1001 || e.Name != "Hero" {
			t.Fatalf("delivered event lost fields: %+v", e)
		}
		if e.ID != in.ID {
			t.Fatalf("delivered ID = %q, want %q", e.ID, in.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestHandlerFailureLeavesMessageUnacked(t *testing.T) {
	a := openAdapter(t)

	var calls atomic.Int32
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		calls.Add(1)
		return context.DeadlineExceeded // simulate apply failure
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated, CharacterID: 9}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Wait for the handler first: it is the cause of the unacked state, so
	// asserting on pending counters before it runs is racy — NumPending is
	// already 1 once the message lands in the stream, while the pull fetch
	// may not have delivered it to the handler yet.
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("handler should have been called once")
	}

	// The message must stay pending (unacked) after the handler fails —
	// fetched-but-unacked shows up in NumAckPending.
	deadline = time.Now().Add(3 * time.Second)
	var pending int64
	for time.Now().Before(deadline) {
		info, err := a.js.ConsumerInfo(a.stream, a.durable)
		if err == nil && info.NumAckPending+int(info.NumPending) > 0 {
			pending = int64(info.NumAckPending) + int64(info.NumPending)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pending == 0 {
		t.Fatal("expected unacked pending message after handler failure")
	}
}

func TestPoisonPayloadIsDroppedAndAcked(t *testing.T) {
	a := openAdapter(t)

	var called atomic.Int32
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		called.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Probe with a well-formed event first: it proves the consume loop is
	// live, so the "poison never reached the handler" assertion below
	// cannot pass vacuously by racing ahead of the consumer.
	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated, CharacterID: 1}); err != nil {
		t.Fatalf("probe publish: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for called.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if called.Load() != 1 {
		t.Fatalf("probe handler calls = %d, want 1", called.Load())
	}

	// Raw-publish an undecodable payload on the same subject.
	if _, err := a.js.Publish(event.TopicCharacters, []byte("not-json")); err != nil {
		t.Fatalf("raw publish: %v", err)
	}

	// The poison message must be acked (drained to zero pending) without
	// ever reaching the handler.
	deadline = time.Now().Add(3 * time.Second)
	var pending uint64
	for time.Now().Before(deadline) {
		info, err := a.js.ConsumerInfo(a.stream, a.durable)
		if err == nil {
			pending = uint64(info.NumAckPending) + info.NumPending
			if pending == 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := called.Load(); n != 1 {
		t.Fatalf("handler called %d times, want 1 (poison payload must not be delivered)", n)
	}
	if pending != 0 {
		t.Fatalf("poison payload still pending: %d", pending)
	}
}

func TestPayloadRoundTripPreservesOptionalFields(t *testing.T) {
	a := openAdapter(t)

	got := make(chan *event.Event, 1)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	lvl := 10
	if err := a.Publish(context.Background(), &event.Event{
		Type: event.EventCharacterMoved, CharacterID: 5, ServerID: "s1",
		TargetServerID: "s2", Level: &lvl,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case e := <-got:
		if e.TargetServerID != "s2" || e.Level == nil || *e.Level != 10 {
			t.Fatalf("optional fields lost: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestOpenConnectFailureAndAccessors(t *testing.T) {
	if _, err := Open(Options{URL: "nats://127.0.0.1:1"}); err == nil ||
		!strings.Contains(err.Error(), "nats connect") {
		t.Errorf("Open to closed port = %v, want connect failure", err)
	}

	a := New(nil, nil, Options{})
	if a.Name() != "nats" {
		t.Errorf("Name = %q", a.Name())
	}
	if a.Synchronous() {
		t.Error("Synchronous = true, want false")
	}
	if err := a.Ack(context.Background(), &event.Event{}); err != nil {
		t.Errorf("Ack (no-op) = %v", err)
	}
}
