// Package nats implements the NATS JetStream event adapter (docs/sync.md §4).
//
// Publish sends the event to the topic's subject on the "ATLAS" stream;
// Subscribe attaches a durable pull consumer (explicit acks). Delivery is
// at-least-once: messages are acked only after the handler succeeds and are
// redelivered otherwise, so handlers must be idempotent — Directory.
// ApplyEvent is.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/cuihairu/atlas/internal/event"
)

const (
	// DefaultURL is used when Options.URL is empty.
	DefaultURL = "nats://localhost:4222"
	// DefaultStream is the JetStream stream carrying all Atlas subjects.
	DefaultStream = "ATLAS"
	// DefaultDurable is the durable consumer name shared by Atlas replicas.
	DefaultDurable = "atlas"
	// defaultFetch is how long a pull fetch waits before re-checking ctx.
	defaultFetch = 5 * time.Second
)

// Options tunes the adapter. Zero values select defaults.
type Options struct {
	// URL is the NATS server URL (default "nats://localhost:4222").
	URL string
	// Stream overrides the stream name (default "ATLAS").
	Stream string
	// Durable overrides the durable consumer name (default "atlas").
	Durable string
	// Fetch is the pull-fetch wait interval (default 5s).
	Fetch time.Duration
	// MaxDelivery caps failed-delivery attempts per message before it is
	// republished to "<subject>.dl" and acked (docs/sync.md §4). 0 = no cap:
	// the message stays unacked and the durable consumer redelivers it
	// forever (the server default MaxDeliver measures -1) — the historical
	// behavior. The counter is in-process; a restart resets it, so the cap
	// is per process lifetime, but a dead-lettered message is acked and
	// never re-delivered.
	MaxDelivery int
	// DeliveryMetrics counts failed deliveries and dead letters (nil = a
	// no-op).
	DeliveryMetrics event.DeliveryMetrics
	// Logger receives consume failures (default slog.Default()).
	Logger *slog.Logger
}

// Adapter delivers events over NATS JetStream.
type Adapter struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	stream  string
	durable string
	fetch   time.Duration
	logger  *slog.Logger
	owns    bool

	maxDelivery int
	metrics     event.DeliveryMetrics
	attempts    *event.AttemptTracker

	mu      sync.Mutex
	cancels []context.CancelFunc
	wg      sync.WaitGroup
}

// Open connects to the server, ensures the stream exists, and returns an
// adapter owning the connection (Close releases it).
func Open(opts Options) (*Adapter, error) {
	url := opts.URL
	if url == "" {
		url = DefaultURL
	}
	conn, err := nats.Connect(url, nats.Name("atlas"), nats.Timeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("event: nats connect: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("event: nats jetstream: %w", err)
	}
	a := New(conn, js, opts)
	a.owns = true
	// The dead-letter subject must be covered by the stream or JetStream
	// rejects publishes to it ("no responders"): extend the subject list on
	// creation, or reconcile an existing stream (a rolling restart onto the
	// new option leaves the old subject list behind).
	dl := event.DeadLetterTopic(event.TopicCharacters)
	if _, err := js.StreamInfo(a.stream); errors.Is(err, nats.ErrStreamNotFound) {
		subjects := []string{event.TopicCharacters}
		if a.maxDelivery > 0 {
			subjects = append(subjects, dl)
		}
		if _, err := js.AddStream(&nats.StreamConfig{
			Name:      a.stream,
			Subjects:  subjects,
			Retention: nats.LimitsPolicy,
		}); err != nil {
			conn.Close()
			return nil, fmt.Errorf("event: nats add stream: %w", err)
		}
	} else if err != nil {
		conn.Close()
		return nil, fmt.Errorf("event: nats stream info: %w", err)
	} else if a.maxDelivery > 0 {
		if info, err := js.StreamInfo(a.stream); err == nil && !containsString(info.Config.Subjects, dl) {
			cfg := info.Config
			cfg.Subjects = append(cfg.Subjects, dl)
			if _, err := js.UpdateStream(&cfg); err != nil {
				conn.Close()
				return nil, fmt.Errorf("event: nats update stream for dead letter: %w", err)
			}
		}
	}
	return a, nil
}

// New wraps a pre-built connection. The adapter closes the connection on
// Close only when created through Open.
func New(conn *nats.Conn, js nats.JetStreamContext, opts Options) *Adapter {
	a := &Adapter{
		conn:        conn,
		js:          js,
		stream:      opts.Stream,
		durable:     opts.Durable,
		fetch:       opts.Fetch,
		logger:      opts.Logger,
		maxDelivery: opts.MaxDelivery,
		metrics:     event.MetricsOrNop(opts.DeliveryMetrics),
		attempts:    event.NewAttemptTracker(),
	}
	if a.stream == "" {
		a.stream = DefaultStream
	}
	if a.durable == "" {
		a.durable = DefaultDurable
	}
	if a.fetch <= 0 {
		a.fetch = defaultFetch
	}
	if a.logger == nil {
		a.logger = slog.Default()
	}
	return a
}

// Name implements event.EventAdapter.
func (a *Adapter) Name() string { return "nats" }

// Synchronous implements event.EventAdapter.
func (a *Adapter) Synchronous() bool { return false }

// Publish sends the event to its topic's subject and stamps the stream
// sequence as the event ID.
func (a *Adapter) Publish(ctx context.Context, e *event.Event) error {
	topic, err := event.TopicFor(e.Type)
	if err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("event: marshal: %w", err)
	}
	ack, err := a.js.Publish(topic, data, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("event: nats publish: %w", err)
	}
	e.ID = fmt.Sprintf("%d", ack.Sequence)
	return nil
}

