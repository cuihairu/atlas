package nats

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/cuihairu/atlas/internal/event"
)

// startServer boots an in-process NATS server with JetStream enabled —
// the same mechanism the NATS project ships for its own tests.
func startServer(t *testing.T) *natsserver.Server {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready")
	}
	return srv
}

func openAdapter(t *testing.T) *Adapter {
	t.Helper()
	a, err := Open(Options{URL: startServer(t).ClientURL()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestPublishStampsSequenceAndDelivers(t *testing.T) {
	a := openAdapter(t)

	got := make(chan *event.Event, 1)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	in := &event.Event{Type: event.EventCharacterCreated, AccountID: 42, CharacterID: 1001, Name: "Hero"}
	if err := a.Publish(context.Background(), in); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if in.ID == "" {
		t.Fatal("publish should stamp the stream sequence as event ID")
	}

	select {
	case e := <-got:
		if e.CharacterID != 1001 || e.Name != "Hero" {
			t.Fatalf("delivered event lost fields: %+v", e)
		}
		if e.ID != in.ID {
			t.Fatalf("delivered ID = %q, want %q", e.ID, in.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestHandlerFailureLeavesMessageUnacked(t *testing.T) {
	a := openAdapter(t)

	var calls atomic.Int32
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		calls.Add(1)
		return context.DeadlineExceeded // simulate apply failure
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated, CharacterID: 9}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Wait for the handler first: it is the cause of the unacked state, so
	// asserting on pending counters before it runs is racy — NumPending is
	// already 1 once the message lands in the stream, while the pull fetch
	// may not have delivered it to the handler yet.
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("handler should have been called once")
	}

	// The message must stay pending (unacked) after the handler fails —
	// fetched-but-unacked shows up in NumAckPending.
	deadline = time.Now().Add(3 * time.Second)
	var pending int64
	for time.Now().Before(deadline) {
		info, err := a.js.ConsumerInfo(a.stream, a.durable)
		if err == nil && info.NumAckPending+int(info.NumPending) > 0 {
			pending = int64(info.NumAckPending) + int64(info.NumPending)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pending == 0 {
		t.Fatal("expected unacked pending message after handler failure")
	}
}

func TestPoisonPayloadIsDroppedAndAcked(t *testing.T) {
	a := openAdapter(t)

	var called atomic.Int32
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		called.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Probe with a well-formed event first: it proves the consume loop is
	// live, so the "poison never reached the handler" assertion below
	// cannot pass vacuously by racing ahead of the consumer.
	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated, CharacterID: 1}); err != nil {
		t.Fatalf("probe publish: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for called.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if called.Load() != 1 {
		t.Fatalf("probe handler calls = %d, want 1", called.Load())
	}

	// Raw-publish an undecodable payload on the same subject.
	if _, err := a.js.Publish(event.TopicCharacters, []byte("not-json")); err != nil {
		t.Fatalf("raw publish: %v", err)
	}

	// The poison message must be acked (drained to zero pending) without
	// ever reaching the handler.
	deadline = time.Now().Add(3 * time.Second)
	var pending uint64
	for time.Now().Before(deadline) {
		info, err := a.js.ConsumerInfo(a.stream, a.durable)
		if err == nil {
			pending = uint64(info.NumAckPending) + info.NumPending
			if pending == 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := called.Load(); n != 1 {
		t.Fatalf("handler called %d times, want 1 (poison payload must not be delivered)", n)
	}
	if pending != 0 {
		t.Fatalf("poison payload still pending: %d", pending)
	}
}

func TestPayloadRoundTripPreservesOptionalFields(t *testing.T) {
	a := openAdapter(t)

	got := make(chan *event.Event, 1)
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	lvl := 10
	if err := a.Publish(context.Background(), &event.Event{
		Type: event.EventCharacterMoved, CharacterID: 5, ServerID: "s1",
		TargetServerID: "s2", Level: &lvl,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case e := <-got:
		if e.TargetServerID != "s2" || e.Level == nil || *e.Level != 10 {
			t.Fatalf("optional fields lost: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestOpenConnectFailureAndAccessors(t *testing.T) {
	if _, err := Open(Options{URL: "nats://127.0.0.1:1"}); err == nil ||
		!strings.Contains(err.Error(), "nats connect") {
		t.Errorf("Open to closed port = %v, want connect failure", err)
	}

	a := New(nil, nil, Options{})
	if a.Name() != "nats" {
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

func TestPublishAfterConnClosed(t *testing.T) {
	// A JetStream publish over a dead connection must fail with a wrapped
	// nats error, not silently drop the event.
	a := openAdapter(t)
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err := a.Publish(context.Background(), &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, CharacterID: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "event: nats publish") {
		t.Errorf("publish on closed conn = %v, want nats publish error", err)
	}
}

func TestSubscribeUnknownStreamFails(t *testing.T) {
	// Binding a durable consumer to a stream that does not exist must fail
	// Subscribe up front — a misconfigured stream name cannot start a loop
	// that will never receive anything.
	srv := startServer(t)
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	js, err := conn.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}

	a := New(conn, js, Options{Stream: "NOPE", Durable: "atlas-test", Fetch: 10 * time.Millisecond})
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err == nil || !strings.Contains(err.Error(), "event: nats pull subscribe") {
		t.Errorf("subscribe to unknown stream = %v, want pull subscribe error", err)
	}
}

func TestFetchErrorLoggedAndLoopEnds(t *testing.T) {
	// A fetch failure that is neither a timeout nor a shutdown (here: the
	// connection dying under the subscription) is logged; the loop exits on
	// the next context check instead of spinning.
	var buf syncBuffer
	a := openAdapter(t)
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))
	a.fetch = 10 * time.Millisecond
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	a.conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(buf.String(), "event: fetch failed") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "event: fetch failed") {
		t.Fatalf("fetch failure never logged, buffer = %q", buf.String())
	}
	if err := a.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

// TestConsumerDefaultsUnboundedRedelivery pins what a perpetually-failing
// handler means today: PullSubscribe binds the consumer with JetStream server
// defaults, MaxDeliver <= 0 = no delivery limit (measured: this server reports -1), and there is no dead-letter
// target. A poison event (handler always errors) is re-delivered forever and,
// because a pull consumer delivers one subject strictly in order, it wedges
// every later event on that subject.
//
// This is the measured evidence behind the delivery-limit / backoff / DLQ step
// recorded in docs/sync.md; the assertion must be updated (not deleted) when a
// bounded MaxDeliver becomes configurable.
func TestConsumerDefaultsUnboundedRedelivery(t *testing.T) {
	a := openAdapter(t)

	var calls atomic.Int32
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, _ *event.Event) error {
		calls.Add(1)
		return context.DeadlineExceeded // simulate a handler that never succeeds
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated, CharacterID: 9}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("handler should have been called once")
	}

	info, err := a.js.ConsumerInfo(a.stream, a.durable)
	if err != nil {
		t.Fatalf("consumer info: %v", err)
	}
	// 0 and -1 both mean "no delivery limit" across nats-server versions.
	if info.Config.MaxDeliver > 0 {
		t.Errorf("MaxDeliver = %d, want unbounded redelivery (<= 0)", info.Config.MaxDeliver)
	}
	if info.Config.AckWait <= 0 {
		t.Errorf("AckWait = %v, want the server default", info.Config.AckWait)
	}
}

// TestDeliveryCapDeadLettersAfterThreshold pins the opt-in cap
// (ATLAS_EVENT_MAX_DELIVERY) on a real JetStream server: after the failed
// delivery the message is republished to "<subject>.dl" and acked, ending
// the chain the server default (MaxDeliver -1) would otherwise keep forever
// (pinned by TestConsumerDefaultsUnboundedRedelivery). The cap is set at
// Open so the stream is created covering the dead-letter subject — a
// publish to an uncovered subject is rejected by JetStream outright.
func TestDeliveryCapDeadLettersAfterThreshold(t *testing.T) {
	a, err := Open(Options{URL: startServer(t).ClientURL(), MaxDelivery: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	var calls atomic.Int32
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		calls.Add(1)
		return context.DeadlineExceeded
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated, CharacterID: 9}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	dlSubject := event.DeadLetterTopic(event.TopicCharacters)
	info, err := a.js.StreamInfo(a.stream)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	covered := false
	for _, s := range info.Config.Subjects {
		if s == dlSubject {
			covered = true
		}
	}
	if !covered {
		t.Fatalf("stream subjects %v miss the dead-letter subject %s", info.Config.Subjects, dlSubject)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ci, err := a.js.ConsumerInfo(a.stream, a.durable); err == nil &&
			ci.NumAckPending == 0 && ci.NumPending == 0 && calls.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("handler calls = %d, want exactly 1 (the cap ends the chain; a second delivery needs AckWait)", n)
	}
	if ci, err := a.js.ConsumerInfo(a.stream, a.durable); err != nil || ci.NumAckPending != 0 || ci.NumPending != 0 {
		t.Fatalf("consumer still holds the message after the cap: %+v, %v", ci, err)
	}
	raw, err := a.js.GetLastMsg(a.stream, dlSubject)
	if err != nil {
		t.Fatalf("dead letter not stored on %s: %v", dlSubject, err)
	}
	if !strings.Contains(string(raw.Data), `"character_id":9`) {
		t.Fatalf("dead-letter payload = %s, want the original event", raw.Data)
	}
}

func TestOpenDefaultsToLocalhostWhenURLOmitted(t *testing.T) {
	// An empty URL must fall back to the default NATS address, not the
	// empty string; with nothing listening there the connect error is the
	// observable proof the fallback ran.
	if _, err := Open(Options{}); err == nil ||
		!strings.Contains(err.Error(), "nats connect") {
		t.Errorf("Open without URL = %v, want connect failure via default URL", err)
	}
}

func TestOpenReconcilesDeadLetterSubjectOntoExistingStream(t *testing.T) {
	srv := startServer(t)

	// First open with no delivery cap: the stream is created covering only
	// the live subject.
	first, err := Open(Options{URL: srv.ClientURL()})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	dl := event.DeadLetterTopic(event.TopicCharacters)
	info, err := first.js.StreamInfo(first.stream)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	if containsString(info.Config.Subjects, dl) {
		t.Fatalf("fresh stream already covers %s: %v", dl, info.Config.Subjects)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	// A rolling restart onto the delivery cap must extend the existing
	// stream's subject list — JetStream rejects publishes to uncovered
	// subjects, so dead-lettering would silently fail without the reconcile.
	second, err := Open(Options{URL: srv.ClientURL(), MaxDelivery: 2})
	if err != nil {
		t.Fatalf("open onto existing stream: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if info, err := second.js.StreamInfo(second.stream); err != nil {
		t.Fatalf("stream info after reconcile: %v", err)
	} else if !containsString(info.Config.Subjects, dl) {
		t.Fatalf("subjects after reconcile = %v, want %s covered", info.Config.Subjects, dl)
	}

	// A third open onto an already-covered stream must be a no-op success.
	third, err := Open(Options{URL: srv.ClientURL(), MaxDelivery: 2})
	if err != nil {
		t.Fatalf("open onto covered stream: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("third close: %v", err)
	}
}

func TestDeadLetterCopyFailureKeepsMessageUnacked(t *testing.T) {
	srv := startServer(t)

	// Create the stream without the dead-letter subject, then attach a
	// capped adapter through New — bypassing Open's subject reconcile — so
	// the dead-letter copy must fail against the real server.
	seed, err := Open(Options{URL: srv.ClientURL()})
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	conn, err := nats.Connect(srv.ClientURL(), nats.Name("atlas-test"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	a := New(conn, js, Options{MaxDelivery: 1, Fetch: 20 * time.Millisecond})

	var calls atomic.Int32
	if err := a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, _ *event.Event) error {
		calls.Add(1)
		return context.DeadlineExceeded
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated, CharacterID: 7}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("handler never called")
	}
	// The copy fails, so nothing may sit on the dead-letter subject and the
	// original must stay unacked for redelivery — the message is never
	// dropped to make a dead-letter write succeed (adapter.go deadLetter).
	dlSubject := event.DeadLetterTopic(event.TopicCharacters)
	if _, err := a.js.GetLastMsg(a.stream, dlSubject); err == nil {
		t.Fatalf("dead-letter copy stored on uncovered subject %s", dlSubject)
	}
	ci, err := a.js.ConsumerInfo(a.stream, a.durable)
	if err != nil {
		t.Fatalf("consumer info: %v", err)
	}
	if ci.NumAckPending != 1 {
		t.Fatalf("ack pending = %d, want 1 (failed copy must leave the original)", ci.NumAckPending)
	}
}

func TestPublishRejectsUnknownEventType(t *testing.T) {
	a := openAdapter(t)
	err := a.Publish(context.Background(), &event.Event{Type: event.EventType("bogus")})
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("publish with unknown type = %v, want topic-mapping error", err)
	}
}

func TestOpenStreamInfoErrorOnNonJetStreamServer(t *testing.T) {
	// A plain NATS server: connect works, JetStream context builds lazily,
	// and the first StreamInfo fails with something other than
	// "stream not found" — that error must surface, not be mistaken for
	// "create the stream".
	srv, err := natsserver.NewServer(&natsserver.Options{Port: -1, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready")
	}
	if _, err := Open(Options{URL: srv.ClientURL()}); err == nil ||
		!strings.Contains(err.Error(), "nats stream info") {
		t.Errorf("Open against non-JetStream server = %v, want stream info error", err)
	}
}

func TestOpenAddStreamFailsOnSubjectOverlap(t *testing.T) {
	srv := startServer(t)
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	// Another stream already owns the subject: JetStream rejects a second
	// stream over the same subject, which must surface as an add-stream
	// error, not a silent half-open adapter.
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:      "squatter",
		Subjects:  []string{event.TopicCharacters},
		Retention: nats.LimitsPolicy,
	}); err != nil {
		t.Fatalf("seed squatter stream: %v", err)
	}
	if _, err := Open(Options{URL: srv.ClientURL()}); err == nil ||
		!strings.Contains(err.Error(), "nats add stream") {
		t.Errorf("Open with subject overlap = %v, want add stream error", err)
	}
}

func TestOpenUpdateStreamFailsWhenDeadLetterSubjectOwnedElsewhere(t *testing.T) {
	srv := startServer(t)
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	dl := event.DeadLetterTopic(event.TopicCharacters)
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:      "dl-owner",
		Subjects:  []string{dl},
		Retention: nats.LimitsPolicy,
	}); err != nil {
		t.Fatalf("seed dl owner stream: %v", err)
	}
	// The live stream exists without the dead-letter subject; reconciling
	// it must fail because another stream already owns that subject.
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:      DefaultStream,
		Subjects:  []string{event.TopicCharacters},
		Retention: nats.LimitsPolicy,
	}); err != nil {
		t.Fatalf("seed live stream: %v", err)
	}
	if _, err := Open(Options{URL: srv.ClientURL(), MaxDelivery: 2}); err == nil ||
		!strings.Contains(err.Error(), "nats update stream for dead letter") {
		t.Errorf("Open with owned dl subject = %v, want update stream error", err)
	}
}

func TestDeadLetterAckFailureIsLoggedNotPanic(t *testing.T) {
	a := openReplayAdapter(t)
	// A copied message whose ack then fails must be logged and swallowed —
	// the copy already succeeded, so the delivery chain still ends.
	a.attempts.Reset("0")
	msg := &nats.Msg{Subject: event.TopicCharacters, Data: []byte(`{}`)}
	a.deadLetter(msg, 1, 1)
}
