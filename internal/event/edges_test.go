// Direct unit pins for the small contracts the adapter packages' tests
// never force: the replay error text, the report's Failed verdict, the
// nil-metrics no-op swap and non-nil passthrough, TopicFor's rejection of
// unknown types, and the counting wrapper's unknown-topic fallback and
// sorted Stats snapshot.
package event

import (
	"context"
	"errors"
	"testing"
)

func TestReplayErrorText(t *testing.T) {
	e := &ReplayError{ID: "0@7", Err: errors.New("commit failed")}
	if got := e.Error(); got != "0@7: commit failed" {
		t.Fatalf("Error() = %q, want id-prefixed text", got)
	}
}

func TestReportFailed(t *testing.T) {
	if (Report{}).Failed() {
		t.Fatal("empty report must not count as failed")
	}
	if !(Report{Errors: []*ReplayError{{}}}).Failed() {
		t.Fatal("report with errors must count as failed")
	}
}

func TestMetricsOrNopNil(t *testing.T) {
	m := MetricsOrNop(nil)
	if m == nil {
		t.Fatal("MetricsOrNop(nil) returned nil, want the no-op")
	}
	// The no-op must accept every observation without panic.
	m.ObserveRedelivery("redis")
	m.ObserveDeadLetter("redis", "atlas.characters.dl")
}

func TestTopicForUnknownType(t *testing.T) {
	if _, err := TopicFor(EventType("totally_bogus")); err == nil {
		t.Fatal("TopicFor with an unknown type: expected error, got nil")
	}
}

// countingBus is the smallest EventAdapter: publish succeeds and
// subscribe hands one synthetic event straight to the handler.
type countingBus struct{}

func (countingBus) Name() string                          { return "counting-stub" }
func (countingBus) Synchronous() bool                     { return true }
func (countingBus) Publish(context.Context, *Event) error { return nil }
func (countingBus) Ack(context.Context, *Event) error     { return nil }
func (countingBus) Close() error                          { return nil }
func (b countingBus) Subscribe(_ context.Context, _ string, h Handler) error {
	return h(context.Background(), &Event{Type: EventCharacterCreated})
}

// recordingMetrics is a DeliveryMetrics the passthrough test can observe.
type recordingMetrics struct{ redeliveries int }

func (r *recordingMetrics) ObserveRedelivery(string)         { r.redeliveries++ }
func (r *recordingMetrics) ObserveDeadLetter(string, string) {}

// TestMetricsOrNopPassthrough pins the non-nil arm: the caller's metrics
// are stored as-is, not wrapped or swapped.
func TestMetricsOrNopPassthrough(t *testing.T) {
	r := &recordingMetrics{}
	got := MetricsOrNop(r)
	got.ObserveRedelivery("kafka")
	if r.redeliveries != 1 {
		t.Fatalf("passthrough did not reach the caller's metrics: %d", r.redeliveries)
	}
}

// TestCountingWrapperUnknownTopicAndSortedStats pins the counting wrapper:
// an unroutable event type falls back to the atlas.unknown bucket instead
// of being dropped or panicking, a handler invoked through Subscribe
// counts as consumed only on success, and Stats snapshots every counter
// sorted by topic.
func TestCountingWrapperUnknownTopicAndSortedStats(t *testing.T) {
	c := WrapCounting(countingBus{})

	if got := topicOf(&Event{Type: EventType("totally_bogus")}); got != "atlas.unknown" {
		t.Fatalf("topicOf(unknown) = %q, want atlas.unknown", got)
	}
	if err := c.Publish(context.Background(), &Event{Type: EventCharacterCreated}); err != nil {
		t.Fatalf("publish known type: %v", err)
	}
	if err := c.Publish(context.Background(), &Event{Type: EventType("totally_bogus")}); err != nil {
		t.Fatalf("publish unknown type: %v", err)
	}
	if err := c.Subscribe(context.Background(), TopicCharacters, func(_ context.Context, _ *Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	stats := c.Stats()
	if len(stats) != 2 {
		t.Fatalf("stats topics = %v, want the known and unknown buckets", stats)
	}
	if stats[0].Topic != TopicCharacters || stats[0].Published != 1 || stats[0].Consumed != 1 {
		t.Fatalf("characters bucket = %+v, want published/consumed 1/1", stats[0])
	}
	if stats[1].Topic != "atlas.unknown" || stats[1].Published != 1 {
		t.Fatalf("unknown bucket = %+v, want published 1", stats[1])
	}
}
