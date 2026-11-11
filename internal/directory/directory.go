// Package directory implements the character directory service described in
// docs/api.md §Directory.
package directory

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
)

// Service manages the character index (projection).
type Service struct {
	characters store.CharacterStore
	metrics    *metrics.Metrics
}

// New creates a new directory service.
func New(characters store.CharacterStore) *Service {
	return &Service{characters: characters}
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
