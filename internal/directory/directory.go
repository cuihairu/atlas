// Package directory implements the character directory service described in
// docs/api.md §Directory.
package directory

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
)

// PresenceView is the 角色在线查询 response: the character row, its resolved
// presence status, and the open session (nil when offline).
type PresenceView struct {
	Character      *model.Character     `json:"character"`
	PresenceStatus model.PresenceStatus `json:"presence_status"`
	ActiveSession  *model.PlayerSession `json:"active_session,omitempty"`
}

// Service manages the character index (projection).
type Service struct {
	characters store.CharacterStore
	sessions   store.PlayerSessionStore
	metrics    *metrics.Metrics
}

// New creates a new directory service.
func New(characters store.CharacterStore) *Service {
	return &Service{characters: characters}
}

// WithPlayerSessions attaches the player-session store used by the presence
// login/logout state machine (docs/presence.md). Optional so stores that only
// implement CharacterStore (the sharded store) keep working; without it the
// directory falls back to the legacy last_login_at-only behavior.
func (s *Service) WithPlayerSessions(sessions store.PlayerSessionStore) *Service {
	s.sessions = sessions
	return s
}

// WithMetrics attaches Prometheus instrumentation (optional). Write-path
// operations are observed as atlas_directory_write_duration_seconds{op}.
func (s *Service) WithMetrics(m *metrics.Metrics) *Service {
	s.metrics = m
	return s
}

// CreateCharacter creates or updates a character index entry.
func (s *Service) CreateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, name string, level int, classID int) (*model.Character, error) {
	t0 := time.Now()
	defer func() { s.metrics.ObserveDirectoryWrite("create", time.Since(t0)) }()
	ctx, span := atlastracing.Start(ctx, "directory.create")
	defer span.End()
	if accountID <= 0 {
		return nil, fmt.Errorf("%w: account_id must be > 0", model.ErrInvalid)
	}
	if serverID == "" {
		return nil, fmt.Errorf("%w: server_id is required", model.ErrInvalid)
	}
	if characterID <= 0 {
		return nil, fmt.Errorf("%w: character_id must be > 0", model.ErrInvalid)
	}

	now := time.Now()
	ch := &model.Character{
		AccountID:   accountID,
		ServerID:    serverID,
		CharacterID: characterID,
		Name:        name,
		Level:       level,
		ClassID:     classID,
		LastLoginAt: &now,
	}

	if err := s.characters.UpsertCharacter(ctx, ch); err != nil {
		return nil, fmt.Errorf("create character: %w", err)
	}
	return ch, nil
}

// UpdateCharacter applies a partial patch to a character index entry.
func (s *Service) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) (*model.Character, error) {
	t0 := time.Now()
	defer func() { s.metrics.ObserveDirectoryWrite("update", time.Since(t0)) }()
	ctx, span := atlastracing.Start(ctx, "directory.update")
	defer span.End()
	if err := s.characters.UpdateCharacter(ctx, accountID, serverID, characterID, patch); err != nil {
		return nil, fmt.Errorf("update character: %w", err)
	}
	return s.characters.GetCharacter(ctx, accountID, serverID, characterID)
}

// DeleteCharacter removes a character index entry.
func (s *Service) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	t0 := time.Now()
	defer func() { s.metrics.ObserveDirectoryWrite("delete", time.Since(t0)) }()
	ctx, span := atlastracing.Start(ctx, "directory.delete")
	defer span.End()
	if err := s.characters.DeleteCharacter(ctx, accountID, serverID, characterID); err != nil {
		return fmt.Errorf("delete character: %w", err)
	}
	return nil
}

// GetCharacter returns a character by composite key.
func (s *Service) GetCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	ctx, span := atlastracing.Start(ctx, "directory.get")
	defer span.End()
	ch, err := s.characters.GetCharacter(ctx, accountID, serverID, characterID)
	if err != nil {
		return nil, fmt.Errorf("get character: %w", err)
	}
	return ch, nil
}

// GetCharacterByCharacterID returns a character by its global character ID.
func (s *Service) GetCharacterByCharacterID(ctx context.Context, characterID int64) (*model.Character, error) {
	ctx, span := atlastracing.Start(ctx, "directory.get_by_character_id")
	defer span.End()
	ch, err := s.characters.GetCharacterByCharacterID(ctx, characterID)
	if err != nil {
		return nil, fmt.Errorf("get character by id: %w", err)
	}
	return ch, nil
}

