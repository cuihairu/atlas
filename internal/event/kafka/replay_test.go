package kafka

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/cuihairu/atlas/internal/event"
)

// newReplayAdapter wires an adapter whose dead-letter reader factory hands
// out dlReader and records the topic it was scoped to.
func newReplayAdapter(t *testing.T, dl *fakeReader) (*Adapter, *fakeWriter, *string) {
	t.Helper()
	w := &fakeWriter{}
	a := New(w, &fakeReader{}, Options{Block: 20 * time.Millisecond})
	var scoped string
	a.newDLReader = func(topic string) Reader {
		scoped = topic
		return dl
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, w, &scoped
}

func dlMessage(e *event.Event) (kafka.Message, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return kafka.Message{}, err
	}
	return kafka.Message{Topic: event.DeadLetterTopic(event.TopicCharacters), Partition: 0, Offset: 0, Value: data}, nil
}

func TestReplayRepublishesAndCommits(t *testing.T) {
	dl := &fakeReader{}
	m1, _ := dlMessage(&event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 4001, Name: "A"})
	m2, _ := dlMessage(&event.Event{Type: event.EventCharacterDeleted, AccountID: 7, CharacterID: 4002})
	m1.Offset, m2.Offset = 3, 4
	dl.push(m1)
	dl.push(m2)
	a, w, scoped := newReplayAdapter(t, dl)

	// The drain wait is one block interval; the empty fake reader returns
	// ctx.Err() after that and the pass stops.
	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Topic != event.TopicCharacters || rep.Found != 2 || rep.Republished != 2 || rep.Removed != 2 || rep.Failed() {
		t.Fatalf("report = %+v, want 2/2/2 clean", rep)
	}
	if *scoped != event.DeadLetterTopic(event.TopicCharacters) {
		t.Fatalf("dl reader scoped to %q", *scoped)
	}
	if w.count() != 2 {
		t.Fatalf("writer got %d messages, want 2", w.count())
	}
	for _, msg := range w.written {
		if msg.Topic != event.TopicCharacters {
			t.Fatalf("replayed to topic %q, want %q", msg.Topic, event.TopicCharacters)
		}
		var decoded event.Event
		if err := json.Unmarshal(msg.Value, &decoded); err != nil {
			t.Fatalf("replayed value not event JSON: %v", err)
		}
	}
	// Kafka cannot delete records; "removed" is the committed offset, so a
	// second run of the same group skips what was replayed.
	if dl.commitCount() != 2 {
		t.Fatalf("dl reader committed %d messages, want 2", dl.commitCount())
	}
}

func TestReplayDryRunCountsOnly(t *testing.T) {
	dl := &fakeReader{}
	m1, _ := dlMessage(&event.Event{Type: event.EventCharacterLogin, AccountID: 7, CharacterID: 4100})
	dl.push(m1)
	a, w, _ := newReplayAdapter(t, dl)

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{DryRun: true})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 0 || rep.Removed != 0 {
		t.Fatalf("dry-run report = %+v, want 1/0/0", rep)
	}
	if w.count() != 0 || dl.commitCount() != 0 {
		t.Fatalf("dry run wrote (%d) or committed (%d)", w.count(), dl.commitCount())
	}
}

func TestReplayDecodeFailureSkipsWithoutCommit(t *testing.T) {
	dl := &fakeReader{}
	good, _ := dlMessage(&event.Event{Type: event.EventCharacterUpdated, AccountID: 7, CharacterID: 4200})
	good.Offset = 1
	dl.push(good)
	dl.push(kafka.Message{Topic: event.DeadLetterTopic(event.TopicCharacters), Partition: 0, Offset: 2, Value: []byte("}{")})
	a, w, _ := newReplayAdapter(t, dl)

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 2 || rep.Republished != 1 || rep.Removed != 1 || !rep.Failed() || len(rep.Errors) != 1 {
		t.Fatalf("report = %+v, want 2/1/1 one error", rep)
	}
	if rep.Errors[0].ID != "0@2" {
		t.Fatalf("error ID = %q, want the un-replayed offset", rep.Errors[0].ID)
	}
	if w.count() != 1 || dl.commitCount() != 1 {
		t.Fatalf("wrote %d / committed %d, want only the good copy", w.count(), dl.commitCount())
	}
}

func TestReplayLimit(t *testing.T) {
	dl := &fakeReader{}
	for i := int64(0); i < 3; i++ {
		m, _ := dlMessage(&event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 4300 + i})
		m.Offset = i
		dl.push(m)
	}
	a, w, _ := newReplayAdapter(t, dl)

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{Limit: 2})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 2 || rep.Republished != 2 || rep.Removed != 2 {
		t.Fatalf("limited report = %+v, want 2/2/2", rep)
	}
	if w.count() != 2 {
		t.Fatalf("wrote %d, want 2", w.count())
	}
}

func TestReplayWithoutReaderFactoryFailsFast(t *testing.T) {
	w := &fakeWriter{}
	a := New(w, &fakeReader{}, Options{})
	defer a.Close()
	if _, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{}); err == nil {
		t.Fatal("replay on a New-built adapter (no dl reader factory) must fail, not silently no-op")
	}
}
