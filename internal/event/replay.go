package event

import "context"

// ReplayOptions tunes a dead-letter replay pass (docs/sync.md §4). Zero
// values mean: no limit, real run.
type ReplayOptions struct {
	// Limit caps how many dead letters one pass processes (0 = all).
	Limit int
	// DryRun counts and decodes the copies without re-publishing or
	// removing anything — the "what would replay do" preview.
	DryRun bool
}

// ReplayError is one dead letter a pass could not replay. ID is the
// store-native identifier of the copy (stream entry ID, offset, delivery
// tag) so an operator can inspect exactly what was left behind.
type ReplayError struct {
	ID  string
	Err error
}

func (e *ReplayError) Error() string { return e.ID + ": " + e.Err.Error() }

// Report is what one replay pass did. Counted like the sink counters in
// security.md §3: Republished and Removed only advance after the store
// confirmed the operation — never optimistically.
//
// Removed counts copies the store no longer holds: an XDEL'd Redis entry,
// a committed Kafka .dl offset (the record itself ages out with topic
// retention — Kafka cannot delete records), a deleted JetStream message,
// an AMQP delivery ack.
type Report struct {
	Topic       string
	Found       int
	Republished int
	Removed     int
	Errors      []*ReplayError
}

// Failed reports whether any copy could not be replayed.
func (r Report) Failed() bool { return len(r.Errors) > 0 }

// Replayer re-publishes a topic's dead-letter channel (docs/sync.md §4)
// onto the source topic: each copy is read oldest-first, re-published as
// a fresh canonical event, and only then removed from the dead-letter
// channel. Consumers stay idempotent (Directory.ApplyEvent is an upsert,
// §6), so a crash between publish and removal at worst re-applies.
type Replayer interface {
	ReplayDeadLetters(ctx context.Context, topic string, opts ReplayOptions) (Report, error)
}