// ListByAccount returns all characters belonging to an account.
func (s *Service) ListByAccount(ctx context.Context, accountID int64) ([]*model.Character, error) {
	ctx, span := atlastracing.Start(ctx, "directory.list_by_account")
	defer span.End()
	chars, err := s.characters.ListCharactersByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("list by account: %w", err)
	}
	return chars, nil
}

// ListByServer returns characters on a given server with cursor pagination.
// Returns the characters and the cursor for the next page.
func (s *Service) ListByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, string, error) {
	ctx, span := atlastracing.Start(ctx, "directory.list_by_server")
	defer span.End()
	chars, err := s.characters.ListCharactersByServer(ctx, serverID, limit, cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list by server: %w", err)
	}

	// Compute next cursor from the last character ID.
	nextCursor := ""
	if len(chars) > 0 {
		nextCursor = strconv.FormatInt(chars[len(chars)-1].CharacterID, 10)
	}

	return chars, nextCursor, nil
}

// Presence query surface (PP-3, docs/presence.md §4). All read the player
// session store; a service without one returns ErrNotFound so callers can
// distinguish "offline / unknown" from "unsupported".

// GetPresence returns the character's presence state and, when online, its
// active session. Returns ErrNotFound when the character does not exist; when
// the character is offline the session is nil and presence_status is
// "offline".
func (s *Service) GetPresence(ctx context.Context, characterID int64) (*PresenceView, error) {
	ctx, span := atlastracing.Start(ctx, "directory.get_presence")
	defer span.End()
	if s.sessions == nil {
		return nil, fmt.Errorf("presence store not configured: %w", store.ErrNotFound)
	}
	ch, err := s.characters.GetCharacterByCharacterID(ctx, characterID)
	if err != nil {
		return nil, fmt.Errorf("get presence: %w", err)
	}
	sess, err := s.sessions.GetActivePlayerSession(ctx, characterID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("get presence: %w", err)
	}
	view := &PresenceView{
		Character:      ch,
		PresenceStatus: ch.PresenceStatus.Normalize(),
	}
	if sess != nil {
		view.ActiveSession = sess
	}
	return view, nil
}

// ListOnlineByServer returns the online characters on a server (某服在线名单)
// with cursor pagination, the "某服在线名单" query.
func (s *Service) ListOnlineByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, string, error) {
	ctx, span := atlastracing.Start(ctx, "directory.list_online_by_server")
	defer span.End()
	if s.sessions == nil {
		return nil, "", fmt.Errorf("presence store not configured: %w", store.ErrNotFound)
	}
	chars, nextCursor, err := s.sessions.ListOnlineByServer(ctx, serverID, limit, cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list online by server: %w", err)
	}
	return chars, nextCursor, nil
}

// CountOnlineByServer returns how many characters are online on a server.
func (s *Service) CountOnlineByServer(ctx context.Context, serverID string) (int, error) {
	ctx, span := atlastracing.Start(ctx, "directory.count_online_by_server")
	defer span.End()
	if s.sessions == nil {
		return 0, fmt.Errorf("presence store not configured: %w", store.ErrNotFound)
	}
	n, err := s.sessions.CountOnlineByServer(ctx, serverID)
	if err != nil {
		return 0, fmt.Errorf("count online by server: %w", err)
	}
	return n, nil
}

// GetPlayerSession returns a player session by ID, or ErrNotFound.
func (s *Service) GetPlayerSession(ctx context.Context, sessionID string) (*model.PlayerSession, error) {
	ctx, span := atlastracing.Start(ctx, "directory.get_player_session")
	defer span.End()
	if s.sessions == nil {
		return nil, fmt.Errorf("presence store not configured: %w", store.ErrNotFound)
	}
	sess, err := s.sessions.GetPlayerSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get player session: %w", err)
	}
	return sess, nil
}

// ListPlayerSessionsByCharacter returns a character's session history, newest
// first, capped at limit (<= 0 means no cap).
func (s *Service) ListPlayerSessionsByCharacter(ctx context.Context, characterID int64, limit int) ([]*model.PlayerSession, error) {
	ctx, span := atlastracing.Start(ctx, "directory.list_player_sessions")
	defer span.End()
	if s.sessions == nil {
		return nil, fmt.Errorf("presence store not configured: %w", store.ErrNotFound)
	}
	sessions, err := s.sessions.ListPlayerSessionsByCharacter(ctx, characterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list player sessions: %w", err)
	}
	return sessions, nil
}
