// Error-path coverage for the Redis Streams event adapter: the dead-letter
// sweep's copy failures (missing source, mirror-write failure), the poison
// payload drop, the ack failure, and the replay's read/publish/delete error
// branches are driven directly or through a failing-client wrapper so the
// coverage is deterministic, not a scheduling bet.
package redis

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/event"
)

var errFault = errors.New("fault injected")

// ── failing-client wrappers ────────────────────────────────────────────

// xaddFailClient fails XAdd only for the dead-letter stream, so the mirror
// write of copyToDeadLetter fails while the source read still succeeds.
type xaddFailClient struct {
	redis.UniversalClient
	failStream string
}

func (c *xaddFailClient) XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd {
	if args.Stream == c.failStream {
		return redis.NewStringResult("", errFault)
	}
	return c.UniversalClient.XAdd(ctx, args)
}

// xackFailClient makes every acknowledge fail, pinning handle()'s ack-error
// log without a flaky connection drop.
type xackFailClient struct {
	redis.UniversalClient
}

func (c *xackFailClient) XAck(ctx context.Context, stream, group string, ids ...string) *redis.IntCmd {
	return redis.NewIntResult(0, errFault)
}

// xdelFailClient makes every stream-entry delete fail, pinning replay's
// "republished but not removed" report branch.
type xdelFailClient struct {
	redis.UniversalClient
}

func (c *xdelFailClient) XDel(ctx context.Context, stream string, ids ...string) *redis.IntCmd {
	return redis.NewIntResult(0, errFault)
}

// newClient returns a raw client on a fresh miniredis so tests can build both
// a seeding adapter and a failing-wrapper adapter against the same broker.
func newClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return client, mr
}

func newWrapped(t *testing.T, mr *miniredis.Miniredis, wrap func(redis.UniversalClient) redis.UniversalClient, opts Options) *Adapter {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return New(wrap(client), opts)
}

// newTestSweeper returns a second adapter on the same broker so a test can
// drive the sweep synchronously with no competing consume loop (see
// TestDeadLetterAfterMaxRedelivery for the rationale).
func newTestSweeper(t *testing.T, mr *miniredis.Miniredis, buf *syncBuffer, opts Options) *Adapter {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	opts.Logger = slog.New(slog.NewTextHandler(buf, nil))
	return New(client, opts)
}