// Subscribe ensures the durable pull consumer exists and starts fetching
// in the background until ctx is cancelled.
func (a *Adapter) Subscribe(ctx context.Context, topic string, h event.Handler) error {
	sub, err := a.js.PullSubscribe(topic, a.durable, nats.BindStream(a.stream))
	if err != nil {
		return fmt.Errorf("event: nats pull subscribe: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.mu.Lock()
	a.cancels = append(a.cancels, cancel)
	a.mu.Unlock()
	a.wg.Add(1)
	go a.consume(runCtx, sub, h)
	return nil
}

// Ack implements event.EventAdapter; acks happen inside the consume loop,
// so this is a no-op kept for interface symmetry.
func (a *Adapter) Ack(ctx context.Context, e *event.Event) error { return nil }

// Close stops all consume loops and, when the connection is owned, closes it.
func (a *Adapter) Close() error {
	a.mu.Lock()
	cancels := a.cancels
	a.cancels = nil
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	a.wg.Wait()
	if a.owns {
		a.conn.Close()
	}
	return nil
}

func (a *Adapter) consume(ctx context.Context, sub *nats.Subscription, h event.Handler) {
	defer a.wg.Done()
	for ctx.Err() == nil {
		msgs, err := sub.Fetch(1, nats.MaxWait(a.fetch))
		switch {
		case err == nil:
			for _, msg := range msgs {
				a.handle(ctx, h, msg)
			}
		case errors.Is(err, nats.ErrTimeout), errors.Is(err, context.Canceled):
			continue
		default:
			if ctx.Err() != nil {
				return
			}
			a.logger.Error("event: fetch failed", "stream", a.stream, "durable", a.durable, "error", err)
			time.Sleep(a.fetch)
		}
	}
}

func (a *Adapter) handle(ctx context.Context, h event.Handler, msg *nats.Msg) {
	// Stream sequence doubles as the event ID (0 when metadata is absent).
	seq := uint64(0)
	if meta, err := msg.Metadata(); err == nil {
		seq = meta.Sequence.Stream
	}

	var e event.Event
	if err := json.Unmarshal(msg.Data, &e); err != nil {
		// Poison payload: it can never succeed, so ack it and move on.
		a.logger.Error("event: undecodable payload, dropping",
			"subject", msg.Subject, "stream_seq", seq, "error", err)
		_ = msg.Ack()
		return
	}
	e.ID = fmt.Sprintf("%d", seq)

	if err := h(ctx, &e); err != nil {
		a.logger.Error("event: handler failed, left unacked",
			"subject", msg.Subject, "stream_seq", seq,
			"type", string(e.Type), "error", err)
		a.metrics.ObserveRedelivery(a.Name())
		if a.maxDelivery > 0 {
			if n := a.attempts.Failed(e.ID); n >= a.maxDelivery {
				a.deadLetter(msg, seq, n)
			}
		}
		return
	}
	a.attempts.Reset(e.ID)
	if err := msg.Ack(); err != nil {
		a.logger.Error("event: ack failed", "subject", msg.Subject, "stream_seq", seq, "error", err)
	}
}

// deadLetter copies an exhausted message to "<subject>.dl" and acks the
// original, ending its redelivery chain (docs/sync.md §4). A failed copy
// leaves the original unacked, so the consumer redelivers and the next
// attempt retries the copy — the message is never dropped to make a
// dead-letter write succeed.
func (a *Adapter) deadLetter(msg *nats.Msg, seq uint64, n int) {
	if _, err := a.js.Publish(event.DeadLetterTopic(msg.Subject), msg.Data); err != nil {
		a.logger.Error("event: dead-letter publish failed",
			"subject", msg.Subject, "stream_seq", seq, "error", err)
		return
	}
	a.metrics.ObserveDeadLetter(a.Name(), msg.Subject)
	a.attempts.Reset(fmt.Sprintf("%d", seq))
	a.logger.Error("event: dead-lettered after failed deliveries",
		"subject", msg.Subject, "stream_seq", seq, "deliveries", n)
	if err := msg.Ack(); err != nil {
		a.logger.Error("event: dead-letter ack failed", "subject", msg.Subject, "stream_seq", seq, "error", err)
	}
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
