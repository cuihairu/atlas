package nats

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/cuihairu/atlas/internal/event"
)

// openReplayAdapter opens an adapter whose stream covers the dead-letter
// subject (MaxDelivery > 0 extends the subject list at open).
func openReplayAdapter(t *testing.T) *Adapter {
	t.Helper()
	a, err := Open(Options{
		URL:         startServer(t).ClientURL(),
		MaxDelivery: 3,
		Fetch:       20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// seedDeadLetter publishes a copy onto the dead-letter subject, mirroring
// Adapter.deadLetter.
func seedDeadLetter(t *testing.T, a *Adapter, e *event.Event) {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := a.js.Publish(event.DeadLetterTopic(event.TopicCharacters), data); err != nil {
		t.Fatalf("publish dl: %v", err)
	}
}

// dlSubjectState counts stream messages still on the dead-letter subject.
func dlSubjectState(t *testing.T, a *Adapter) uint64 {
	t.Helper()
	ci, err := a.js.AddConsumer(a.stream, &nats.ConsumerConfig{
		Durable:       "check-dl",
		FilterSubject: event.DeadLetterTopic(event.TopicCharacters),
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		ReplayPolicy:  nats.ReplayInstantPolicy,
		MaxDeliver:    1,
	})
	if err != nil {
		t.Fatalf("add consumer: %v", err)
	}
	sub, err := a.js.PullSubscribe(event.DeadLetterTopic(event.TopicCharacters), ci.Config.Durable, nats.BindStream(a.stream))
	if err != nil {
		t.Fatalf("pull subscribe: %v", err)
	}
	defer func() {
		_ = sub.Unsubscribe()
		_ = a.js.DeleteConsumer(a.stream, ci.Config.Durable)
	}()
	var n uint64
	for {
		msgs, err := sub.Fetch(10, nats.MaxWait(100*time.Millisecond))
		if err != nil || len(msgs) == 0 {
			return n
		}
		n += uint64(len(msgs))
		for _, m := range msgs {
			_ = m.Ack()
		}
	}
}

func TestReplayRepublishesAndDeletes(t *testing.T) {
	a := openReplayAdapter(t)
	ctx := context.Background()
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 5001, Name: "A"})
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterDeleted, AccountID: 7, CharacterID: 5002})

	rep, err := a.ReplayDeadLetters(ctx, event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Topic != event.TopicCharacters || rep.Found != 2 || rep.Republished != 2 || rep.Removed != 2 || rep.Failed() {
		t.Fatalf("report = %+v, want 2/2/2 clean", rep)
	}
	if n := dlSubjectState(t, a); n != 0 {
		t.Fatalf("%d copies still on the dl subject, want 0", n)
	}
	// The copies must be back on the source subject as fresh messages.
	info, err := a.js.StreamInfo(a.stream)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	// 2 seeded dl + 2 replayed = 4 ever stored; the two deletes leave 2.
	if info.State.Msgs != 2 {
		t.Fatalf("stream holds %d messages, want the 2 replayed", info.State.Msgs)
	}
	ci, err := a.js.AddConsumer(a.stream, &nats.ConsumerConfig{
		Durable:       "check-replayed",
		FilterSubject: event.TopicCharacters,
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		ReplayPolicy:  nats.ReplayInstantPolicy,
	})
	if err != nil {
		t.Fatalf("add consumer: %v", err)
	}
	defer a.js.DeleteConsumer(a.stream, ci.Config.Durable)
	sub, err := a.js.PullSubscribe(event.TopicCharacters, ci.Config.Durable, nats.BindStream(a.stream))
	if err != nil {
		t.Fatalf("pull subscribe: %v", err)
	}
	defer sub.Unsubscribe()
	msgs, err := sub.Fetch(2, nats.MaxWait(time.Second))
	if err != nil || len(msgs) != 2 {
		t.Fatalf("fetch source subject: %v (%d), want 2 replayed messages", err, len(msgs))
	}
	for _, m := range msgs {
		var decoded event.Event
		if err := json.Unmarshal(m.Data, &decoded); err != nil {
			t.Fatalf("replayed payload not event JSON: %v", err)
		}
	}
}

func TestReplayDryRunCountsOnly(t *testing.T) {
	a := openReplayAdapter(t)
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterLogin, AccountID: 7, CharacterID: 5100})

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{DryRun: true})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 0 || rep.Removed != 0 {
		t.Fatalf("dry-run report = %+v, want 1/0/0", rep)
	}
	if n := dlSubjectState(t, a); n != 1 {
		t.Fatalf("%d copies on the dl subject after dry run, want 1", n)
	}
}

func TestReplayLimitAndEmptyChannel(t *testing.T) {
	a := openReplayAdapter(t)
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 5201})
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 5202})

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{Limit: 1})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 1 || rep.Removed != 1 {
		t.Fatalf("limited report = %+v, want 1/1/1", rep)
	}
	if n := dlSubjectState(t, a); n != 1 {
		t.Fatalf("%d copies left, want 1", n)
	}

	// An empty channel replays to all zeros without error.
	rep, err = a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 1 || rep.Removed != 1 || rep.Failed() {
		t.Fatalf("second pass = %+v, want the remaining copy replayed", rep)
	}
}