// seedPending publishes one event, lets a failing handler leave it pending,
// stamps the delivery count the way a broker-side redelivery chain would
// (XCLAIM RETRYCOUNT), then stops the consume loop so the test can drive the
// sweep synchronously.
func seedPending(t *testing.T, a *Adapter, in *event.Event, retryCount string) {
	t.Helper()
	ctx := context.Background()
	if err := a.Subscribe(ctx, event.TopicCharacters, func(context.Context, *event.Event) error {
		return errors.New("poison handler")
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := a.Publish(ctx, in); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitFor(t, "failed delivery pending", func() bool { return pendingCount(t, a) == 1 })
	if err := a.client.Do(ctx, "XCLAIM", event.TopicCharacters, a.group,
		"dead-claimed", "0", in.ID, "RETRYCOUNT", retryCount).Err(); err != nil {
		t.Fatalf("xclaim: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// pendingEntries counts the group's pending list directly (XPENDING
// EXTENDED). miniredis's summary counter does not track entries moved to an
// out-of-band consumer by seedPending's XCLAIM, so pendingCount
// under-reports here; the extended list is the ground truth.
func pendingEntries(t *testing.T, a *Adapter) int {
	t.Helper()
	pending, err := a.client.XPendingExt(context.Background(), &redis.XPendingExtArgs{
		Stream: event.TopicCharacters, Group: a.group, Start: "-", End: "+", Count: 16,
	}).Result()
	if err != nil {
		t.Fatalf("xpendingext: %v", err)
	}
	return len(pending)
}

// TestDeadLetterMirrorFailsWhenSourceMissing pins the mirror contract: an
// exhausted entry whose source stream no longer holds the event cannot be
// copied, so it stays pending (the next sweep retries rather than losing it).
func TestDeadLetterMirrorFailsWhenSourceMissing(t *testing.T) {
	var buf syncBuffer
	a, mr := newTestAdapter(t, Options{MaxDelivery: 1, Block: 30 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	ctx := context.Background()
	in := &event.Event{Type: event.EventCharacterCreated, AccountID: 1, CharacterID: 600}
	seedPending(t, a, in, "1")

	// The source entry is gone (trimmed): the copy has nothing to mirror.
	if err := a.client.XDel(ctx, event.TopicCharacters, in.ID).Err(); err != nil {
		t.Fatalf("xdel source: %v", err)
	}
	mr.SetTime(time.Now().Add(31 * time.Second))
	sweeper := newTestSweeper(t, mr, &buf, Options{MaxDelivery: 1})
	sweeper.deadLetter(ctx, event.TopicCharacters)

	if p := pendingEntries(t, a); p != 1 {
		t.Fatalf("pending after failed mirror = %d, want 1 (kept for retry)", p)
	}
	if strings.Contains(buf.String(), "dead-letter after max redelivery") {
		t.Fatalf("must not dead-letter without a mirror: %q", buf.String())
	}
}

// TestCopyToDeadLetterSourceReadError pins the copy's read-failure branch: a
// dead source read reports false instead of pretending the copy landed.
func TestCopyToDeadLetterSourceReadError(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a.copyToDeadLetter(cctx, event.TopicCharacters, "1-1", 5) {
		t.Fatal("source read failure must report false")
	}
}

// TestCopyToDeadLetterSourceMissing pins the empty-source branch: an entry
// trimmed away before the copy is reported false (nothing to mirror).
func TestCopyToDeadLetterSourceMissing(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	if a.copyToDeadLetter(context.Background(), event.TopicCharacters, "999-999", 5) {
		t.Fatal("missing source must report false")
	}
}

// TestCopyToDeadLetterMirrorWriteError pins the copy's write-failure branch: a
// dead-letter stream that cannot be written reports false, so the sweep
// leaves the entry pending instead of acking an unseen event.
func TestCopyToDeadLetterMirrorWriteError(t *testing.T) {
	base, mr := newClient(t)
	ctx := context.Background()
	id, err := base.XAdd(ctx, &redis.XAddArgs{
		Stream: event.TopicCharacters,
		Values: map[string]any{"type": string(event.EventCharacterCreated), "data": `{"event":"character.created"}`},
	}).Result()
	if err != nil {
		t.Fatalf("xadd: %v", err)
	}

	a := newWrapped(t, mr, func(c redis.UniversalClient) redis.UniversalClient {
		return &xaddFailClient{UniversalClient: c, failStream: event.DeadLetterTopic(event.TopicCharacters)}
	}, Options{})
	if a.copyToDeadLetter(ctx, event.TopicCharacters, id, 5) {
		t.Fatal("mirror write failure must report false")
	}
	if n, _ := a.client.XLen(ctx, event.DeadLetterTopic(event.TopicCharacters)).Result(); n != 0 {
		t.Fatalf("dead-letter stream holds %d copies, want 0", n)
	}
}

// TestConsumeDropsPoisonPayload pins the poison-payload contract: an entry
// whose data is not event JSON is acked and dropped with a log, never handed
// to the handler or left to loop forever.
func TestConsumeDropsPoisonPayload(t *testing.T) {
	var buf syncBuffer
	a, _ := newTestAdapter(t, Options{Block: 10 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	ctx := context.Background()

	if err := a.client.XAdd(ctx, &redis.XAddArgs{
		Stream: event.TopicCharacters,
		Values: map[string]any{"type": string(event.EventCharacterCreated), "data": "}{ not json"},
	}).Err(); err != nil {
		t.Fatalf("xadd poison: %v", err)
	}

	var calls atomic.Int32
	if err := a.Subscribe(ctx, event.TopicCharacters, func(context.Context, *event.Event) error {
		calls.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	waitFor(t, "poison drop log", func() bool {
		return strings.Contains(buf.String(), "undecodable payload, dropping")
	})
	if calls.Load() != 0 {
		t.Fatalf("handler calls = %d, want 0 for a poison payload", calls.Load())
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestConsumeAckFailureIsLogged pins the ack-error branch: a successful
// delivery whose XACK fails is logged (the entry stays pending and will be
// re-delivered — at-least-once).
func TestConsumeAckFailureIsLogged(t *testing.T) {
	_, mr := newClient(t)
	var buf syncBuffer
	a := newWrapped(t, mr, func(c redis.UniversalClient) redis.UniversalClient {
		return &xackFailClient{UniversalClient: c}
	}, Options{Block: 10 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	ctx := context.Background()

	if err := a.Subscribe(ctx, event.TopicCharacters, func(context.Context, *event.Event) error { return nil }); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := a.Publish(ctx, &event.Event{Type: event.EventCharacterCreated, AccountID: 1, CharacterID: 602}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitFor(t, "ack failure log", func() bool {
		return strings.Contains(buf.String(), "event: ack failed")
	})
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestReplayReadError pins the replay's stream-read wrap: a dead read is
// surfaced as an error, never a silent empty report.
func TestReplayReadError(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ReplayDeadLetters(cctx, event.TopicCharacters, event.ReplayOptions{}); err == nil {
		t.Fatal("replay with a dead read: expected error, got nil")
	}
}

// TestReplayPublishErrorLeavesCopy pins the re-publish failure branch: a copy
// whose re-publish fails is reported as an error and left in place.
func TestReplayPublishErrorLeavesCopy(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	ctx := context.Background()
	// A valid-JSON event with an unknown type decodes cleanly but cannot be
	// routed, so the re-publish fails and the copy stays put.
	if _, err := a.client.XAdd(ctx, &redis.XAddArgs{
		Stream: event.DeadLetterTopic(event.TopicCharacters),
		Values: map[string]any{"type": string(event.EventCharacterCreated), "data": `{"event":"totally_bogus"}`},
	}).Result(); err != nil {
		t.Fatalf("xadd dl: %v", err)
	}

	rep, err := a.ReplayDeadLetters(ctx, event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 0 || rep.Removed != 0 || !rep.Failed() {
		t.Fatalf("report = %+v, want 1/0/0 with one error", rep)
	}
	if n := dlLen(t, a); n != 1 {
		t.Fatalf("dl stream holds %d copies, want the failed one kept", n)
	}
}

// TestReplayDeleteAfterPublishFailure pins the report branch: a copy whose
// re-publish landed but whose delete failed is reported as an error, not a
// silent "removed" (the copy survives for a safe re-run).
func TestReplayDeleteAfterPublishFailure(t *testing.T) {
	base, mr := newClient(t)
	ctx := context.Background()
	seeder := New(base, Options{})
	seedDeadLetter(t, seeder, &event.Event{Type: event.EventCharacterUpdated, AccountID: 7, CharacterID: 603}, 3)

	a := newWrapped(t, mr, func(c redis.UniversalClient) redis.UniversalClient {
		return &xdelFailClient{UniversalClient: c}
	}, Options{})
	rep, err := a.ReplayDeadLetters(ctx, event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 1 || rep.Removed != 0 || !rep.Failed() {
		t.Fatalf("report = %+v, want 1/1/0 with one error", rep)
	}
	if n := dlLen(t, a); n != 1 {
		t.Fatalf("dl stream holds %d copies, want the survivor", n)
	}
}
