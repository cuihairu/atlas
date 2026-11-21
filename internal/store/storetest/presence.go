package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// presence pins the player-presence state machine (docs/presence.md §3)
// across memory / PostgreSQL / MySQL: the login opens a session AND flips the
// character online atomically, logout closes it AND flips it offline, and the
// idempotency / illegal-transition rules are identical everywhere.
func presence(t *testing.T, ctx context.Context, s Core) {
	serverA := id("pres-srv-a")
	serverB := id("pres-srv-b")
	account := time.Now().UnixNano()

	mk := func(serverID string, charID int64, name string) *model.Character {
		return &model.Character{
			AccountID: account, ServerID: serverID, CharacterID: charID,
			Name: name, Level: 1, ClassID: 1, Avatar: "a.png",
		}
	}

	// A fresh character has unknown presence until the first login.
	ch := mk(serverA, 501, "presence-one")
	if err := s.UpsertCharacter(ctx, ch); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.GetCharacter(ctx, account, serverA, 501)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PresenceStatus != model.PresenceUnknown {
		t.Errorf("fresh presence = %q, want unknown", got.PresenceStatus)
	}

	// Login opens the session and flips online, stamping last_login_at.
	loginAt := time.Now().UTC().Truncate(time.Microsecond)
	sess, err := s.Login(ctx, account, serverA, 501, "sess-501-a", loginAt)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if sess.SessionID != "sess-501-a" || !sess.Open() {
		t.Errorf("login session = %+v, want open sess-501-a", sess)
	}
	got, err = s.GetCharacter(ctx, account, serverA, 501)
	if err != nil {
		t.Fatalf("get after login: %v", err)
	}
	if got.PresenceStatus != model.PresenceOnline {
		t.Errorf("presence after login = %q, want online", got.PresenceStatus)
	}
	if got.LastLoginAt == nil || !sameTimeT(got.LastLoginAt, loginAt) {
		t.Errorf("last_login_at after login = %v, want %v", got.LastLoginAt, loginAt)
	}

	// GetActivePlayerSession resolves the open session.
	active, err := s.GetActivePlayerSession(ctx, 501)
	if err != nil {
		t.Fatalf("get active session: %v", err)
	}
	if active.SessionID != "sess-501-a" {
		t.Errorf("active session = %q, want sess-501-a", active.SessionID)
	}

	// Idempotent re-login with the same session: no-op, same session, still
	// one open session (at-least-once transports re-deliver login events).
	sess2, err := s.Login(ctx, account, serverA, 501, "sess-501-a", loginAt.Add(1*time.Millisecond))
	if err != nil {
		t.Fatalf("re-login same session: %v", err)
	}
	if sess2.SessionID != "sess-501-a" {
		t.Errorf("re-login returned %q, want sess-501-a", sess2.SessionID)
	}
	active, err = s.GetActivePlayerSession(ctx, 501)
	if err != nil {
		t.Fatalf("get active after re-login: %v", err)
	}
	if active.SessionID != "sess-501-a" {
		t.Errorf("active after re-login = %q, want sess-501-a", active.SessionID)
	}

	// Illegal: login while online in a different session.
	if _, err := s.Login(ctx, account, serverA, 501, "sess-501-b", loginAt.Add(2*time.Millisecond)); !errors.Is(err, store.ErrConflict) {
		t.Errorf("login different session = %v, want ErrConflict", err)
	}

	// Logout closes the session and flips offline, stamping last_logout_at.
	logoutAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.Logout(ctx, account, serverA, 501, "sess-501-a", model.SessionEndNormal, logoutAt); err != nil {
		t.Fatalf("logout: %v", err)
	}
	got, err = s.GetCharacter(ctx, account, serverA, 501)
	if err != nil {
		t.Fatalf("get after logout: %v", err)
	}
	if got.PresenceStatus != model.PresenceOffline {
		t.Errorf("presence after logout = %q, want offline", got.PresenceStatus)
	}
	if got.LastLogoutAt == nil || !sameTimeT(got.LastLogoutAt, logoutAt) {
		t.Errorf("last_logout_at after logout = %v, want %v", got.LastLogoutAt, logoutAt)
	}
	if _, err := s.GetActivePlayerSession(ctx, 501); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("active session after logout = %v, want ErrNotFound", err)
	}
	closed, err := s.GetPlayerSession(ctx, "sess-501-a")
	if err != nil {
		t.Fatalf("get closed session: %v", err)
	}
	if closed.Open() || closed.EndReason != model.SessionEndNormal {
		t.Errorf("closed session = %+v, want ended with reason normal", closed)
	}

	// Idempotent duplicate logout: already offline, no error.
	if err := s.Logout(ctx, account, serverA, 501, "sess-501-a", model.SessionEndNormal, logoutAt.Add(1*time.Millisecond)); err != nil {
		t.Errorf("duplicate logout = %v, want nil", err)
	}

	// Illegal: logout naming a session that is not the open one.
	ch2 := mk(serverA, 502, "presence-two")
	if err := s.UpsertCharacter(ctx, ch2); err != nil {
		t.Fatalf("upsert 502: %v", err)
	}
	if _, err := s.Login(ctx, account, serverA, 502, "sess-502-a", loginAt); err != nil {
		t.Fatalf("login 502: %v", err)
	}
	if err := s.Logout(ctx, account, serverA, 502, "sess-502-wrong", model.SessionEndKicked, logoutAt); !errors.Is(err, store.ErrConflict) {
		t.Errorf("logout wrong session = %v, want ErrConflict", err)
	}
	if err := s.Logout(ctx, account, serverA, 502, "sess-502-a", model.SessionEndKicked, logoutAt); err != nil {
		t.Fatalf("logout 502: %v", err)
	}

	// Illegal: logout on a character that never logged in (unknown).
	ch3 := mk(serverB, 503, "presence-never")
	if err := s.UpsertCharacter(ctx, ch3); err != nil {
		t.Fatalf("upsert 503: %v", err)
	}
	if err := s.Logout(ctx, account, serverB, 503, "", model.SessionEndNormal, logoutAt); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("logout never-logged-in = %v, want ErrInvalid", err)
	}

	// Illegal: invalid end reason.
	if err := s.UpsertCharacter(ctx, mk(serverA, 504, "presence-invalid-reason")); err != nil {
		t.Fatalf("upsert 504: %v", err)
	}
	if _, err := s.Login(ctx, account, serverA, 504, "sess-504-a", loginAt); err != nil {
		t.Fatalf("login 504: %v", err)
	}
	if err := s.Logout(ctx, account, serverA, 504, "sess-504-a", model.SessionEndReason("banana"), logoutAt); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("logout invalid reason = %v, want ErrInvalid", err)
	}
	// The rejected logout left the session open; close it cleanly.
	if err := s.Logout(ctx, account, serverA, 504, "sess-504-a", model.SessionEndNormal, logoutAt); err != nil {
		t.Fatalf("logout 504 after invalid reason: %v", err)
	}

	// Session history: newest first, with the end reason recorded.
	history, err := s.ListPlayerSessionsByCharacter(ctx, 501, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(history) != 1 || history[0].SessionID != "sess-501-a" || history[0].EndReason != model.SessionEndNormal {
		t.Errorf("session history = %+v, want [sess-501-a normal]", history)
	}

	// Re-login after logout opens a fresh session; the history grows.
	if _, err := s.Login(ctx, account, serverA, 501, "sess-501-c", loginAt.Add(3*time.Millisecond)); err != nil {
		t.Fatalf("re-login after logout: %v", err)
	}
	history, err = s.ListPlayerSessionsByCharacter(ctx, 501, 10)
	if err != nil {
		t.Fatalf("list sessions after re-login: %v", err)
	}
	if len(history) != 2 || history[0].SessionID != "sess-501-c" || history[1].SessionID != "sess-501-a" {
		t.Errorf("session history after re-login = %+v, want [sess-501-c, sess-501-a]", history)
	}

	// Presence is preserved by the projection upsert: a stale
	// character.created replay must not flip an online character unknown.
	if err := s.UpsertCharacter(ctx, mk(serverA, 501, "presence-one-renamed")); err != nil {
		t.Fatalf("upsert overwrite while online: %v", err)
	}
	got, err = s.GetCharacter(ctx, account, serverA, 501)
	if err != nil {
		t.Fatalf("get after overwrite: %v", err)
	}
	if got.PresenceStatus != model.PresenceOnline {
		t.Errorf("presence after upsert overwrite = %q, want online (presence preserved)", got.PresenceStatus)
	}

	// Online roster: count + list on a server.
	// serverA: 501 (online), 502 (offline) → 1 online.
	// serverB: 503 (unknown) → 0 online.
	n, err := s.CountOnlineByServer(ctx, serverA)
	if err != nil {
		t.Fatalf("count online: %v", err)
	}
	if n != 1 {
		t.Errorf("count online serverA = %d, want 1", n)
	}
	online, nextCursor, err := s.ListOnlineByServer(ctx, serverA, 50, "")
	if err != nil {
		t.Fatalf("list online: %v", err)
	}
	if len(online) != 1 || online[0].CharacterID != 501 {
		t.Errorf("list online serverA = %+v, want [501]", online)
	}
	// The cursor is a resume token (last returned character_id), not a
	// "more rows" flag: it is populated even on the final page.
	if nextCursor != "501" {
		t.Errorf("next_cursor = %q, want 501 (resume token)", nextCursor)
	}
	if more, _, _ := s.ListOnlineByServer(ctx, serverA, 50, nextCursor); len(more) != 0 {
		t.Errorf("list online after cursor = %+v, want empty", more)
	}
	if n, _ := s.CountOnlineByServer(ctx, serverB); n != 0 {
		t.Errorf("count online serverB = %d, want 0", n)
	}

	// Pagination cursor walks the roster by character_id.
	ch4 := mk(serverA, 505, "presence-four")
	ch5 := mk(serverA, 506, "presence-five")
	if err := s.UpsertCharacter(ctx, ch4); err != nil {
		t.Fatalf("upsert 505: %v", err)
	}
	if err := s.UpsertCharacter(ctx, ch5); err != nil {
		t.Fatalf("upsert 506: %v", err)
	}
	if _, err := s.Login(ctx, account, serverA, 505, "sess-505-a", loginAt); err != nil {
		t.Fatalf("login 505: %v", err)
	}
	if _, err := s.Login(ctx, account, serverA, 506, "sess-506-a", loginAt); err != nil {
		t.Fatalf("login 506: %v", err)
	}
	page1, cursor, err := s.ListOnlineByServer(ctx, serverA, 2, "")
	if err != nil {
		t.Fatalf("list online page1: %v", err)
	}
	if len(page1) != 2 || page1[0].CharacterID != 501 || page1[1].CharacterID != 505 {
		t.Errorf("page1 = %+v, want [501, 505]", page1)
	}
	if cursor == "" {
		t.Fatal("page1 cursor empty, want continuation")
	}
	page2, _, err := s.ListOnlineByServer(ctx, serverA, 2, cursor)
	if err != nil {
		t.Fatalf("list online page2: %v", err)
	}
	if len(page2) != 1 || page2[0].CharacterID != 506 {
		t.Errorf("page2 = %+v, want [506]", page2)
	}
}

// sameTimeT is the non-T variant of the contract's timestamp comparison.
func sameTimeT(got *time.Time, want time.Time) bool {
	if got == nil {
		return false
	}
	d := got.Sub(want)
	if d < 0 {
		d = -d
	}
	return d <= time.Microsecond
}
