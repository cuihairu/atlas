// Error-path coverage for the Kafka adapter: the default-broker Open and
// the dead-letter reader factory it wires, the generic-fetch-error-during-
// shutdown exit, the dead-letter write failure, and every replay error
// branch (canceled context, dead-letter read failure, re-publish failure,
// commit failure) are driven with fake halves, not scheduling bets.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/cuihairu/atlas/internal/event"
)

var errFault = errors.New("fault injected")

// shutdownFaultReader blocks in FetchMessage until the context is canceled
// and then reports a GENERIC error: the connection already broke while the
// loop was shutting down, which must exit instead of logging and sleeping
// one more block interval. entered signals the loop is parked inside the
// fetch, so the test can cancel after — not before — it got there.
type shutdownFaultReader struct{ entered chan struct{} }

func (r *shutdownFaultReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return kafka.Message{}, errFault
}

func (shutdownFaultReader) CommitMessages(context.Context, ...kafka.Message) error {
	return nil
}

func (shutdownFaultReader) Close() error { return nil }

// TestOpenDefaultsBuildDeadLetterReader pins the zero-Options contract:
// empty brokers fall back to the documented default, and the adapter Open
// wires can build a dead-letter group reader (and release it) without a
// live broker — kafka-go dials lazily.
func TestOpenDefaultsBuildDeadLetterReader(t *testing.T) {
	a, err := Open(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Open with default brokers: %v", err)
	}
	r := a.newDLReader(event.DeadLetterTopic(event.TopicCharacters))
	if r == nil {
		t.Fatal("dead-letter reader = nil")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close dead-letter reader: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close adapter: %v", err)
	}
}

// TestConsumeExitsOnFetchErrorDuringShutdown pins the shutdown exit: a
// fetch that fails with a non-context error while the context is already
// canceled must return quietly — logging it as a live failure (and sleeping
// a block first) would stall every Close behind a dead connection.
func TestConsumeExitsOnFetchErrorDuringShutdown(t *testing.T) {
	var buf syncBuffer
	sr := &shutdownFaultReader{entered: make(chan struct{}, 1)}
	a := New(&fakeWriter{}, sr, Options{Block: 50 * time.Millisecond, Logger: capLogger(&buf)})
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, _ *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	<-sr.entered // the loop is parked in the fetch with a live context
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if strings.Contains(buf.String(), "event: fetch failed") {
		t.Fatalf("generic fetch error during shutdown must not be logged: %q", buf.String())
	}
}

// TestDeadLetterWriteFailureKeepsOffset pins the copy contract: a dead-
// letter mirror write that fails leaves the original offset uncommitted,
// so the broker redelivers and the next attempt retries the copy — the
// event is never dropped to make a dead-letter write succeed.
func TestDeadLetterWriteFailureKeepsOffset(t *testing.T) {
	var buf syncBuffer
	w := &fakeWriter{fail: errFault}
	r := &fakeReader{}
	a := New(w, r, Options{Block: 10 * time.Millisecond, MaxDelivery: 1, Logger: capLogger(&buf)})

	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterUpdated, CharacterID: 9})
	r.push(kafka.Message{Topic: event.TopicCharacters, Partition: 0, Offset: 3, Value: payload})

	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, _ *event.Event) error {
		return errors.New("apply failed")
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if !strings.Contains(buf.String(), "event: dead-letter write failed") {
		t.Fatalf("dead-letter write failure was not logged: %q", buf.String())
	}
	if n := r.commitCount(); n != 0 {
		t.Fatalf("commits = %d, want 0 (offset stays uncommitted for redelivery)", n)
	}
}

// TestReplayFetchErrorSurfaces pins the dead-letter read wrap: a failing
// fetch is surfaced as an error, never a silent empty report.
func TestReplayFetchErrorSurfaces(t *testing.T) {
	a := New(&fakeWriter{}, &fakeReader{}, Options{Block: 10 * time.Millisecond})
	a.newDLReader = func(string) Reader { return &fakeReader{fetchErr: errFault} }

	if _, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{}); err == nil {
		t.Fatal("replay with a failing dead-letter read: expected error, got nil")
	}
}

// TestReplayCanceledContextSurfaces pins the cancellation wrap: a canceled
// caller context is returned as-is (ctx.Canceled), not masked by the
// injected fetch error or a deadline.
func TestReplayCanceledContextSurfaces(t *testing.T) {
	a := New(&fakeWriter{}, &fakeReader{}, Options{Block: 10 * time.Millisecond})
	a.newDLReader = func(string) Reader { return &fakeReader{fetchErr: errFault} }
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.ReplayDeadLetters(cctx, event.TopicCharacters, event.ReplayOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("replay on a canceled context = %v, want context.Canceled", err)
	}
}

// TestReplayPublishFailureKeepsRecord pins the re-publish failure branch:
// a copy whose re-publish fails is reported as an error and left
// uncommitted, so a re-run picks it up again.
func TestReplayPublishFailureKeepsRecord(t *testing.T) {
	dl := &fakeReader{}
	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterUpdated, CharacterID: 9})
	dl.push(kafka.Message{Topic: event.DeadLetterTopic(event.TopicCharacters), Partition: 0, Offset: 0, Value: payload})

	a := New(&fakeWriter{fail: errFault}, &fakeReader{}, Options{Block: 10 * time.Millisecond})
	a.newDLReader = func(string) Reader { return dl }

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay must drain to a deadline, got %v", err)
	}
	if rep.Found != 1 || rep.Republished != 0 || rep.Removed != 0 || !rep.Failed() {
		t.Fatalf("report = %+v, want 1/0/0 with one error", rep)
	}
	if len(dl.commits) != 0 {
		t.Fatalf("dead-letter commits = %d, want 0 (record kept for a safe re-run)", len(dl.commits))
	}
}

// TestReplayCommitFailureKeepsRecord pins the commit wrap: a record that
// was re-published but whose dead-letter commit failed counts as an error,
// not a removal — the copy survives and a re-run republishes it
// (idempotent handlers make that safe).
func TestReplayCommitFailureKeepsRecord(t *testing.T) {
	dl := &fakeReader{failCommit: errFault}
	payload, _ := json.Marshal(&event.Event{Type: event.EventCharacterUpdated, CharacterID: 9})
	dl.push(kafka.Message{Topic: event.DeadLetterTopic(event.TopicCharacters), Partition: 0, Offset: 0, Value: payload})

	a := New(&fakeWriter{}, &fakeReader{}, Options{Block: 10 * time.Millisecond})
	a.newDLReader = func(string) Reader { return dl }

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay must drain to a deadline, got %v", err)
	}
	if rep.Found != 1 || rep.Republished != 1 || rep.Removed != 0 || !rep.Failed() {
		t.Fatalf("report = %+v, want 1/1/0 with one error", rep)
	}
}
