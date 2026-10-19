package event

import "testing"

// TestAttemptTrackerCountsAndResets pins the in-process delivery counter the
// kafka/nats/rabbitmq adapters use to cap redeliveries: one Failed per failed
// handler invocation, Reset on success or dead letter, and the map drops
// entries so it holds only events failing right now.
func TestAttemptTrackerCountsAndResets(t *testing.T) {
	tr := NewAttemptTracker()
	if n := tr.Failed("a"); n != 1 {
		t.Fatalf("first Failed = %d, want 1", n)
	}
	if n := tr.Failed("a"); n != 2 {
		t.Fatalf("second Failed = %d, want 2", n)
	}
	if n := tr.Failed("b"); n != 1 {
		t.Fatalf("Failed on other ID = %d, want 1 (counts are per ID)", n)
	}
	tr.Reset("a")
	if n := tr.Failed("a"); n != 1 {
		t.Fatalf("Failed after Reset = %d, want 1 (not 3)", n)
	}
}

// TestDeadLetterTopicNaming pins the "<topic>.dl" convention the adapters and
// ops tooling both rely on.
func TestDeadLetterTopicNaming(t *testing.T) {
	if got := DeadLetterTopic(TopicCharacters); got != "atlas.characters.dl" {
		t.Fatalf("DeadLetterTopic = %q, want atlas.characters.dl", got)
	}
}

// TestMetricsOrNopKeepsNilSafe pins that a nil DeliveryMetrics becomes a
// usable no-op, so adapters can store it unconditionally.
func TestMetricsOrNopKeepsNilSafe(t *testing.T) {
	m := MetricsOrNop(nil)
	m.ObserveRedelivery("kafka") // must not panic
	m.ObserveDeadLetter("kafka", TopicCharacters)
}
