package redis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cuihairu/atlas/internal/event"
)

// ReplayDeadLetters re-publishes the "<topic>.dl" stream entries onto the
// source stream, deleting each copy only after the re-publish succeeded
// (docs/sync.md §4). Entries are read oldest-first, so a backlog drains in
// publish order; a copy that cannot be decoded is reported with its stream
// ID and left in place for inspection, never deleted — a corrupt copy
// would otherwise be discarded as "replayed".
//
// Crash between publish and delete is safe: the re-published copy is a
// fresh entry and Directory.ApplyEvent is an upsert (§6), so a re-run
// re-applies idempotently.
func (a *Adapter) ReplayDeadLetters(ctx context.Context, topic string, opts event.ReplayOptions) (event.Report, error) {
	dl := event.DeadLetterTopic(topic)
	entries, err := a.client.XRange(ctx, dl, "-", "+").Result()
	if err != nil {
		return event.Report{}, fmt.Errorf("event: replay read %s: %w", dl, err)
	}
	// XRANGE has no count limit in go-redis; cap in Go on the already
	// ascending result, which keeps the oldest copies first.
	if opts.Limit > 0 && len(entries) > opts.Limit {
		entries = entries[:opts.Limit]
	}

	rep := event.Report{Topic: topic, Found: len(entries)}
	for _, entry := range entries {
		var e event.Event
		raw, _ := entry.Values["data"].(string)
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: entry.ID, Err: err})
			a.logger.Error("event: dead-letter copy not event JSON, left in place",
				"topic", topic, "dl", dl, "id", entry.ID, "error", err)
			continue
		}
		if opts.DryRun {
			continue
		}
		if err := a.Publish(ctx, &e); err != nil {
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: entry.ID, Err: err})
			continue
		}
		rep.Republished++
		n, err := a.client.XDel(ctx, dl, entry.ID).Result()
		if err != nil || n == 0 {
			// The re-publish landed; the copy just survived. Report it —
			// a second pass would re-publish again, which is safe (§6).
			rep.Errors = append(rep.Errors, &event.ReplayError{ID: entry.ID, Err: fmt.Errorf("delete: %v (removed %d)", err, n)})
			continue
		}
		rep.Removed++
	}
	return rep, nil
}
