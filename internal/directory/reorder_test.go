package directory

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/event"
)

// Out-of-order character events are an accepted v0.1 tradeoff
// (docs/sync.md §乱序处理 — eventual consistency, no event versions), but
// retreating a field that is only ever supposed to advance is always
// wrong: it can never be a newer value, only a stale one. These tests pin
// the one monotonic field the projection owns.
func TestApplyEventCreatedReplayDoesNotRetreatLoginAt(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	older := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	newer := older.Add(time.Hour)

	created := &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 13,
		Name: "c", Timestamp: older,
	}
	if _, err := svc.ApplyEvent(ctx, created); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, CharacterID: 13, Timestamp: newer,
	}); err != nil {
		t.Fatalf("login: %v", err)
	}

	// Redelivery of the original created event (at-least-once transport).
	if _, err := svc.ApplyEvent(ctx, created); err != nil {
		t.Fatalf("replayed create: %v", err)
	}

	ch, err := mem.GetCharacter(ctx, 6, "s1", 13)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.LastLoginAt == nil || !ch.LastLoginAt.Equal(newer) {
		t.Fatalf("last_login_at = %v, want %v (replay must not retreat it)", ch.LastLoginAt, newer)
	}
	if ch.Name != "c" {
		t.Errorf("replay lost the name: %q", ch.Name)
	}
}

func TestApplyEventLoginStaleTimestampSkipped(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	older := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(2 * time.Hour)

	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 14,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, CharacterID: 14, Timestamp: newer,
	}); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, CharacterID: 14, Timestamp: older,
	}); err != nil {
		t.Fatalf("stale login must be a no-op, got %v", err)
	}

	ch, err := mem.GetCharacter(ctx, 6, "s1", 14)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.LastLoginAt == nil || !ch.LastLoginAt.Equal(newer) {
		t.Fatalf("last_login_at = %v, want %v (stale login must not retreat it)", ch.LastLoginAt, newer)
	}
}

// TestApplyEventCreatedReplayAdvancesWhenNewer keeps the guard one-way:
// a create event newer than the row still stamps its timestamp, so an
// honest backfill never gets dropped.
func TestApplyEventCreatedReplayAdvancesWhenNewer(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	older := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	newer := older.Add(time.Hour)

	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 15,
		Name: "c", Timestamp: older,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 15,
		Name: "c", Timestamp: newer,
	}); err != nil {
		t.Fatalf("newer replay: %v", err)
	}

	ch, err := mem.GetCharacter(ctx, 6, "s1", 15)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.LastLoginAt == nil || !ch.LastLoginAt.Equal(newer) {
		t.Fatalf("last_login_at = %v, want %v (newer replay must advance it)", ch.LastLoginAt, newer)
	}
}
