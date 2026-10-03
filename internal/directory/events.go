// Event application: the projection write path driven by character events
// (docs/sync.md §2/§6). All operations are idempotent — events may be
// re-delivered by at-least-once transports without side effects beyond a
// repeated upsert.
package directory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// ApplyEvent applies a character event to the index and returns the
// resulting projection row (nil for deleted events).
func (s *Service) ApplyEvent(ctx context.Context, e *event.Event) (*model.Character, error) {
	if e == nil {
		return nil, fmt.Errorf("%w: event is required", model.ErrInvalid)
	}

	t0 := time.Now()
	defer func() { s.metrics.ObserveDirectoryWrite(eventWriteOp(e.Type), time.Since(t0)) }()

	switch e.Type {
	case event.EventCharacterCreated:
		return s.applyCreated(ctx, e)
	case event.EventCharacterUpdated:
		return s.applyUpdated(ctx, e)
	case event.EventCharacterDeleted:
		return nil, s.applyDeleted(ctx, e)
	case event.EventCharacterLogin:
		return s.applyLogin(ctx, e)
	case event.EventCharacterMoved:
		return s.applyMoved(ctx, e)
	default:
		return nil, fmt.Errorf("%w: unknown event type %q", model.ErrInvalid, e.Type)
	}
}

// eventWriteOp renders an event type as the write-latency op label:
// "character.created" → "created". Every production write path (REST and
// gRPC alike) funnels through here, so these labels carry the real
// per-operation breakdown; the create/update/delete labels cover direct
// service-API writes.
func eventWriteOp(t event.EventType) string {
	if v, ok := strings.CutPrefix(string(t), "character."); ok {
		return v
	}
	return string(t)
}

// applyCreated upserts a full index row. Re-delivered created events are
// harmless upserts (docs/sync.md §6).
func (s *Service) applyCreated(ctx context.Context, e *event.Event) (*model.Character, error) {
	if e.AccountID <= 0 {
		return nil, fmt.Errorf("%w: account_id must be > 0", model.ErrInvalid)
	}
	if e.ServerID == "" {
		return nil, fmt.Errorf("%w: server_id is required", model.ErrInvalid)
	}
	if e.CharacterID <= 0 {
		return nil, fmt.Errorf("%w: character_id must be > 0", model.ErrInvalid)
	}

	now := time.Now()
	ch := &model.Character{
		AccountID:   e.AccountID,
		ServerID:    e.ServerID,
		CharacterID: e.CharacterID,
		Name:        e.Name,
		Level:       derefInt(e.Level),
		ClassID:     derefInt(e.ClassID),
		Avatar:      derefString(e.Avatar),
		Metadata:    derefMetadata(e.Metadata),
		LastLoginAt: &now,
	}
	if !e.Timestamp.IsZero() {
		ch.LastLoginAt = &e.Timestamp
	}

	if err := s.characters.UpsertCharacter(ctx, ch); err != nil {
		return nil, fmt.Errorf("create character: %w", err)
	}
	return ch, nil
}

// applyUpdated patches a partial set of fields, resolving the composite key
// from character_id when the event omits account/server.
func (s *Service) applyUpdated(ctx context.Context, e *event.Event) (*model.Character, error) {
	if e.CharacterID <= 0 {
		return nil, fmt.Errorf("%w: character_id must be > 0", model.ErrInvalid)
	}

	accountID, serverID := e.AccountID, e.ServerID
	if accountID <= 0 || serverID == "" {
		existing, err := s.characters.GetCharacterByCharacterID(ctx, e.CharacterID)
		if err != nil {
			return nil, fmt.Errorf("update character: %w", err)
		}
		accountID, serverID = existing.AccountID, existing.ServerID
	}

	patch := store.CharacterPatch{
		Level:    e.Level,
		ClassID:  e.ClassID,
		Avatar:   e.Avatar,
		Metadata: e.Metadata,
	}
	if e.Name != "" {
		patch.Name = &e.Name
	}
	if err := s.characters.UpdateCharacter(ctx, accountID, serverID, e.CharacterID, patch); err != nil {
		return nil, fmt.Errorf("update character: %w", err)
	}
	return s.characters.GetCharacter(ctx, accountID, serverID, e.CharacterID)
}

