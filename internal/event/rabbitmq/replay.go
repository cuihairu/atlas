package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/cuihairu/atlas/internal/event"
)

// ReplayDeadLetters re-publishes the "<topic>.dl" queue's messages onto the
// source topic, acking each copy only after the re-publish succeeded
// (docs/sync.md §4). Deliveries are read oldest-first by the queue, so a
// backlog drains in publish order; a copy that cannot be decoded is
// reported with its delivery tag and left unacked, and the pass stops — a
// broker would otherwise redeliver it straight back and spin the process.
//
// A dry run consumes with manual acks (there is no "peek" in AMQP), so each
// copy is acked and re-enqueued unchanged: nothing reaches the source topic
// and the queue's contents are preserved.
//
// Crash between publish and ack is safe: the re-published copy is a fresh
// message and Directory.ApplyEvent is an upsert (§6).
func (a *Adapter) ReplayDeadLetters(ctx context.Context, topic string, opts event.ReplayOptions) (event.Report, error) {
	dl := event.DeadLetterTopic(topic)
	if _, err := a.ch.QueueDeclare(dl, true, false, false, false, nil); err != nil {
		return event.Report{}, fmt.Errorf("event: replay declare queue %s: %w", dl, err)
	}
	if err := a.ch.QueueBind(dl, dl, a.exchange, false, nil); err != nil {
		return event.Report{}, fmt.Errorf("event: replay bind queue %s: %w", dl, err)
	}
	deliveries, err := a.ch.Consume(dl, a.consumer+"-dl-replay", false, false, false, false, nil)
	if err != nil {
		return event.Report{}, fmt.Errorf("event: replay consume %s: %w", dl, err)
	}

	rep := event.Report{Topic: topic}
	drain := a.drainWait
	for opts.Limit == 0 || rep.Found < opts.Limit {
		select {
		case <-ctx.Done():
			return rep, ctx.Err()
		case msg, ok := <-deliveries:
			if !ok {
				return rep, nil // broker closed the channel
			}
			id := fmt.Sprint(msg.DeliveryTag)
			rep.Found++
			var e event.Event
			if err := json.Unmarshal(msg.Body, &e); err != nil {
				rep.Errors = append(rep.Errors, &event.ReplayError{ID: id, Err: err})
				a.logger.Error("event: dead-letter message not event JSON, left unacked",
					"queue", dl, "delivery_tag", msg.DeliveryTag, "error", err)
				// Stop: without an ack the broker redelivers immediately.
				return rep, nil
			}
			if opts.DryRun {
				if err := a.requeueCopy(ctx, dl, msg); err != nil {
					rep.Errors = append(rep.Errors, &event.ReplayError{ID: id, Err: err})
					return rep, nil
				}
				continue
			}
			if err := a.Publish(ctx, &e); err != nil {
				// The copy cannot reach the source topic; ack it out rather
				// than redelivering into the same pass forever.
				_ = a.ch.Ack(msg.DeliveryTag, false)
				rep.Errors = append(rep.Errors, &event.ReplayError{ID: id, Err: err})
				continue
			}
			rep.Republished++
			if err := a.ch.Ack(msg.DeliveryTag, false); err != nil {
				rep.Errors = append(rep.Errors, &event.ReplayError{ID: id, Err: fmt.Errorf("ack: %w", err)})
				continue
			}
			rep.Removed++
		case <-time.After(drain):
			return rep, nil // drained
		}
	}
	return rep, nil
}

// requeueCopy acks one consumed delivery and re-enqueues it unchanged, the
// dry-run's way of leaving the queue as it found it.
func (a *Adapter) requeueCopy(ctx context.Context, dl string, msg amqp.Delivery) error {
	if err := a.ch.Ack(msg.DeliveryTag, false); err != nil {
		return fmt.Errorf("dry-run ack: %w", err)
	}
	return a.ch.PublishWithContext(ctx, a.exchange, dl, false, false, amqp.Publishing{
		MessageId:    msg.MessageId,
		ContentType:  msg.ContentType,
		DeliveryMode: msg.DeliveryMode,
		Timestamp:    msg.Timestamp,
		Type:         msg.Type,
		Body:         msg.Body,
		Headers:      msg.Headers,
	})
}
