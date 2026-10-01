package nats

import (
	"context"
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

	// The message must stay pending (unacked) after the handler fails —
	// fetched-but-unacked shows up in NumAckPending.
	deadline := time.Now().Add(3 * time.Second)
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
	if calls.Load() == 0 {
		t.Fatal("handler should have been called once")
	}
}

func TestPoisonPayloadIsDroppedAndAcked(t *testing.T) {
	a := openAdapter(t)

	called := 0
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		called++
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Raw-publish an undecodable payload on the same subject.
	if _, err := a.js.Publish(event.TopicCharacters, []byte("not-json")); err != nil {
		t.Fatalf("raw publish: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var pending uint64
	for time.Now().Before(deadline) {
		info, err := a.js.ConsumerInfo(a.stream, a.durable)
		if err == nil && info.NumAckPending == 0 && info.NumPending == 0 {
			pending = 0
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if called != 0 {
		t.Fatalf("handler called %d times for poison payload, want 0", called)
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
