package memory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// PlayerSessionStore implementation (docs/presence.md §3). The state machine
// is the single source of truth across stores: Login opens a session AND flips
// the character online in one critical section; Logout closes the session AND
// flips it offline in one. Presence is only ever moved here — a generic
// CharacterPatch cannot turn a character online or offline, and the
// character.created/updated upserts never touch presence either.
//
// Invariant: a character is online ⟺ it has exactly one open session.
// Login and Logout preserve it by construction (both mutate the character row
// and the session state under the same charMu critical section, 拍板①).

func (s *Store) Login(ctx context.Context, accountID int64, serverID string, characterID int64, sessionID string, at time.Time) (*model.PlayerSession, error) {
	sess := &model.PlayerSession{
		SessionID:   sessionID,
		AccountID:   accountID,
		ServerID:    serverID,
		CharacterID: characterID,
		StartedAt:   at,
	}
	ins := &instruction{
		kind:        opPlayerLogin,
		target:      charKey(accountID, serverID, characterID),
		session:     sess,
		presenceAt:  at,
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	if err := s.submit(ctx, ins).err; err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) Logout(ctx context.Context, accountID int64, serverID string, characterID int64, sessionID string, reason model.SessionEndReason, endAt time.Time) error {
	ins := &instruction{
		kind:        opPlayerLogout,
		target:      charKey(accountID, serverID, characterID),
		sessionID:   sessionID,
		endReason:   reason,
		presenceAt:  endAt,
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) GetActivePlayerSession(_ context.Context, characterID int64) (*model.PlayerSession, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	id, ok := s.activeSession[characterID]
	if !ok {
		return nil, fmt.Errorf("character_id %d active session: %w", characterID, store.ErrNotFound)
	}
	cp := *s.sessions[id]
	return &cp, nil
}

func (s *Store) GetPlayerSession(_ context.Context, sessionID string) (*model.PlayerSession, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s: %w", sessionID, store.ErrNotFound)
	}
	cp := *sess
	return &cp, nil
}

func (s *Store) ListPlayerSessionsByCharacter(_ context.Context, characterID int64, limit int) ([]*model.PlayerSession, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	ids := s.sessionsByChar[characterID]
	var result []*model.PlayerSession
	// Newest first: walk the insertion-ordered history in reverse.
	for i := len(ids) - 1; i >= 0; i-- {
		sess, ok := s.sessions[ids[i]]
		if !ok {
			continue
		}
		cp := *sess
		result = append(result, &cp)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *Store) CountOnlineByServer(_ context.Context, serverID string) (int, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	n := 0
	for _, ch := range s.characters {
		if ch.ServerID == serverID && ch.PresenceStatus.Normalize() == model.PresenceOnline {
			n++
		}
	}
	return n, nil
}

func (s *Store) ListOnlineByServer(_ context.Context, serverID string, limit int, cursor string) ([]*model.Character, string, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var result []*model.Character
	for _, ch := range s.characters {
		if ch.ServerID != serverID || ch.PresenceStatus.Normalize() != model.PresenceOnline {
			continue
		}
		cp := *ch
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CharacterID < result[j].CharacterID })

	if cursor != "" {
		cursorID, err := strconv.ParseInt(cursor, 10, 64)
		if err == nil {
			start := 0
			for i, ch := range result {
				if ch.CharacterID > cursorID {
					start = i
					break
				}
				start = i + 1
			}
			result = result[start:]
		}
	}

	nextCursor := ""
	if len(result) > limit {
		result = result[:limit]
	}
	if len(result) > 0 {
		nextCursor = strconv.FormatInt(result[len(result)-1].CharacterID, 10)
	}
	return result, nextCursor, nil
}

// ── queue-side primitives (locks held by the applier) ──────────────────

// applyPlayerLogin opens the session and marks the character online. Two
// outcomes are not errors: a re-reported identical open session is an
// idempotent no-op (at-least-once transports re-deliver login events). A
// login while online in a DIFFERENT session is ErrConflict — the previous
// session must be closed first, so reconnect flows run Logout(crash) → Login.
func (s *Store) applyPlayerLogin(ins *instruction) error {
	ch, ok := s.characters[ins.target]
	if !ok {
		return fmt.Errorf("character %s: %w", ins.target, store.ErrNotFound)
	}
	sess := ins.session
	if sess.SessionID == "" {
		return fmt.Errorf("%w: session_id is required", store.ErrInvalid)
	}
	if sess.CharacterID <= 0 {
		return fmt.Errorf("%w: character_id must be > 0", store.ErrInvalid)
	}

	cur := ch.PresenceStatus.Normalize()
	if cur == model.PresenceOnline {
		if activeID, ok := s.activeSession[sess.CharacterID]; ok && activeID == sess.SessionID {
			// Idempotent re-report: return the stored open session.
			*sess = *cloneSession(s.sessions[activeID])
			return nil
		}
		return fmt.Errorf("character %s: already online in a different session: %w", ins.target, store.ErrConflict)
	}

	// Not online: open the session and flip presence in the same section.
	// Heal any stray open session (a move or delete left it behind) so the
	// online ⟺ open-session invariant cannot double-book a character.
	if activeID, ok := s.activeSession[sess.CharacterID]; ok {
		s.closeSessionLocked(activeID, model.SessionEndCrash, sess.StartedAt)
	}
	sess.EndedAt = nil
	sess.EndReason = ""
	stored := cloneSession(sess)
	s.sessions[sess.SessionID] = stored
	s.activeSession[sess.CharacterID] = sess.SessionID
	s.sessionsByChar[sess.CharacterID] = append(s.sessionsByChar[sess.CharacterID], sess.SessionID)

	ch.PresenceStatus = model.PresenceOnline
	ch.LastLoginAt = &sess.StartedAt
	ch.UpdatedAt = time.Now()
	*sess = *stored
	return nil
}

// applyPlayerLogout closes the open session and marks the character offline.
// A re-reported logout on an already-offline character is an idempotent no-op
// (duplicate delivery); a logout on a character that never logged in
// (unknown) is ErrInvalid.
func (s *Store) applyPlayerLogout(ins *instruction) error {
	ch, ok := s.characters[ins.target]
	if !ok {
		return fmt.Errorf("character %s: %w", ins.target, store.ErrNotFound)
	}
	if !ins.endReason.Valid() {
		return fmt.Errorf("%w: invalid end_reason %q", store.ErrInvalid, ins.endReason)
	}

	cur := ch.PresenceStatus.Normalize()
	activeID, hasActive := s.activeSession[ch.CharacterID]

	switch cur {
	case model.PresenceOnline:
		if hasActive {
			if ins.sessionID != "" && ins.sessionID != activeID {
				return fmt.Errorf("character %s: session %s is not the open session: %w", ins.target, ins.sessionID, store.ErrConflict)
			}
			s.closeSessionLocked(activeID, ins.endReason, ins.presenceAt)
		}
		// !hasActive here is an invariant break (online but no session) —
		// heal by recording the logout, never leave the character stuck online.
		ch.PresenceStatus = model.PresenceOffline
		ch.LastLogoutAt = &ins.presenceAt
		ch.UpdatedAt = time.Now()
		return nil

	case model.PresenceOffline:
		// Idempotent duplicate logout.
		if hasActive {
			// Invariant break (offline but a session is open): close it so the
			// history stays self-consistent rather than leaking an open row.
			s.closeSessionLocked(activeID, model.SessionEndCrash, ins.presenceAt)
		}
		return nil

	default: // PresenceUnknown — never logged in.
		return fmt.Errorf("character %s: logout with no open session: %w", ins.target, store.ErrInvalid)
	}
}

// closeSessionLocked ends a session and drops its active-session index entry.
// The caller holds charMu (the queue applier's critical section).
func (s *Store) closeSessionLocked(sessionID string, reason model.SessionEndReason, at time.Time) {
	sess, ok := s.sessions[sessionID]
	if !ok {
		return
	}
	ended := at
	sess.EndedAt = &ended
	sess.EndReason = reason
	delete(s.activeSession, sess.CharacterID)
}
