package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/cuihairu/atlas/internal/event"
)

// fakeWriter records published messages; Partition/Offset are stamped like
// the real writer does, and messages are handed to the paired reader.
type fakeWriter struct {
	mu      sync.Mutex
	written []kafka.Message
	next    *fakeReader
	fail    error
}

func (w *fakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	if w.fail != nil {
		return w.fail
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range msgs {
		msgs[i].Partition = 0
		msgs[i].Offset = int64(len(w.written))
		w.written = append(w.written, msgs[i])
		if w.next != nil {
			w.next.push(msgs[i])
		}
	}
	return nil
}

func (w *fakeWriter) Close() error { return nil }

func (w *fakeWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.written)
}

// fakeReader pops from an internal queue, mirroring a group member's fetch.
type fakeReader struct {
	mu       sync.Mutex
	queue    []kafka.Message
	commits  []kafka.Message
	fetchErr error
	notify   chan struct{}
}

func (r *fakeReader) push(m kafka.Message) {
	r.mu.Lock()
	r.queue = append(r.queue, m)
	r.mu.Unlock()
	if r.notify != nil {
		select {
		case r.notify <- struct{}{}:
		default:
		}
	}
}

func (r *fakeReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	for {
		r.mu.Lock()
		if len(r.queue) > 0 {
			m := r.queue[0]
			r.queue = r.queue[1:]
			r.mu.Unlock()
			return m, nil
		}
		err := r.fetchErr
		r.mu.Unlock()
		if err != nil {
			return kafka.Message{}, err
		}
		select {
		case <-ctx.Done():
			return kafka.Message{}, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (r *fakeReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commits = append(r.commits, msgs...)
	return nil
}

func (r *fakeReader) Close() error { return nil }

func (r *fakeReader) commitCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.commits)
}

func newTestAdapter(t *testing.T) (*Adapter, *fakeWriter, *fakeReader) {
	t.Helper()
	w := &fakeWriter{}
	r := &fakeReader{}
	w.next = r
	a := New(w, r, Options{Block: 20 * time.Millisecond})
	t.Cleanup(func() { _ = a.Close() })
	return a, w, r
}

func TestPublishWritesTopicAndStampsID(t *testing.T) {
	a, w, _ := newTestAdapter(t)
	e := &event.Event{Type: event.EventCharacterCreated, AccountID: 42, CharacterID: 1001, Name: "Hero"}

	if err := a.Publish(context.Background(), e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if e.ID != "0@0" {
		t.Fatalf("event ID = %q, want partition@offset", e.ID)
	}
	if w.count() != 1 {
		t.Fatalf("writer has %d messages, want 1", w.count())
	}
	msg := w.written[0]
	if msg.Topic != event.TopicCharacters {
		t.Fatalf("topic = %q, want %q", msg.Topic, event.TopicCharacters)
	}
	var decoded event.Event
	if err := json.Unmarshal(msg.Value, &decoded); err != nil {
		t.Fatalf("payload not valid event JSON: %v", err)
	}
	if decoded.CharacterID != 1001 || decoded.Name != "Hero" {
		t.Fatalf("payload lost fields: %+v", decoded)
	}
}

func TestPublishRejectsUnknownType(t *testing.T) {
	a, w, _ := newTestAdapter(t)
	if err := a.Publish(context.Background(), &event.Event{Type: "bogus"}); err == nil {
		t.Fatal("expected error for unknown event type")
	}
	if w.count() != 0 {
		t.Fatalf("writer has %d messages, want 0", w.count())
	}
}

func TestSubscribeDeliversAndCommits(t *testing.T) {
	a, _, r := newTestAdapter(t)
	got := make(chan *event.Event, 1)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterLogin, CharacterID: 7})
	r.push(kafka.Message{Topic: event.TopicCharacters, Value: payload})

	select {
	case e := <-got:
		if e.CharacterID != 7 {
			t.Fatalf("delivered character_id = %d, want 7", e.CharacterID)
		}
		if e.ID == "" {
			t.Fatal("delivery should stamp the event ID")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}

	deadline := time.Now().Add(2 * time.Second)
	for r.commitCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.commitCount() != 1 {
		t.Fatalf("commits = %d, want 1 after successful handler", r.commitCount())
	}
}

func TestHandlerFailureLeavesOffsetUncommitted(t *testing.T) {
	a, _, r := newTestAdapter(t)
	calls := 0
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		calls++
		return errors.New("apply failed")
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterUpdated, CharacterID: 9})
	r.push(kafka.Message{Topic: event.TopicCharacters, Value: payload})

	time.Sleep(300 * time.Millisecond)
	if r.commitCount() != 0 {
		t.Fatalf("commits = %d, want 0 after handler failure", r.commitCount())
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestPoisonPayloadIsDroppedAndCommitted(t *testing.T) {
	a, _, r := newTestAdapter(t)
	called := 0
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		called++
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	r.push(kafka.Message{Topic: event.TopicCharacters, Value: []byte("not-json")})
	time.Sleep(300 * time.Millisecond)

	if called != 0 {
		t.Fatalf("handler called %d times for poison payload, want 0", called)
	}
	if r.commitCount() != 1 {
		t.Fatalf("commits = %d, want 1 (poison dropped)", r.commitCount())
	}
}

func TestCloseStopsConsumption(t *testing.T) {
	w := &fakeWriter{}
	r := &fakeReader{}
	w.next = r
	a := New(w, r, Options{Block: 20 * time.Millisecond})
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// After Close the consume loop is gone: a late push must not panic or
	// resurrect the loop, and Close is idempotent via interface nil checks.
	r.push(kafka.Message{Topic: event.TopicCharacters, Value: []byte("{}")})
	time.Sleep(100 * time.Millisecond)
}

// Open dials lazily in kafka-go, so it must succeed (and Close cleanly) even
// with an unreachable broker; the accessors pin the interface contract.
func TestOpenWithoutBrokerAndAccessors(t *testing.T) {
	a, err := Open(context.Background(), Options{Brokers: []string{"127.0.0.1:1"}})
	if err != nil {
		t.Fatalf("Open (lazy dial): %v", err)
	}
	if a.Name() != "kafka" {
		t.Errorf("Name = %q", a.Name())
	}
	if a.Synchronous() {
		t.Error("Synchronous = true, want false")
	}
	if err := a.Ack(context.Background(), &event.Event{}); err != nil {
		t.Errorf("Ack (no-op) = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Zero Options select the documented defaults.
	d := New(nil, nil, Options{})
	if d.group != DefaultGroup || d.block != defaultBlock || d.logger == nil {
		t.Errorf("defaults = group %q block %v logger %v", d.group, d.block, d.logger)
	}
}
