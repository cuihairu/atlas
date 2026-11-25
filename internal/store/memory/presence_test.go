package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Presence ops ride the instruction queue like every other write, but they
// must NOT be coalesced (a state machine is not last-writer-wins) and they
// must survive the capture-ring replay. The behavioral contract covers the
// state machine itself; these tests pin the queue integration.
func TestPresenceOpsNotCoalescedInOneBatch(t *testing.T) {
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertCharacter(ctx, &model.Character{
		AccountID: 1, ServerID: "srv-1", CharacterID: 7, Name: "p", Level: 1, ClassID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)

	// A batch of login → logout → login on the same character: none of the
	// three coalesce. A merge here would drop the open/close ordering and
	// leave the character online with a stale session (or vice versa).
	s.applyBatchN([]*instruction{
		{kind: opPlayerLogin, target: charKey(1, "srv-1", 7),
			session:    &model.PlayerSession{SessionID: "s1", AccountID: 1, CharacterID: 7, ServerID: "srv-1", StartedAt: at},
			presenceAt: at, out: make(chan instructionResult, 1)},
		{kind: opPlayerLogout, target: charKey(1, "srv-1", 7),
			sessionID: "s1", endReason: model.SessionEndNormal, presenceAt: at, out: make(chan instructionResult, 1)},
		{kind: opPlayerLogin, target: charKey(1, "srv-1", 7),
			session:    &model.PlayerSession{SessionID: "s2", AccountID: 1, CharacterID: 7, ServerID: "srv-1", StartedAt: at},
			presenceAt: at, out: make(chan instructionResult, 1)},
	})
	// Three applied, zero merged.
	if stats := s.QueueStats(); stats.Merged != 0 {
		t.Errorf("merged = %d, want 0 (presence ops must not coalesce)", stats.Merged)
	}

	ch, err := s.GetCharacter(ctx, 1, "srv-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if ch.PresenceStatus != model.PresenceOnline {
		t.Errorf("presence = %q, want online (login survived after logout)", ch.PresenceStatus)
	}
	active, err := s.GetActivePlayerSession(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if active.SessionID != "s2" {
		t.Errorf("active session = %q, want s2", active.SessionID)
	}
	history, err := s.ListPlayerSessionsByCharacter(ctx, 7, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].SessionID != "s2" || history[1].SessionID != "s1" {
		t.Errorf("history = %+v, want [s2 open, s1 closed]", history)
	}
}

// The capture ring must reproduce the session history and presence through
// ReplayInto — incident replay is how an incident is rebuilt, so presence has to
// ride the same capture path as the other writes.
func TestPresenceSurvivesCaptureReplay(t *testing.T) {
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertCharacter(ctx, &model.Character{
		AccountID: 1, ServerID: "srv-1", CharacterID: 7, Name: "p", Level: 1, ClassID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := s.Login(ctx, 1, "srv-1", 7, "s1", at); err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(ctx, 1, "srv-1", 7, "s1", model.SessionEndNormal, at.Add(1*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, 1, "srv-1", 7, "s2", at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}

	sandbox := New()
	defer sandbox.Close()
	if err := ReplayInto(sandbox, s); err != nil {
		t.Fatalf("replay: %v", err)
	}

	ch, err := sandbox.GetCharacter(ctx, 1, "srv-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if ch.PresenceStatus != model.PresenceOnline {
		t.Errorf("replayed presence = %q, want online", ch.PresenceStatus)
	}
	active, err := sandbox.GetActivePlayerSession(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if active.SessionID != "s2" {
		t.Errorf("replayed active session = %q, want s2", active.SessionID)
	}
	history, err := sandbox.ListPlayerSessionsByCharacter(ctx, 7, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].SessionID != "s2" || history[1].SessionID != "s1" {
		t.Errorf("replayed history = %+v, want [s2 open, s1 closed]", history)
	}
	if history[1].EndReason != model.SessionEndNormal || history[1].EndedAt == nil {
		t.Errorf("replayed s1 = %+v, want ended with reason normal", history[1])
	}
}

// Presence ops run on control lanes, never the hot lane: a heartbeat burst
// must not delay a login, and a login must not be starved behind the
// heartbeat backlog.
func TestPresenceOpsAreControlLane(t *testing.T) {
	for k := opPlayerLogin; k <= opPlayerLogout; k++ {
		if k.hot() {
			t.Errorf("%s is hot; presence ops belong on control lanes", k)
		}
	}
	// And a login against a never-seen character must not corrupt state.
	s := New()
	defer s.Close()
	if _, err := s.Login(context.Background(), 1, "srv-x", 7, "s1", time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("login unknown character = %v, want ErrNotFound", err)
	}
}
