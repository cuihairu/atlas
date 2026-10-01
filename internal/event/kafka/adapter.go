// Package kafka implements the Kafka event adapter (docs/sync.md §4),
// built on segmentio/kafka-go (pure Go, no cgo).
//
// Publish writes a record to the topic; Subscribe joins a consumer group
// and commits offsets only after the handler succeeds, so delivery is
// at-least-once — handlers must be idempotent, and Directory.ApplyEvent
// is. A record whose handler fails is redelivered to any group member on
// the next rebalance/poll.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/cuihairu/atlas/internal/event"
)

const (
	// DefaultGroup is the consumer group name.
	DefaultGroup = "atlas"
	// DefaultBrokers is used when Options.Brokers is empty.
	DefaultBrokers = "localhost:9092"
	// defaultBlock is how long FetchMessage waits before re-checking ctx.
	defaultBlock = 5 * time.Second
)

// Writer is the subset of *kafka.Writer the adapter needs.
type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// Reader is the subset of *kafka.Reader the adapter needs.
type Reader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// Options tunes the adapter. Zero values select defaults.
type Options struct {
	// Brokers seeds the client connection (default "localhost:9092").
	Brokers []string
	// Group overrides the consumer group (default "atlas").
	Group string
	// Block is the fetch wait interval (default 5s).
	Block time.Duration
	// Logger receives consume failures (default slog.Default()).
	Logger *slog.Logger
}

// Adapter delivers events over Kafka.
type Adapter struct {
	writer Writer
	reader Reader
	group  string
	block  time.Duration
	logger *slog.Logger

	mu      sync.Mutex
	cancels []context.CancelFunc
	wg      sync.WaitGroup
}

// Open connects to the brokers and returns an adapter owning the
// connections (Close releases them). topic is the record topic; the
// standard deployment uses event.TopicCharacters for every event type.
func Open(ctx context.Context, opts Options) (*Adapter, error) {
	brokers := opts.Brokers
	if len(brokers) == 0 {
		brokers = []string{DefaultBrokers}
	}
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Balancer:     &kafka.RoundRobin{},
		RequiredAcks: kafka.RequireAll,
		BatchTimeout: 10 * time.Millisecond,
	}
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		GroupID:     groupOf(opts),
		MaxWait:     blockOf(opts),
		StartOffset: kafka.FirstOffset,
	})
	return New(w, r, opts), nil
}

// New wraps pre-built client halves. The adapter owns them: Close closes
// both (pass fake implementations in tests).
func New(w Writer, r Reader, opts Options) *Adapter {
	return &Adapter{
		writer: w,
		reader: r,
		group:  groupOf(opts),
		block:  blockOf(opts),
		logger: loggerOf(opts),
	}
}

func groupOf(opts Options) string {
	if opts.Group != "" {
		return opts.Group
	}
	return DefaultGroup
}

func blockOf(opts Options) time.Duration {
	if opts.Block > 0 {
		return opts.Block
	}
	return defaultBlock
}

func loggerOf(opts Options) *slog.Logger {
	if opts.Logger != nil {
		return opts.Logger
	}
	return slog.Default()
}

// Name implements event.EventAdapter.
func (a *Adapter) Name() string { return "kafka" }

// Synchronous implements event.EventAdapter.
func (a *Adapter) Synchronous() bool { return false }

// Publish writes the event to its topic and stamps the record's
// partition@offset as the event ID.
func (a *Adapter) Publish(ctx context.Context, e *event.Event) error {
	topic, err := event.TopicFor(e.Type)
	if err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("event: marshal: %w", err)
	}
	msg := kafka.Message{Topic: topic, Key: []byte(fmt.Sprint(e.CharacterID)), Value: data}
	if err := a.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("event: kafka write: %w", err)
	}
	e.ID = fmt.Sprintf("%d@%d", msg.Partition, msg.Offset)
	return nil
}

// Subscribe starts group consumption in the background until ctx is
// cancelled. A record is committed only after its handler succeeds.
func (a *Adapter) Subscribe(ctx context.Context, topic string, h event.Handler) error {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.mu.Lock()
	a.cancels = append(a.cancels, cancel)
	a.mu.Unlock()
	a.wg.Add(1)
	go a.consume(runCtx, h)
	return nil
}

// Ack implements event.EventAdapter; commits happen inside the consume
// loop, so this is a no-op kept for interface symmetry.
func (a *Adapter) Ack(ctx context.Context, e *event.Event) error { return nil }

// Close stops all consume loops and closes the client halves.
func (a *Adapter) Close() error {
	a.mu.Lock()
	cancels := a.cancels
	a.cancels = nil
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	a.wg.Wait()
	errW := a.writer.Close()
	errR := a.reader.Close()
	return errors.Join(errW, errR)
}

func (a *Adapter) consume(ctx context.Context, h event.Handler) {
	defer a.wg.Done()
	for ctx.Err() == nil {
		msg, err := a.reader.FetchMessage(ctx)
		switch {
		case err == nil:
			a.handle(ctx, h, msg)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return
		default:
			if ctx.Err() != nil {
				return
			}
			a.logger.Error("event: fetch failed", "group", a.group, "error", err)
			time.Sleep(a.block)
		}
	}
}

func (a *Adapter) handle(ctx context.Context, h event.Handler, msg kafka.Message) {
	var e event.Event
	if err := json.Unmarshal(msg.Value, &e); err != nil {
		// Poison payload: it can never succeed, so commit it and move on.
		a.logger.Error("event: undecodable payload, dropping",
			"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
		a.commit(ctx, msg)
		return
	}
	e.ID = fmt.Sprintf("%d@%d", msg.Partition, msg.Offset)

	if err := h(ctx, &e); err != nil {
		a.logger.Error("event: handler failed, offset not committed",
			"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset,
			"type", string(e.Type), "error", err)
		return
	}
	a.commit(ctx, msg)
}

func (a *Adapter) commit(ctx context.Context, msg kafka.Message) {
	if err := a.reader.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
		a.logger.Error("event: commit failed", "topic", msg.Topic, "offset", msg.Offset, "error", err)
	}
}
