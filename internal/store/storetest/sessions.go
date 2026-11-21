package storetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// RunSessions is the behavioral contract for the player-session projection
// (PP): session lifecycle (open → online, close → offline) and the
// denormalized presence fields on the character index. Every store must
// satisfy it — the same suite runs against memory, PostgreSQL and MySQL.
//
// The suite seeds its own character under a unique account and keeps all
// state scoped to that character, so subtests stay order-independent.
func RunSessions(t *testing.T, s Core) {
	t.Helper()
	t.Run("SessionLifecycle", func(t *testing.T) {
		ctx := context.Background()
		uid := seq.Add(1)
		acct := 900000000000000 + uid
		ch := &model.Character{
			AccountID:   acct,
			ServerID:    fmt.Sprintf("game-sess-%d", uid),
			CharacterID: 800000000000000 + uid,
			Name:        "presence-char",
			Level:       1,
		}
		ch.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
		ch.UpdatedAt = ch.CreatedAt
		if err := s.UpsertCharacter(ctx, ch); err != nil {
			t.Fatalf("seed character: %v", err)
		}

		// A freshly projected character reads as "unknown": it has never
		// reported presence.
		got, err := s.GetCharacter(ctx, ch.AccountID, ch.ServerID, ch.CharacterID)
		if err != nil {
			t.Fatalf("get character: %v", err)
		}
		if got.PresenceStatus != model.PresenceUnknown {
			t.Fatalf("presence = %q, want %q", got.PresenceStatus, model.PresenceUnknown)
		}

		// A character that is not in the index cannot host a session.
		err = s.CreateSession(ctx, &model.PlayerSession{
			SessionID:   fmt.Sprintf("s-missing-%d", uid),
			AccountID:   acct,
			CharacterID: 999999999999999 - uid, // no index row
			ServerID:    ch.ServerID,
			StartedAt:   time.Now(),
		})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("create session on missing character: err = %v, want ErrNotFound", err)
		}

		// Open: the character goes online.
		started := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Microsecond)
		sess := &model.PlayerSession{
			SessionID:   fmt.Sprintf("s1-%d", uid),
			AccountID:   acct,
			CharacterID: ch.CharacterID,
			ServerID:    ch.ServerID,
			StartedAt:   started,
		}
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatalf("create session: %v", err)
		}
		got, err = s.GetCharacter(ctx, ch.AccountID, ch.ServerID, ch.CharacterID)
		if err != nil {
			t.Fatalf("get character: %v", err)
		}
		if got.PresenceStatus != model.PresenceOnline {
			t.Fatalf("presence after open = %q, want %q", got.PresenceStatus, model.PresenceOnline)
		}

		// A second open session for the same character is refused.
		err = s.CreateSession(ctx, &model.PlayerSession{
			SessionID:   fmt.Sprintf("s2-%d", uid),
			AccountID:   acct,
			CharacterID: ch.CharacterID,
			ServerID:    ch.ServerID,
			StartedAt:   time.Now(),
		})
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate open session: err = %v, want ErrConflict", err)
		}

		// The open session is readable by character.
		active, err := s.GetActiveSessionByCharacter(ctx, ch.CharacterID)
		if err != nil {
			t.Fatalf("get active session: %v", err)
		}
		if active.SessionID != sess.SessionID {
			t.Fatalf("active session = %q, want %q", active.SessionID, sess.SessionID)
		}

		// Close (normal): the character goes offline, LastLogoutAt = endedAt.
		endedAt := started.Add(30 * time.Minute).UTC().Truncate(time.Microsecond)
		closed, err := s.EndSession(ctx, sess.SessionID, endedAt, model.SessionEndNormal)
		if err != nil {
			t.Fatalf("end session: %v", err)
		}
		if closed.EndedAt == nil {
			t.Fatalf("closed.EndedAt = nil, want %v", endedAt)
		}
		sameTime(t, "closed.EndedAt", *closed.EndedAt, endedAt)
		if closed.EndReason != model.SessionEndNormal {
			t.Fatalf("closed.EndReason = %q, want %q", closed.EndReason, model.SessionEndNormal)
		}
		got, err = s.GetCharacter(ctx, ch.AccountID, ch.ServerID, ch.CharacterID)
		if err != nil {
			t.Fatalf("get character: %v", err)
		}
		if got.PresenceStatus != model.PresenceOffline {
			t.Fatalf("presence after close = %q, want %q", got.PresenceStatus, model.PresenceOffline)
		}
		if got.LastLogoutAt == nil {
			t.Fatalf("LastLogoutAt = nil, want %v", endedAt)
		}
		sameTime(t, "LastLogoutAt", *got.LastLogoutAt, endedAt)

		// Ending an already-closed session is refused.
		_, err = s.EndSession(ctx, sess.SessionID, endedAt.Add(time.Minute), model.SessionEndNormal)
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("double end: err = %v, want ErrConflict", err)
		}

		// Ending an unknown session is refused.
		_, err = s.EndSession(ctx, fmt.Sprintf("nope-%d", uid), time.Now(), model.SessionEndNormal)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("end unknown session: err = %v, want ErrNotFound", err)
		}

		// No open session is visible any more.
		_, err = s.GetActiveSessionByCharacter(ctx, ch.CharacterID)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("active session after close: err = %v, want ErrNotFound", err)
		}

		// Reopen: a new session puts the character back online.
		sess3 := &model.PlayerSession{
			SessionID:   fmt.Sprintf("s3-%d", uid),
			AccountID:   acct,
			CharacterID: ch.CharacterID,
			ServerID:    ch.ServerID,
			StartedAt:   endedAt.Add(time.Hour),
		}
		if err := s.CreateSession(ctx, sess3); err != nil {
			t.Fatalf("reopen session: %v", err)
		}
		got, err = s.GetCharacter(ctx, ch.AccountID, ch.ServerID, ch.CharacterID)
		if err != nil {
			t.Fatalf("get character: %v", err)
		}
		if got.PresenceStatus != model.PresenceOnline {
			t.Fatalf("presence after reopen = %q, want %q", got.PresenceStatus, model.PresenceOnline)
		}

		// Close with a non-normal reason.
		kickedAt := endedAt.Add(2 * time.Hour).UTC().Truncate(time.Microsecond)
		closed3, err := s.EndSession(ctx, sess3.SessionID, kickedAt, model.SessionEndKicked)
		if err != nil {
			t.Fatalf("end session kicked: %v", err)
		}
		if closed3.EndReason != model.SessionEndKicked {
			t.Fatalf("EndReason = %q, want %q", closed3.EndReason, model.SessionEndKicked)
		}
		got, err = s.GetCharacter(ctx, ch.AccountID, ch.ServerID, ch.CharacterID)
		if err != nil {
			t.Fatalf("get character: %v", err)
		}
		if got.PresenceStatus != model.PresenceOffline {
			t.Fatalf("presence after kick = %q, want %q", got.PresenceStatus, model.PresenceOffline)
		}

		// The session history is newest first (s3 then s1).
		list, err := s.ListSessionsByCharacter(ctx, ch.CharacterID, 0)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("list len = %d, want 2", len(list))
		}
		if list[0].SessionID != sess3.SessionID || list[1].SessionID != sess.SessionID {
			t.Fatalf("list order = [%s %s], want [%s %s]",
				list[0].SessionID, list[1].SessionID, sess3.SessionID, sess.SessionID)
		}
		if list[0].StartedAt.Before(list[1].StartedAt) {
			t.Fatalf("list is not newest first: %v then %v", list[0].StartedAt, list[1].StartedAt)
		}

		// limit=1 returns only the newest session.
		list, err = s.ListSessionsByCharacter(ctx, ch.CharacterID, 1)
		if err != nil {
			t.Fatalf("list sessions limit 1: %v", err)
		}
		if len(list) != 1 || list[0].SessionID != sess3.SessionID {
			t.Fatalf("list limit 1 = %v, want [%s]", list, sess3.SessionID)
		}

		// An invalid end reason is rejected before any write.
		if err := s.UpsertCharacter(ctx, ch); err != nil {
			t.Fatalf("re-seed: %v", err)
		}
		s4 := &model.PlayerSession{
			SessionID:   fmt.Sprintf("s4-%d", uid),
			AccountID:   acct,
			CharacterID: ch.CharacterID,
			ServerID:    ch.ServerID,
			StartedAt:   time.Now(),
		}
		if err := s.CreateSession(ctx, s4); err != nil {
			t.Fatalf("create s4: %v", err)
		}
		if _, err := s.EndSession(ctx, s4.SessionID, time.Now(), model.SessionEndReason("bogus")); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("bogus end reason: err = %v, want ErrInvalid", err)
		}
	})
}
