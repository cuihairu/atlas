package directory

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
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

// TestApplyEventLogoutStaleTimestampSkipped pins the logout ordering guard
// (the mirror of applyLogin's): an empty-session logout re-delivered after a
// newer login must not close that newer session — "close whatever is open"
// means the session that was open when the event was produced, not whatever
// happens to be open at consumption time.
func TestApplyEventLogoutStaleTimestampSkipped(t *testing.T) {
	mem := memory.New()
	svc := New(mem).WithPlayerSessions(mem)
	ctx := context.Background()

	older := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(2 * time.Hour)

	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 17,
		Name: "c", Timestamp: older,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, AccountID: 6, ServerID: "s1", CharacterID: 17,
		SessionID: "sess-a", Timestamp: older,
	}); err != nil {
		t.Fatalf("login sess-a: %v", err)
	}
	// A legitimate empty-session logout closes sess-a (timestamp newer than
	// the session it closes), then a fresh login opens sess-b.
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogout, AccountID: 6, ServerID: "s1", CharacterID: 17,
		Timestamp: older.Add(time.Minute),
	}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, AccountID: 6, ServerID: "s1", CharacterID: 17,
		SessionID: "sess-b", Timestamp: newer,
	}); err != nil {
		t.Fatalf("login sess-b: %v", err)
	}

	// Stale re-delivery of the logout (at-least-once transport, retried after
	// the newer login): its timestamp predates sess-b's start, so it is a
	// no-op — sess-b stays open and the character stays online.
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogout, AccountID: 6, ServerID: "s1", CharacterID: 17,
		Timestamp: older.Add(time.Minute),
	}); err != nil {
		t.Fatalf("stale logout must be a no-op, got %v", err)
	}

	active, err := mem.GetActivePlayerSession(ctx, 17)
	if err != nil {
		t.Fatalf("get active session: %v", err)
	}
	if active.SessionID != "sess-b" {
		t.Errorf("active session = %q, want sess-b (stale logout must not close it)", active.SessionID)
	}
	ch, err := mem.GetCharacter(ctx, 6, "s1", 17)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.PresenceStatus.Normalize() != model.PresenceOnline {
		t.Errorf("presence = %q, want online (stale logout must not flip offline)", ch.PresenceStatus)
	}
}
