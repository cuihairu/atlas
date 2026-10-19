package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/cuihairu/atlas/internal/event"
)

// newReplayAdapter wires an adapter whose dead-letter drain wait is short
// enough for tests.
func newReplayAdapter(t *testing.T) (*Adapter, *fakeChannel) {
	t.Helper()
	ch := newFakeChannel()
	a := New(ch, Options{Consumer: "test-consumer", RequeueBackoff: time.Millisecond})
	a.drainWait = 10 * time.Millisecond
	t.Cleanup(func() { _ = a.Close() })
	return a, ch
}

func dlDelivery(tag uint64, e *event.Event) (amqp.Delivery, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return amqp.Delivery{}, err
	}
	return amqp.Delivery{MessageId: fmt.Sprintf("m-%d", tag), Body: data, DeliveryTag: tag}, nil
}

func TestReplayRepublishesAndAcks(t *testing.T) {
	a, ch := newReplayAdapter(t)
	d1, _ := dlDelivery(1, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 6101, Name: "A"})
	d2, _ := dlDelivery(2, &event.Event{Type: event.EventCharacterDeleted, AccountID: 7, CharacterID: 6102})
	ch.deliveries <- d1
	ch.deliveries <- d2

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Topic != event.TopicCharacters || rep.Found != 2 || rep.Republished != 2 || rep.Removed != 2 || rep.Failed() {
		t.Fatalf("report = %+v, want 2/2/2 clean", rep)
	}
	published, acked, _ := ch.stats()
	if published != 2 || acked != 2 {
		t.Fatalf("published %d / acked %d, want 2/2", published, acked)
	}
	ch.mu.Lock()
	for _, key := range ch.publishKeys {
		if key != event.TopicCharacters {
			t.Fatalf("replayed to routing key %q, want %q", key, event.TopicCharacters)
		}
	}
	var dlQueue, dlBind bool
	for _, d := range ch.declared {
		if d == "queue:"+event.DeadLetterTopic(event.TopicCharacters) {
			dlQueue = true
		}
		if d == "bind:"+event.DeadLetterTopic(event.TopicCharacters) {
			dlBind = true
		}
	}
	ch.mu.Unlock()
	if !dlQueue || !dlBind {
		t.Fatalf("dead-letter queue/bind not declared (declared = %v)", ch.declared)
	}
}

func TestReplayDryRunCountsOnly(t *testing.T) {
	a, ch := newReplayAdapter(t)
	d, _ := dlDelivery(1, &event.Event{Type: event.EventCharacterLogin, AccountID: 7, CharacterID: 6200})
	ch.deliveries <- d

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{DryRun: true})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 0 || rep.Removed != 0 {
		t.Fatalf("dry-run report = %+v, want 1/0/0", rep)
	}
	// Nothing may reach the source topic; the only publish is the dry-run
	// re-enqueue back onto the dead-letter queue.
	ch.mu.Lock()
	defer ch.mu.Unlock()
	for _, key := range ch.publishKeys {
		if key == event.TopicCharacters {
			t.Fatalf("dry run published to the source topic")
		}
	}
	if len(ch.acked) != 1 || len(ch.published) != 1 {
		t.Fatalf("dry run acked %d / re-enqueued %d, want 1/1", len(ch.acked), len(ch.published))
	}
}

func TestReplayDecodeFailureStopsWithoutAck(t *testing.T) {
	a, ch := newReplayAdapter(t)
	good, _ := dlDelivery(1, &event.Event{Type: event.EventCharacterUpdated, AccountID: 7, CharacterID: 6300})
	ch.deliveries <- good
	ch.deliveries <- amqp.Delivery{MessageId: "m-2", Body: []byte("}{"), DeliveryTag: 2}

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 2 || rep.Republished != 1 || rep.Removed != 1 || !rep.Failed() || len(rep.Errors) != 1 {
		t.Fatalf("report = %+v, want 2/1/1 one error", rep)
	}
	if rep.Errors[0].ID != "2" {
		t.Fatalf("error ID = %q, want the corrupt delivery tag", rep.Errors[0].ID)
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	// Only the good copy is acked; the corrupt one is left for inspection.
	if len(ch.acked) != 1 || ch.acked[0] != 1 {
		t.Fatalf("acked = %v, want only tag 1", ch.acked)
	}
}

func TestReplayLimitAndEmptyChannel(t *testing.T) {
	a, ch := newReplayAdapter(t)
	d1, _ := dlDelivery(1, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 6401})
	d2, _ := dlDelivery(2, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 6402})
	ch.deliveries <- d1
	ch.deliveries <- d2

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{Limit: 1})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 1 || rep.Removed != 1 {
		t.Fatalf("limited report = %+v, want 1/1/1", rep)
	}

	// A channel with nothing left replays to all zeros without error.
	empty, _ := newReplayAdapter(t)
	rep, err = empty.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 0 || rep.Republished != 0 || rep.Removed != 0 || rep.Failed() {
		t.Fatalf("empty-channel report = %+v, want all zero", rep)
	}
}

func TestReplayAckFailureCountsAsError(t *testing.T) {
	a, ch := newReplayAdapter(t)
	d, _ := dlDelivery(1, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 6500})
	ch.deliveries <- d
	ch.errAck = errReplayAck

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// The re-publish landed; only the ack failed, so the copy is not
	// counted as removed and a re-run would re-publish it (§6 idempotent).
	if rep.Republished != 1 || rep.Removed != 0 || !rep.Failed() {
		t.Fatalf("report = %+v, want published 1 / removed 0 with error", rep)
	}
}

var errReplayAck = errors.New("amqp ack failed")
