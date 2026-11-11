package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/cuihairu/atlas/internal/event"
)

// ReplayDeadLetters re-publishes the "<topic>.dl" subject's messages onto
// the source subject, deleting each copy from the stream only after the
// re-publish succeeded (docs/sync.md §4). An ephemeral pull consumer reads
// the copies oldest-first, so a backlog drains in publish order; a copy
// that cannot be decoded is reported with its stream sequence and left in
// place for inspection.
//
// Crash between publish and delete is safe: the re-published copy is a
// fresh message and Directory.ApplyEvent is an upsert (§6).
func (a *Adapter) ReplayDeadLetters(ctx context.Context, topic string, opts event.ReplayOptions) (event.Report, error) {
	dl := event.DeadLetterTopic(topic)
	// A durable push-less, pull consumer scoped to the dead-letter subject;
	// it is deleted at the end, so between runs the stream holds no replay
	// state beyond the copies themselves.
	const durable = "atlas-dl-replay"
	ci, err := a.js.AddConsumer(a.stream, &nats.ConsumerConfig{
		Durable:       durable,
		FilterSubject: dl,
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
	})
	if err != nil {
		return event.Report{}, fmt.Errorf("event: nats replay consumer %s: %w", dl, err)
	}
	defer func() { _ = a.js.DeleteConsumer(a.stream, ci.Config.Durable) }()
	sub, err := a.js.PullSubscribe(dl, durable, nats.BindStream(a.stream))
	if err != nil {
		return event.Report{}, fmt.Errorf("event: nats replay subscribe %s: %w", dl, err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	rep := event.Report{Topic: topic}
	for opts.Limit == 0 || rep.Found < opts.Limit {
		msgs, err := sub.Fetch(1, nats.MaxWait(a.fetch))
		if err != nil {
			switch {
			case errors.Is(err, nats.ErrTimeout):
				return rep, nil // drained
			case ctx.Err() != nil:
				return rep, ctx.Err()
			default:
				return rep, fmt.Errorf("event: nats replay fetch: %w", err)
			}
		}
		if len(msgs) == 0 {
			return rep, nil
		}
		msg := msgs[0]
		rep.Found++

		var e event.Event
		if err := json.Unmarshal(msg.Data, &e); err != nil {
			_ = msg.Ack()
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: seqID(msg), Err: err})
			a.logger.Error("event: dead-letter message not event JSON, left in place",
				"subject", dl, "stream_seq", seqID(msg), "error", err)
			continue
		}
		if opts.DryRun {
			_ = msg.Ack()
			continue
		}
		seq := uint64(0)
		if meta, err := msg.Metadata(); err == nil {
			seq = meta.Sequence.Stream
		}
		if seq == 0 {
			_ = msg.Ack()
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: seqID(msg), Err: fmt.Errorf("no stream sequence metadata")})
			continue
		}
		if err := a.Publish(ctx, &e); err != nil {
			_ = msg.Ack()
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: seqID(msg), Err: err})
			continue
		}
		rep.Republished++
		// Ack the delivery, then delete the copy. Ack first so a delete
		// failure cannot redeliver the same message within this pass; the
		// copy survives in the stream and a re-run finds it again.
		_ = msg.Ack()
		if err := a.js.DeleteMsg(a.stream, seq); err != nil {
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: seqID(msg), Err: fmt.Errorf("delete: %w", err)})
			return rep, nil
		}
		rep.Removed++
	}
	return rep, nil
}

func seqID(msg *nats.Msg) string {
	if meta, err := msg.Metadata(); err == nil {
		return fmt.Sprintf("%d", meta.Sequence.Stream)
	}
	return ""
}
