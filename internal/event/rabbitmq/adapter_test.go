package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/cuihairu/atlas/internal/event"
)

// fakeChannel records topology calls, captures publishes, and serves a
// controllable delivery stream — enough to exercise the adapter's logic
// against an in-memory stand-in for *amqp091.Channel.
type fakeChannel struct {
	mu        sync.Mutex
	declared  []string // exchange/queue declare names in call order
	boundKey  string
	qos       int
	published []amqp.Publishing
	publishID string

	deliveries  chan amqp.Delivery
	acked       []uint64
	nacked      []uint64
	nackRequeue bool
	closed      bool
}

func newFakeChannel() *fakeChannel {
	return &fakeChannel{deliveries: make(chan amqp.Delivery, 16)}
}

func (c *fakeChannel) ExchangeDeclare(name, _ string, _, _, _, _ bool, _ amqp.Table) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.declared = append(c.declared, "exchange:"+name)
	return nil
}

func (c *fakeChannel) QueueDeclare(name string, _, _, _, _ bool, _ amqp.Table) (amqp.Queue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.declared = append(c.declared, "queue:"+name)
	return amqp.Queue{Name: name}, nil
}

func (c *fakeChannel) QueueBind(queue, key, _ string, _ bool, _ amqp.Table) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.boundKey = key
	c.declared = append(c.declared, "bind:"+queue)
	return nil
}

func (c *fakeChannel) Qos(count, _ int, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.qos = count
	return nil
}

func (c *fakeChannel) PublishWithContext(_ context.Context, _, key string, _, _ bool, msg amqp.Publishing) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.published = append(c.published, msg)
	c.publishID = msg.MessageId
	_ = key
	return nil
}

func (c *fakeChannel) Consume(queue string, _ string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	return c.deliveries, nil
}

func (c *fakeChannel) Ack(tag uint64, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acked = append(c.acked, tag)
	return nil
}

func (c *fakeChannel) Nack(tag uint64, _, requeue bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nacked = append(c.nacked, tag)
	c.nackRequeue = requeue
	return nil
}

func (c *fakeChannel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeChannel) stats() (published int, acked int, nacked int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.published), len(c.acked), len(c.nacked)
}

func newTestAdapter(t *testing.T) (*Adapter, *fakeChannel) {
	t.Helper()
	ch := newFakeChannel()
	a := New(ch, Options{Consumer: "test-consumer", RequeueBackoff: time.Millisecond})
	t.Cleanup(func() { _ = a.Close() })
	return a, ch
}

func TestPublishSendsPersistentMessageWithID(t *testing.T) {
	a, ch := newTestAdapter(t)
	e := &event.Event{Type: event.EventCharacterCreated, AccountID: 42, CharacterID: 1001, Name: "Hero"}

	if err := a.Publish(context.Background(), e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if e.ID == "" || e.ID != ch.publishID {
		t.Fatalf("event ID %q should equal the published MessageId %q", e.ID, ch.publishID)
	}
	published, _, _ := ch.stats()
	if published != 1 {
		t.Fatalf("published = %d, want 1", published)
	}
	msg := ch.published[0]
	if msg.DeliveryMode != amqp.Persistent {
		t.Fatalf("delivery mode = %d, want persistent", msg.DeliveryMode)
	}
	if msg.ContentType != "application/json" {
		t.Fatalf("content type = %q", msg.ContentType)
	}
	var decoded event.Event
	if err := json.Unmarshal(msg.Body, &decoded); err != nil {
		t.Fatalf("payload not valid event JSON: %v", err)
	}
	if decoded.CharacterID != 1001 {
		t.Fatalf("payload lost fields: %+v", decoded)
	}
}

func TestSubscribeDeclaresDurableTopology(t *testing.T) {
	a, ch := newTestAdapter(t)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// ExchangeDeclare only happens in Open; Subscribe declares the queue
	// and binds it. Assert what the adapter guarantees.
	if ch.boundKey != event.TopicCharacters {
		t.Fatalf("bound routing key = %q, want %q", ch.boundKey, event.TopicCharacters)
	}
	if ch.qos <= 0 {
		t.Fatalf("qos = %d, want prefetch set", ch.qos)
	}
	foundQueue, foundBind := false, false
	for _, d := range ch.declared {
		if d == "queue:"+event.TopicCharacters {
			foundQueue = true
		}
		if d == "bind:"+event.TopicCharacters {
			foundBind = true
		}
	}
	if !foundQueue || !foundBind {
		t.Fatalf("topology calls = %v, want queue + bind declared", ch.declared)
	}
}

func TestSubscribeDeliversAndAcks(t *testing.T) {
	a, ch := newTestAdapter(t)
	got := make(chan *event.Event, 1)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterLogin, CharacterID: 7})
	ch.deliveries <- amqp.Delivery{MessageId: "m-1", Body: payload, DeliveryTag: 1}

	select {
	case e := <-got:
		if e.CharacterID != 7 {
			t.Fatalf("delivered character_id = %d, want 7", e.CharacterID)
		}
		if e.ID != "m-1" {
			t.Fatalf("delivered ID = %q, want m-1", e.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, acked, _ := ch.stats()
		if acked == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delivery not acked after successful handler")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHandlerFailureNacksWithRequeue(t *testing.T) {
	a, ch := newTestAdapter(t)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return errors.New("apply failed")
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterDeleted, CharacterID: 3})
	ch.deliveries <- amqp.Delivery{MessageId: "m-2", Body: payload, DeliveryTag: 2}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, _, nacked := ch.stats()
		if nacked == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failed delivery not nacked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ch.nackRequeue {
		t.Fatal("failure should nack with requeue")
	}
}

func TestPoisonPayloadIsAckedAndDropped(t *testing.T) {
	a, ch := newTestAdapter(t)
	called := 0
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		called++
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ch.deliveries <- amqp.Delivery{MessageId: "m-3", Body: []byte("not-json"), DeliveryTag: 3}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, acked, _ := ch.stats()
		if acked == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("poison payload not acked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if called != 0 {
		t.Fatalf("handler called %d times for poison payload, want 0", called)
	}
}

func TestCloseStopsConsumptionAndClosesChannel(t *testing.T) {
	ch := newFakeChannel()
	a := New(ch, Options{})
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ch.mu.Lock()
	closed := ch.closed
	ch.mu.Unlock()
	if !closed {
		t.Fatal("channel not closed")
	}
}

func TestPublishRejectsUnknownType(t *testing.T) {
	a, ch := newTestAdapter(t)
	if err := a.Publish(context.Background(), &event.Event{Type: "bogus"}); err == nil {
		t.Fatal("expected error for unknown event type")
	}
	if published, _, _ := ch.stats(); published != 0 {
		t.Fatalf("published = %d, want 0", published)
	}
}

func TestOpenDialFailureAndAccessors(t *testing.T) {
	if _, err := Open(Options{URL: "amqp://127.0.0.1:1/"}); err == nil ||
		!strings.Contains(err.Error(), "amqp dial") {
		t.Errorf("Open to closed port = %v, want dial failure", err)
	}

	a := New(nil, Options{})
	if a.Name() != "rabbitmq" {
		t.Errorf("Name = %q", a.Name())
	}
	if a.Synchronous() {
		t.Error("Synchronous = true, want false")
	}
	if err := a.Ack(context.Background(), &event.Event{}); err != nil {
		t.Errorf("Ack (no-op) = %v", err)
	}
}
