// Edge coverage for the consume path: a fetch failure arriving after the
// context is canceled must exit the loop silently (no error spam during
// shutdown), and an ack that cannot be published (dead connection) must be
// logged without failing the handler.
package nats

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/cuihairu/atlas/internal/event"
)

func TestFetchErrorWithCanceledContextExitsSilently(t *testing.T) {
	var buf syncBuffer
	a := openAdapter(t)
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))
	a.fetch = 50 * time.Millisecond

	sub, err := a.js.PullSubscribe(event.TopicCharacters, "cancel-ctx", nats.BindStream(a.stream))
	if err != nil {
		t.Fatalf("pull subscribe: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.wg.Add(1)
	go a.consume(ctx, sub, func(_ context.Context, _ *event.Event) error { return nil })

	// Park the loop inside Fetch, then cancel and kill the connection: the
	// in-flight fetch fails with a connection error while ctx.Err() != nil.
	time.Sleep(20 * time.Millisecond)
	cancel()
	a.conn.Close()

	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("consume loop did not exit after cancel+close")
	}
	if strings.Contains(buf.String(), "event: fetch failed") {
		t.Fatalf("canceled context must exit silently, log = %q", buf.String())
	}
}

func TestHandleAckFailureIsLogged(t *testing.T) {
	var buf syncBuffer
	a := openAdapter(t)
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))

	// Pull a real message while the connection is healthy…
	data, err := json.Marshal(&event.Event{Type: event.EventCharacterCreated, CharacterID: 1})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := a.js.Publish(event.TopicCharacters, data); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := a.js.AddConsumer(a.stream, &nats.ConsumerConfig{
		Durable:       "ack-fail",
		FilterSubject: event.TopicCharacters,
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		ReplayPolicy:  nats.ReplayInstantPolicy,
	}); err != nil {
		t.Fatalf("add consumer: %v", err)
	}
	sub, err := a.js.PullSubscribe(event.TopicCharacters, "ack-fail", nats.BindStream(a.stream))
	if err != nil {
		t.Fatalf("pull subscribe: %v", err)
	}
	msgs, err := sub.Fetch(1, nats.MaxWait(time.Second))
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fetch = %v (%d), want 1 message", err, len(msgs))
	}

	// …then the connection dies before the ack is published.
	a.conn.Close()
	a.handle(context.Background(), func(_ context.Context, _ *event.Event) error { return nil }, msgs[0])
	if !strings.Contains(buf.String(), "event: ack failed") {
		t.Fatalf("ack failure never logged, buffer = %q", buf.String())
	}
}
