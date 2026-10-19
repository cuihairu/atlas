package event

import "sync"

// DeliveryMetrics counts what delivery limiting does (docs/sync.md §4). It is
// an interface on the event side so the adapter packages do not import
// internal/metrics; *metrics.Metrics satisfies it.
type DeliveryMetrics interface {
	// ObserveRedelivery counts one failed handler invocation — i.e. one
	// retry the adapter is about to schedule.
	ObserveRedelivery(adapter string)
	// ObserveDeadLetter counts one event moved to its dead-letter
	// destination, which ends the retries for it.
	ObserveDeadLetter(adapter, topic string)
}

type nopDeliveryMetrics struct{}

func (nopDeliveryMetrics) ObserveRedelivery(string) {}
func (nopDeliveryMetrics) ObserveDeadLetter(string, string) {}

// MetricsOrNop replaces a nil DeliveryMetrics with a no-op so adapter
// constructors can store it unconditionally.
func MetricsOrNop(m DeliveryMetrics) DeliveryMetrics {
	if m == nil {
		return nopDeliveryMetrics{}
	}
	return m
}

// AttemptTracker counts failed handler invocations per event ID. It exists
// because three of the four adapters run against brokers with no delivery
// counter (kafka never commits the offset, nats redelivers unacked messages
// forever, rabbitmq Nack(requeue) has no attempt bookkeeping); the broker's
// own PEL retry count (redis) stays the counter there.
type AttemptTracker struct {
	mu     sync.Mutex
	counts map[string]int
}

func NewAttemptTracker() *AttemptTracker {
	return &AttemptTracker{counts: map[string]int{}}
}

// Failed records one failed invocation and returns the count for the ID.
func (t *AttemptTracker) Failed(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counts[id]++
	return t.counts[id]
}

// Reset drops the count for an ID that was delivered (successfully applied
// or dead-lettered). Keeping both resets means the map holds only events
// that are failing right now, so a long-running process does not grow with
// its traffic volume.
func (t *AttemptTracker) Reset(id string) {
	t.mu.Lock()
	delete(t.counts, id)
	t.mu.Unlock()
}
