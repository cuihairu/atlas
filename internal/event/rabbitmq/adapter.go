// Package rabbitmq implements the RabbitMQ event adapter (docs/sync.md §4),
// built on amqp091-go (pure Go).
//
// The adapter declares a durable topic exchange ("atlas"), one durable
// queue per consumed topic, and binds it with the topic as routing key.
// Publish sends a persistent message; Subscribe consumes with manual acks,
// so delivery is at-least-once — handlers must be idempotent, and
// Directory.ApplyEvent is.
package rabbitmq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/cuihairu/atlas/internal/event"
)

const (
	// DefaultURL is used when Options.URL is empty.
	DefaultURL = "amqp://localhost:5672/"
	// requeueBackoff pauses the consumer before Nack(requeue) so a
	// persistently failing handler retries at a sane rate instead of a
	// hot spin (rabbitmq gives no delivery counter to cap retries with).
	requeueBackoff = 5 * time.Second
	// DefaultExchange is the durable topic exchange all events route through.
	DefaultExchange = "atlas"
	// defaultQos is the consumer prefetch count.
	defaultQos = 16
)

// Channel is the subset of *amqp091.Channel the adapter needs. Injecting
// the interface keeps the consume/publish logic testable without a broker.
type Channel interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(queue, key, exchange string, noWait bool, args amqp.Table) error
	Qos(prefetchCount, prefetchSize int, global bool) error
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	Ack(tag uint64, multiple bool) error
	Nack(tag uint64, multiple, requeue bool) error
	Close() error
}

// Options tunes the adapter. Zero values select defaults.
type Options struct {
	// URL is the AMQP endpoint (default "amqp://localhost:5672/").
	URL string
	// Exchange overrides the topic exchange name (default "atlas").
	Exchange string
	// Consumer overrides this process's consumer tag
	// (default "hostname:pid").
	Consumer string
	// Qos is the prefetch count (default 16).
	Qos int
	// RequeueBackoff pauses the consumer before Nack(requeue) after a
	// handler failure (default 5s; rabbitmq exposes no delivery counter
	// to cap retries, so the pause is what keeps a failing event from
	// hot-spinning the loop).
	RequeueBackoff time.Duration
	// Logger receives consume failures (default slog.Default()).
	Logger *slog.Logger
	// MaxDelivery caps failed-delivery attempts per message before it is
	// republished to "<topic>.dl" and the original is acked (docs/sync.md
	// §4). 0 = no cap: Nack(requeue) loops forever with the 5s backoff —
	// the historical behavior. The count is tracked in-process; a restart
	// resets it, so the cap is per process lifetime, but a dead-lettered
	// message is acked and never requeueed.
	MaxDelivery int
	// DeliveryMetrics counts failed deliveries and dead letters (nil = a
	// no-op).
	DeliveryMetrics event.DeliveryMetrics
}

// Adapter delivers events over RabbitMQ.
type Adapter struct {
	ch       Channel
	exchange string
	consumer string
	qos      int
	backoff  time.Duration
	logger   *slog.Logger
	owns     bool

	maxDelivery int
	metrics     event.DeliveryMetrics
	attempts    *event.AttemptTracker

	mu      sync.Mutex
	cancels []context.CancelFunc
	wg      sync.WaitGroup
}

