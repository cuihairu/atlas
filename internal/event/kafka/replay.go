package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/cuihairu/atlas/internal/event"
)

// ReplayDeadLetters re-publishes the "<topic>.dl" records onto the source
// topic, committing each dead-letter offset only after the re-publish
// succeeded (docs/sync.md §4). Records are read oldest-first by the
// offset-ordered fetch, so a backlog drains in publish order.
//
// Kafka cannot delete records, so "removed" means the committed offset:
// the record itself ages out with the topic's retention, and a second run
// of the same group starts past what was replayed. That is the store's
// honest notion of removal here, and Report documents it.
func (a *Adapter) ReplayDeadLetters(ctx context.Context, topic string, opts event.ReplayOptions) (event.Report, error) {
	if a.newDLReader == nil {
		return event.Report{}, fmt.Errorf("event: kafka replay: no dead-letter reader (open the adapter through Open)")
	}
	dl := a.newDLReader(event.DeadLetterTopic(topic))
	defer func() { _ = dl.Close() }()

	rep := event.Report{Topic: topic}
	for opts.Limit == 0 || rep.Found < opts.Limit {
		// One fetch per block interval: an empty topic is detected as a
		// deadline, not as an error to retry forever.
		fetchCtx, cancel := context.WithTimeout(ctx, a.block)
		msg, err := dl.FetchMessage(fetchCtx)
		cancel()
		switch {
		case err == nil:
		case errors.Is(err, context.DeadlineExceeded):
			return rep, nil
		case ctx.Err() != nil:
			return rep, ctx.Err()
		default:
			return rep, fmt.Errorf("event: kafka replay fetch %s: %w", event.DeadLetterTopic(topic), err)
		}

		id := fmt.Sprintf("%d@%d", msg.Partition, msg.Offset)
		rep.Found++
		var e event.Event
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: id, Err: err})
			a.logger.Error("event: dead-letter record not event JSON, offset left uncommitted",
				"topic", topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
			continue
		}
		if opts.DryRun {
			continue
		}
		if err := a.Publish(ctx, &e); err != nil {
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: id, Err: err})
			continue
		}
		rep.Republished++
		if err := dl.CommitMessages(ctx, msg); err != nil {
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: id, Err: fmt.Errorf("commit: %w", err)})
			continue
		}
		rep.Removed++
	}
	return rep, nil
}

// readDL builds the group reader scoped to one dead-letter topic, mirroring
// the main reader in Open.
func readDL(brokers []string, group string, block time.Duration, topic string) Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		GroupID:     group,
		Topic:       topic,
		MaxWait:     block,
		StartOffset: kafka.FirstOffset,
	})
}
