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
// (docs/sync.md §out-of-order handling — eventual consistency, no event
// versions), but retreating a field that is only ever supposed to advance
// is always wrong: it can never be a newer value, only a stale one. These
// tests pin the one monotonic field the projection owns.
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

// TestApplyEventUpdatedPreservesPresence pins docs/presence.md §3: login and
// logout are the only operations that move presence — a generic
// character.updated patch never touches presence_status, and with a session
// store attached it must not open or close any session either.
func TestApplyEventUpdatedPreservesPresence(t *testing.T) {
	mem := memory.New()
	svc := New(mem).WithPlayerSessions(mem)
	ctx := context.Background()

	t0 := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 18,
		Name: "c", Timestamp: t0,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, AccountID: 6, ServerID: "s1", CharacterID: 18,
		SessionID: "sess-a", Timestamp: t0.Add(time.Minute),
	}); err != nil {
		t.Fatalf("login: %v", err)
	}

	// A generic update (level bump) must leave the presence projection and
	// the open session untouched.
	level := 42
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterUpdated, AccountID: 6, ServerID: "s1", CharacterID: 18,
		Level: &level, Timestamp: t0.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	ch, err := mem.GetCharacter(ctx, 6, "s1", 18)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.Level != 42 {
		t.Errorf("level = %d, want 42 (update must apply)", ch.Level)
	}
	if ch.PresenceStatus.Normalize() != model.PresenceOnline {
		t.Errorf("presence = %q, want online (updated must not touch presence)", ch.PresenceStatus)
	}
	active, err := mem.GetActivePlayerSession(ctx, 18)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active.SessionID != "sess-a" {
		t.Errorf("active session = %q, want sess-a (updated must not open/close sessions)", active.SessionID)
	}
}

// TestApplyEventMovedResetsPresenceAndHealsStray pins docs/presence.md §3:
// presence never travels with a move — the destination row starts unknown,
// the source's open session survives as a stray, and the next login closes
// that stray as crash (crash healing).
func TestApplyEventMovedResetsPresenceAndHealsStray(t *testing.T) {
	mem := memory.New()
	svc := New(mem).WithPlayerSessions(mem)
	ctx := context.Background()

	t0 := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 19,
		Name: "c", Timestamp: t0,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, AccountID: 6, ServerID: "s1", CharacterID: 19,
		SessionID: "sess-a", Timestamp: t0.Add(time.Minute),
	}); err != nil {
		t.Fatalf("login: %v", err)
	}

	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterMoved, AccountID: 6, ServerID: "s1", CharacterID: 19,
		TargetServerID: "s2", Timestamp: t0.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("move: %v", err)
	}

	// Destination row sits at s2 and starts unknown: the move carried the
	// character, not its presence.
	dst, err := mem.GetCharacter(ctx, 6, "s2", 19)
	if err != nil {
		t.Fatalf("get dst: %v", err)
	}
	if dst.PresenceStatus.Normalize() != model.PresenceUnknown {
		t.Errorf("dst presence = %q, want unknown (presence must not travel)", dst.PresenceStatus)
	}
	if dst.Name != "c" {
		t.Errorf("dst name = %q, want c", dst.Name)
	}
	// The source row's open session is left behind as a stray.
	stray, err := mem.GetActivePlayerSession(ctx, 19)
	if err != nil {
		t.Fatalf("get stray: %v", err)
	}
	if stray.SessionID != "sess-a" {
		t.Errorf("stray = %q, want sess-a (move must not close the old session)", stray.SessionID)
	}

	// The next login heals the stray (closed as crash) and opens the new one.
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, AccountID: 6, ServerID: "s2", CharacterID: 19,
		SessionID: "sess-b", Timestamp: t0.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("login at dst: %v", err)
	}
	old, err := mem.GetPlayerSession(ctx, "sess-a")
	if err != nil {
		t.Fatalf("get old session: %v", err)
	}
	if old.EndReason != model.SessionEndCrash {
		t.Errorf("old end_reason = %q, want crash", old.EndReason)
	}
	active, err := mem.GetActivePlayerSession(ctx, 19)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active.SessionID != "sess-b" {
		t.Errorf("active = %q, want sess-b", active.SessionID)
	}
	after, err := mem.GetCharacter(ctx, 6, "s2", 19)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.PresenceStatus.Normalize() != model.PresenceOnline {
		t.Errorf("presence = %q, want online after heal", after.PresenceStatus)
	}
}
