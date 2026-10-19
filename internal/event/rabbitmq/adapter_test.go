package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	publishKeys []string
	publishID string

	deliveries  chan amqp.Delivery
	acked       []uint64
	nacked      []uint64
	nackRequeue bool
	closed      bool

	// injectable failures for the declare/bind/qos/consume/publish/ack
	// error paths
	errQueueDeclare error
	errBind         error
	errQos          error
	errConsume      error
	errPublish      error
	errAck          error
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
	if c.errQueueDeclare != nil {
		return amqp.Queue{}, c.errQueueDeclare
	}
	return amqp.Queue{Name: name}, nil
}

func (c *fakeChannel) QueueBind(queue, key, _ string, _ bool, _ amqp.Table) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.boundKey = key
	c.declared = append(c.declared, "bind:"+queue)
	if c.errBind != nil {
		return c.errBind
	}
	return nil
}

func (c *fakeChannel) Qos(count, _ int, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.qos = count
	if c.errQos != nil {
		return c.errQos
	}
	return nil
}

func (c *fakeChannel) PublishWithContext(_ context.Context, _, key string, _, _ bool, msg amqp.Publishing) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errPublish != nil {
		return c.errPublish
	}
	c.published = append(c.published, msg)
	c.publishKeys = append(c.publishKeys, key)
	c.publishID = msg.MessageId
	return nil
}

func (c *fakeChannel) Consume(queue string, _ string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	if c.errConsume != nil {
		return nil, c.errConsume
	}
	return c.deliveries, nil
}

func (c *fakeChannel) Ack(tag uint64, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errAck != nil {
		return c.errAck
	}
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

func TestSubscribeSetupFailures(t *testing.T) {
	// Each topology step (declare queue → bind → qos → consume) fails
	// Subscribe with a step-specific error before any loop starts.
	boom := errors.New("broker refused")
	cases := []struct {
		name string
		arm  func(*fakeChannel)
		want string
	}{
		{"queue declare", func(c *fakeChannel) { c.errQueueDeclare = boom }, "event: amqp declare queue"},
		{"queue bind", func(c *fakeChannel) { c.errBind = boom }, "event: amqp bind queue"},
		{"qos", func(c *fakeChannel) { c.errQos = boom }, "event: amqp qos"},
		{"consume", func(c *fakeChannel) { c.errConsume = boom }, "event: amqp consume"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := newFakeChannel()
			tc.arm(ch)
			a := New(ch, Options{Consumer: "test-consumer", RequeueBackoff: time.Millisecond})
			t.Cleanup(func() { _ = a.Close() })
			err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
				return nil
			})
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %s failure", err, tc.want)
			}
		})
	}
}

func TestPublishChannelError(t *testing.T) {
	boom := errors.New("connection dropped")
	ch := newFakeChannel()
	ch.errPublish = boom
	a := New(ch, Options{Consumer: "test-consumer", RequeueBackoff: time.Millisecond})
	t.Cleanup(func() { _ = a.Close() })
	err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterCreated, CharacterID: 1})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "event: amqp publish") {
		t.Errorf("err = %v, want amqp publish failure", err)
	}
}

func TestClosedDeliveriesEndsConsumeLoop(t *testing.T) {
	// The broker closing the delivery channel (connection loss, shutdown)
	// must end the loop without wedging Close.
	a, ch := newTestAdapter(t)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	close(ch.deliveries)
	done := make(chan struct{})
	go func() { _ = a.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked after deliveries channel closed")
	}
}

func TestAckFailureLogged(t *testing.T) {
	// An ack that cannot reach the broker is logged, not swallowed: the
	// message will be redelivered, and operators need to see why.
	var buf syncBuffer
	ch := newFakeChannel()
	a := New(ch, Options{Consumer: "test-consumer", RequeueBackoff: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ch.errAck = errors.New("socket closed")
	ch.deliveries <- amqp.Delivery{MessageId: "m-1", Body: []byte(`{"type":"character.created","character_id":5}`)}

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(buf.String(), "event: ack failed") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "event: ack failed") {
		t.Fatalf("ack failure never logged, buffer = %q", buf.String())
	}
}

// TestDeliveryCapDeadLettersAfterThreshold pins the opt-in cap
// (ATLAS_EVENT_MAX_DELIVERY): after MaxDelivery failed deliveries of the
// same message the copy goes to the "<topic>.dl" queue and the original is
// acked — no more Nack(requeue) loops; without the option every failure
// requeues (pinned by the requeue-backoff test).
func TestDeliveryCapDeadLettersAfterThreshold(t *testing.T) {
	a, ch := newTestAdapter(t)
	a.maxDelivery = 2

	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return errors.New("apply failed")
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// The dead-letter destination is declared up front, like the main queue.
	declared := false
	ch.mu.Lock()
	for _, d := range ch.declared {
		if d == "queue:"+event.DeadLetterTopic(event.TopicCharacters) {
			declared = true
		}
	}
	ch.mu.Unlock()
	if !declared {
		t.Fatal("dead-letter queue was not declared with the topic")
	}

	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterUpdated, CharacterID: 9})
	delivery := amqp.Delivery{MessageId: "m1", Type: string(event.EventCharacterUpdated), Body: payload}
	ch.deliveries <- delivery // first delivery — requeued
	ch.deliveries <- delivery // redelivery — hits the cap

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		published, acked, nacked := ch.stats()
		if published == 1 && acked == 1 && nacked == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	published, acked, nacked := ch.stats()
	if published != 1 {
		t.Fatalf("published = %d, want 1 (the dead-letter copy)", published)
	}
	if acked != 1 || nacked != 1 {
		t.Fatalf("acked = %d nacked = %d, want 1/1 (first failure requeues, cap acks)", acked, nacked)
	}
	ch.mu.Lock()
	key := ch.publishKeys[0]
	body := string(ch.published[0].Body)
	ch.mu.Unlock()
	if key != event.DeadLetterTopic(event.TopicCharacters) {
		t.Fatalf("dead-letter routing key = %q, want atlas.characters.dl", key)
	}
	if body != string(payload) {
		t.Fatalf("dead-letter body = %s, want the original %s", body, payload)
	}
}