// applyDeleted removes an index row by composite key, resolving it from
// character_id when the event omits account/server.
func (s *Service) applyDeleted(ctx context.Context, e *event.Event) error {
	if e.CharacterID <= 0 {
		return fmt.Errorf("%w: character_id must be > 0", model.ErrInvalid)
	}

	accountID, serverID := e.AccountID, e.ServerID
	if accountID <= 0 || serverID == "" {
		existing, err := s.characters.GetCharacterByCharacterID(ctx, e.CharacterID)
		if err != nil {
			return fmt.Errorf("delete character: %w", err)
		}
		accountID, serverID = existing.AccountID, existing.ServerID
	}

	if err := s.characters.DeleteCharacter(ctx, accountID, serverID, e.CharacterID); err != nil {
		return fmt.Errorf("delete character: %w", err)
	}
	return nil
}

// applyLogin refreshes last_login_at without touching other fields.
func (s *Service) applyLogin(ctx context.Context, e *event.Event) (*model.Character, error) {
	if e.CharacterID <= 0 {
		return nil, fmt.Errorf("%w: character_id must be > 0", model.ErrInvalid)
	}

	accountID, serverID := e.AccountID, e.ServerID
	if accountID <= 0 || serverID == "" {
		existing, err := s.characters.GetCharacterByCharacterID(ctx, e.CharacterID)
		if err != nil {
			return nil, fmt.Errorf("login character: %w", err)
		}
		accountID, serverID = existing.AccountID, existing.ServerID
	}

	at := time.Now()
	if !e.Timestamp.IsZero() {
		at = e.Timestamp
	}
	patch := store.CharacterPatch{LastLoginAt: &at}
	if err := s.characters.UpdateCharacter(ctx, accountID, serverID, e.CharacterID, patch); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	return s.characters.GetCharacter(ctx, accountID, serverID, e.CharacterID)
}

// applyMoved re-homes a character to TargetServerID: upsert at the target
// first, then delete the old row — index completeness beats momentary
// duplication (docs/sync.md §7).
func (s *Service) applyMoved(ctx context.Context, e *event.Event) (*model.Character, error) {
	if e.CharacterID <= 0 {
		return nil, fmt.Errorf("%w: character_id must be > 0", model.ErrInvalid)
	}
	if e.TargetServerID == "" {
		return nil, fmt.Errorf("%w: target_server is required for moved events", model.ErrInvalid)
	}

	var src *model.Character
	if e.AccountID > 0 && e.ServerID != "" {
		src, _ = s.characters.GetCharacter(ctx, e.AccountID, e.ServerID, e.CharacterID)
	}
	if src == nil {
		var err error
		src, err = s.characters.GetCharacterByCharacterID(ctx, e.CharacterID)
		if err != nil {
			return nil, fmt.Errorf("move character: %w", err)
		}
	}

	dst := &model.Character{
		AccountID:   src.AccountID,
		ServerID:    e.TargetServerID,
		CharacterID: src.CharacterID,
		Name:        src.Name,
		Level:       src.Level,
		ClassID:     src.ClassID,
		Avatar:      src.Avatar,
		Metadata:    src.Metadata,
		LastLoginAt: src.LastLoginAt,
	}
	if err := s.characters.UpsertCharacter(ctx, dst); err != nil {
		return nil, fmt.Errorf("move character: %w", err)
	}
	if err := s.characters.DeleteCharacter(ctx, src.AccountID, src.ServerID, src.CharacterID); err != nil {
		return nil, fmt.Errorf("move character: %w", err)
	}
	return dst, nil
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefMetadata(m *map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return *m
}