// Open connects to the broker, declares the exchange, and returns an
// adapter owning the channel (Close releases it).
func Open(opts Options) (*Adapter, error) {
	url := opts.URL
	if url == "" {
		url = DefaultURL
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("event: amqp dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("event: amqp channel: %w", err)
	}
	a := New(ch, opts)
	a.owns = true
	if err := ch.ExchangeDeclare(a.exchange, "topic", true, false, false, false, nil); err != nil {
		a.Close()
		return nil, fmt.Errorf("event: amqp declare exchange: %w", err)
	}
	return a, nil
}

// New wraps a pre-built channel. The adapter closes it on Close (pass a
// fake implementation in tests).
func New(ch Channel, opts Options) *Adapter {
	a := &Adapter{
		ch:          ch,
		exchange:    opts.Exchange,
		consumer:    opts.Consumer,
		qos:         opts.Qos,
		backoff:     opts.RequeueBackoff,
		logger:      opts.Logger,
		maxDelivery: opts.MaxDelivery,
		metrics:     event.MetricsOrNop(opts.DeliveryMetrics),
		attempts:    event.NewAttemptTracker(),
	}
	if a.exchange == "" {
		a.exchange = DefaultExchange
	}
	if a.consumer == "" {
		host, _ := os.Hostname()
		a.consumer = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	if a.qos <= 0 {
		a.qos = defaultQos
	}
	if a.backoff <= 0 {
		a.backoff = requeueBackoff
	}
	if a.logger == nil {
		a.logger = slog.Default()
	}
	return a
}

// Name implements event.EventAdapter.
func (a *Adapter) Name() string { return "rabbitmq" }

// Synchronous implements event.EventAdapter.
func (a *Adapter) Synchronous() bool { return false }

// Publish sends the event as a persistent message with a generated message
// ID (RabbitMQ does not assign one).
func (a *Adapter) Publish(ctx context.Context, e *event.Event) error {
	topic, err := event.TopicFor(e.Type)
	if err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("event: marshal: %w", err)
	}
	id := newMessageID()
	err = a.ch.PublishWithContext(ctx, a.exchange, topic, false, false, amqp.Publishing{
		MessageId:    id,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Type:         string(e.Type),
		Body:         data,
	})
	if err != nil {
		return fmt.Errorf("event: amqp publish: %w", err)
	}
	e.ID = id
	return nil
}

// Subscribe declares the topic's durable queue, binds it to the exchange,
// and starts consuming with manual acks until ctx is cancelled.
func (a *Adapter) Subscribe(ctx context.Context, topic string, h event.Handler) error {
	if _, err := a.ch.QueueDeclare(topic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("event: amqp declare queue: %w", err)
	}
	if err := a.ch.QueueBind(topic, topic, a.exchange, false, nil); err != nil {
		return fmt.Errorf("event: amqp bind queue: %w", err)
	}
	if a.maxDelivery > 0 {
		// Dead-letter destination for the cap: a sibling queue on the same
		// exchange, never consumed by Atlas.
		dl := event.DeadLetterTopic(topic)
		if _, err := a.ch.QueueDeclare(dl, true, false, false, false, nil); err != nil {
			return fmt.Errorf("event: amqp declare dead-letter queue: %w", err)
		}
		if err := a.ch.QueueBind(dl, dl, a.exchange, false, nil); err != nil {
			return fmt.Errorf("event: amqp bind dead-letter queue: %w", err)
		}
	}
	if err := a.ch.Qos(a.qos, 0, false); err != nil {
		return fmt.Errorf("event: amqp qos: %w", err)
	}
	deliveries, err := a.ch.Consume(topic, a.consumer, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("event: amqp consume: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.mu.Lock()
	a.cancels = append(a.cancels, cancel)
	a.mu.Unlock()
	a.wg.Add(1)
	go a.consume(runCtx, topic, deliveries, h)
	return nil
}

// Ack implements event.EventAdapter; acks happen inside the consume loop,
// so this is a no-op kept for interface symmetry.
func (a *Adapter) Ack(ctx context.Context, e *event.Event) error { return nil }

// Close stops all consume loops and closes the channel.
func (a *Adapter) Close() error {
	a.mu.Lock()
	cancels := a.cancels
	a.cancels = nil
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	a.wg.Wait()
	return a.ch.Close()
}

func (a *Adapter) consume(ctx context.Context, topic string, deliveries <-chan amqp.Delivery, h event.Handler) {
	defer a.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-deliveries:
			if !ok {
				return // channel closed by the broker/shutdown
			}
			a.handle(ctx, topic, h, msg)
		}
	}
}

func (a *Adapter) handle(ctx context.Context, topic string, h event.Handler, msg amqp.Delivery) {
	var e event.Event
	if err := json.Unmarshal(msg.Body, &e); err != nil {
		// Poison payload: it can never succeed, so ack it and move on.
		a.logger.Error("event: undecodable payload, dropping",
			"exchange", a.exchange, "message_id", msg.MessageId, "error", err)
		_ = a.ch.Ack(msg.DeliveryTag, false)
		return
	}
	e.ID = msg.MessageId

	if err := h(ctx, &e); err != nil {
		a.logger.Error("event: handler failed, requeueing",
			"exchange", a.exchange, "message_id", msg.MessageId,
			"type", string(e.Type), "error", err)
		a.metrics.ObserveRedelivery(a.Name())
		if a.maxDelivery > 0 && a.attempts.Failed(e.ID) >= a.maxDelivery {
			a.deadLetter(ctx, topic, msg)
			return
		}
		// Back off before the requeue: rabbitmq has no delivery counter to
		// cap retries against, and Nack(requeue) on the same connection
		// hands the message straight back — without a pause a persistently
		// failing event is a hot spin burning CPU and log volume. The
		// sleep happens on the consumer goroutine, so it also throttles
		// subsequent deliveries; at-least-once is preserved.
		time.Sleep(a.backoff)
		_ = a.ch.Nack(msg.DeliveryTag, false, true)
		return
	}
	a.attempts.Reset(e.ID)
	if err := a.ch.Ack(msg.DeliveryTag, false); err != nil {
		a.logger.Error("event: ack failed", "exchange", a.exchange, "message_id", msg.MessageId, "error", err)
	}
}

// deadLetter copies an exhausted message to "<topic>.dl" and acks the
// original, ending its requeue chain (docs/sync.md §4). A failed copy
// falls through to the plain requeue path so the broker redelivers and the
// next attempt retries the copy — the message is never dropped to make a
// dead-letter write succeed.
func (a *Adapter) deadLetter(ctx context.Context, topic string, msg amqp.Delivery) {
	if err := a.ch.PublishWithContext(ctx, a.exchange, event.DeadLetterTopic(topic), false, false, amqp.Publishing{
		MessageId:    msg.MessageId,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Type:         msg.Type,
		Body:         msg.Body,
		Headers:      msg.Headers,
	}); err != nil {
		a.logger.Error("event: dead-letter publish failed",
			"exchange", a.exchange, "message_id", msg.MessageId, "error", err)
		// Requeue as usual: the cap attempt is retried on the next delivery.
		time.Sleep(a.backoff)
		_ = a.ch.Nack(msg.DeliveryTag, false, true)
		return
	}
	a.metrics.ObserveDeadLetter(a.Name(), topic)
	a.attempts.Reset(msg.MessageId)
	a.logger.Error("event: dead-lettered after failed deliveries",
		"exchange", a.exchange, "message_id", msg.MessageId)
	_ = a.ch.Ack(msg.DeliveryTag, false)
}

// newMessageID returns a random 16-byte hex ID for publish tracing.
func newMessageID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
