package redis

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/event"
)

// seedDeadLetter mirrors one copyToDeadLetter write: the source entry's
// values plus the source_id / deliveries provenance, into "<topic>.dl".
func seedDeadLetter(t *testing.T, a *Adapter, e *event.Event, deliveries int64) string {
	t.Helper()
	if err := a.Publish(context.Background(), e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	id := e.ID
	entries, err := a.client.XRange(context.Background(), event.TopicCharacters, id, id).Result()
	if err != nil || len(entries) != 1 {
		t.Fatalf("xrange %s: %v (%d)", id, err, len(entries))
	}
	vals := entries[0].Values
	vals["source_id"] = id
	vals["deliveries"] = deliveries
	newID, err := a.client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: event.DeadLetterTopic(event.TopicCharacters),
		Values: vals,
	}).Result()
	if err != nil {
		t.Fatalf("xadd dl: %v", err)
	}
	// Remove the source entry so only the dead-letter copy holds the event.
	if err := a.client.XDel(context.Background(), event.TopicCharacters, id).Err(); err != nil {
		t.Fatalf("xdel source: %v", err)
	}
	return newID
}

func dlLen(t *testing.T, a *Adapter) int64 {
	t.Helper()
	n, err := a.client.XLen(context.Background(), event.DeadLetterTopic(event.TopicCharacters)).Result()
	if err != nil {
		t.Fatalf("xlen dl: %v", err)
	}
	return n
}

func TestReplayRepublishesAndDeletes(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	ctx := context.Background()
	seed := event.Event{Type: event.EventCharacterUpdated, AccountID: 7, CharacterID: 1001, Level: ptrLevel(42)}
	seedDeadLetter(t, a, &seed, 3)
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterDeleted, AccountID: 7, CharacterID: 1002}, 3)

	rep, err := a.ReplayDeadLetters(ctx, event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Topic != event.TopicCharacters || rep.Found != 2 || rep.Republished != 2 || rep.Removed != 2 || rep.Failed() {
		t.Fatalf("report = %+v, want 2/2/2 clean", rep)
	}
	if n := dlLen(t, a); n != 0 {
		t.Fatalf("dl stream holds %d copies, want 0", n)
	}
	// The copies must be back on the source stream as fresh canonical
	// entries (a new ID each, decodable by the normal consume path).
	entries, err := a.client.XRange(ctx, event.TopicCharacters, "-", "+").Result()
	if err != nil {
		t.Fatalf("xrange source: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("source stream holds %d entries, want the 2 replayed", len(entries))
	}
	ids := map[string]bool{seed.ID: true}
	for _, e := range entries {
		if ids[e.ID] {
			t.Fatalf("replayed entry reused the dead letter's source ID %s", e.ID)
		}
		var decoded event.Event
		if err := json.Unmarshal([]byte(e.Values["data"].(string)), &decoded); err != nil {
			t.Fatalf("replayed entry not valid event JSON: %v", err)
		}
	}
}

func TestReplayDryRunCountsOnly(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterLogin, AccountID: 7, CharacterID: 1003}, 2)

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{DryRun: true})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 1 || rep.Republished != 0 || rep.Removed != 0 {
		t.Fatalf("dry-run report = %+v, want 1/0/0", rep)
	}
	if n := dlLen(t, a); n != 1 {
		t.Fatalf("dry run left %d copies, want 1 untouched", n)
	}
	if n, _ := a.client.XLen(context.Background(), event.TopicCharacters).Result(); n != 0 {
		t.Fatalf("dry run published to the source stream (%d entries)", n)
	}
}

func TestReplayLimitProcessesOldestFirst(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	first := seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 2001}, 3)
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 2002}, 3)
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterCreated, AccountID: 7, CharacterID: 2003}, 3)

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{Limit: 2})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 2 || rep.Republished != 2 || rep.Removed != 2 {
		t.Fatalf("limited report = %+v, want 2/2/2", rep)
	}
	if n := dlLen(t, a); n != 1 {
		t.Fatalf("dl stream holds %d copies, want 1", n)
	}
	// The oldest copy (first) is the one replayed; the survivor must not be it.
	left, err := a.client.XRange(context.Background(), event.DeadLetterTopic(event.TopicCharacters), "-", "+").Result()
	if err != nil || len(left) != 1 {
		t.Fatalf("xrange dl: %v (%d)", err, len(left))
	}
	if sid, _ := left[0].Values["source_id"].(string); sid == first {
		t.Fatalf("survivor = %+v, want the newest copy (oldest was replayed)", left[0])
	}
}

func TestReplayCorruptCopyStaysPut(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	seedDeadLetter(t, a, &event.Event{Type: event.EventCharacterUpdated, AccountID: 7, CharacterID: 3001}, 3)
	if _, err := a.client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: event.DeadLetterTopic(event.TopicCharacters),
		Values: map[string]any{"type": "character.updated", "data": "}{ not json"},
	}).Result(); err != nil {
		t.Fatalf("xadd corrupt: %v", err)
	}

	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 2 || rep.Republished != 1 || rep.Removed != 1 || !rep.Failed() {
		t.Fatalf("report = %+v, want 2/1/1 with one error", rep)
	}
	if n := dlLen(t, a); n != 1 {
		t.Fatalf("dl stream holds %d copies, want only the corrupt one", n)
	}
}

func TestReplayEmptyChannelIsACleanNoop(t *testing.T) {
	a, _ := newTestAdapter(t, Options{})
	rep, err := a.ReplayDeadLetters(context.Background(), event.TopicCharacters, event.ReplayOptions{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Found != 0 || rep.Republished != 0 || rep.Removed != 0 || rep.Failed() {
		t.Fatalf("report = %+v, want all zero", rep)
	}
}

func ptrLevel(l int) *int { return &l }
