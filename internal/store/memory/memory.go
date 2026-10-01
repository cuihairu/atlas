// Package memory provides a pure-Go, in-memory implementation of store.Store.
//
// It is safe for concurrent use. This implementation requires no external
// dependencies and is useful for testing and development.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// compile-time interface check
var _ store.Store = (*Store)(nil)

// Store is an in-memory implementation of store.Store.
type Store struct {
	mu         sync.RWMutex
	servers    map[string]*model.Server
	characters map[string]*model.Character // key: "accountID:serverID:characterID"
	runtimes   map[string]model.Runtime
}

// New creates a new in-memory store.
func New() *Store {
	return &Store{
		servers:    make(map[string]*model.Server),
		characters: make(map[string]*model.Character),
		runtimes:   make(map[string]model.Runtime),
	}
}

// Ping always succeeds for the in-memory store.
func (s *Store) Ping(_ context.Context) error {
	return nil
}

// Close is a no-op for the in-memory store.
func (s *Store) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// ServerStore
// ---------------------------------------------------------------------------

func (s *Store) RegisterServer(_ context.Context, srv *model.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if existing, ok := s.servers[srv.ID]; ok {
		// Idempotent update: overwrite mutable fields.
		existing.Name = srv.Name
		existing.Type = srv.Type
		existing.Region = srv.Region
		existing.RealmID = srv.RealmID
		existing.ShardID = srv.ShardID
		existing.Version = srv.Version
		existing.Platform = srv.Platform
		existing.Endpoint = srv.Endpoint
		existing.Capacity = srv.Capacity
		if srv.Status != "" {
			existing.Status = srv.Status
		}
		existing.UpdatedAt = now
		// Copy timestamps back so caller sees the stored record.
		*srv = *existing
		return nil
	}

	if srv.Status == "" {
		srv.Status = model.StatusStarting
	}
	srv.CreatedAt = now
	srv.UpdatedAt = now

	// Store a copy to prevent mutation.
	cp := *srv
	s.servers[srv.ID] = &cp
	return nil
}

func (s *Store) GetServer(_ context.Context, id string) (*model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	srv, ok := s.servers[id]
	if !ok {
		return nil, fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	cp := *srv
	return &cp, nil
}

func (s *Store) ListServers(_ context.Context, f store.ServerFilter) ([]*model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Collect matching servers.
	var result []*model.Server
	for _, srv := range s.servers {
		if !matchServer(srv, f) {
			continue
		}
		cp := *srv
		result = append(result, &cp)
	}

	// Sort by ID for deterministic pagination.
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	// Apply cursor: skip entries with ID <= cursor.
	if f.Cursor != "" {
		start := 0
		for i, srv := range result {
			if srv.ID > f.Cursor {
				start = i
				break
			}
			start = i + 1
		}
		result = result[start:]
	}

	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) UpdateServerStatus(_ context.Context, id string, status model.ServerStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	srv, ok := s.servers[id]
	if !ok {
		return fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	srv.Status = status
	srv.UpdatedAt = time.Now()
	return nil
}

func (s *Store) DeleteServer(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.servers[id]; !ok {
		return fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	delete(s.servers, id)
	return nil
}

// ---------------------------------------------------------------------------
// CharacterStore
// ---------------------------------------------------------------------------

func charKey(accountID int64, serverID string, characterID int64) string {
	return fmt.Sprintf("%d:%s:%d", accountID, serverID, characterID)
}

func (s *Store) UpsertCharacter(_ context.Context, ch *model.Character) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := charKey(ch.AccountID, ch.ServerID, ch.CharacterID)
	now := time.Now()

	if existing, ok := s.characters[key]; ok {
		existing.Name = ch.Name
		existing.Level = ch.Level
		existing.ClassID = ch.ClassID
		existing.Avatar = ch.Avatar
		existing.LastLoginAt = ch.LastLoginAt
		existing.UpdatedAt = now
		*ch = *existing
		return nil
	}

	ch.CreatedAt = now
	ch.UpdatedAt = now
	cp := *ch
	s.characters[key] = &cp
	return nil
}

func (s *Store) GetCharacter(_ context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := charKey(accountID, serverID, characterID)
	ch, ok := s.characters[key]
	if !ok {
		return nil, fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	cp := *ch
	return &cp, nil
}

func (s *Store) GetCharacterByCharacterID(_ context.Context, characterID int64) (*model.Character, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.characters {
		if ch.CharacterID == characterID {
			cp := *ch
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("character_id %d: %w", characterID, store.ErrNotFound)
}

func (s *Store) UpdateCharacter(_ context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := charKey(accountID, serverID, characterID)
	ch, ok := s.characters[key]
	if !ok {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}

	if patch.Name != nil {
		ch.Name = *patch.Name
	}
	if patch.Level != nil {
		ch.Level = *patch.Level
	}
	if patch.ClassID != nil {
		ch.ClassID = *patch.ClassID
	}
	if patch.Avatar != nil {
		ch.Avatar = *patch.Avatar
	}
	if patch.LastLoginAt != nil {
		ch.LastLoginAt = patch.LastLoginAt
	}
	ch.UpdatedAt = time.Now()
	return nil
}

func (s *Store) DeleteCharacter(_ context.Context, accountID int64, serverID string, characterID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := charKey(accountID, serverID, characterID)
	if _, ok := s.characters[key]; !ok {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	delete(s.characters, key)
	return nil
}

func (s *Store) ListCharactersByAccount(_ context.Context, accountID int64) ([]*model.Character, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := fmt.Sprintf("%d:", accountID)
	var result []*model.Character
	for key, ch := range s.characters {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			cp := *ch
			result = append(result, &cp)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].ServerID != result[j].ServerID {
			return result[i].ServerID < result[j].ServerID
		}
		return result[i].CharacterID < result[j].CharacterID
	})

	return result, nil
}

func (s *Store) ListCharactersByServer(_ context.Context, serverID string, limit int, cursor string) ([]*model.Character, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Collect characters for this server.
	var result []*model.Character
	for _, ch := range s.characters {
		if ch.ServerID == serverID {
			cp := *ch
			result = append(result, &cp)
		}
	}

	// Sort by character ID for stable pagination.
	sort.Slice(result, func(i, j int) bool {
		return result[i].CharacterID < result[j].CharacterID
	})

	// Apply cursor.
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

	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// RuntimeStore
// ---------------------------------------------------------------------------

func (s *Store) RecordHeartbeat(_ context.Context, id string, hb model.Heartbeat) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtimes[id] = model.Runtime{
		Status:     hb.Status,
		Players:    hb.Players,
		Load:       hb.Load,
		LastSeenAt: time.Now(),
	}
	return nil
}

func (s *Store) GetRuntime(_ context.Context, id string) (*model.Runtime, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rt, ok := s.runtimes[id]
	if !ok {
		return nil, fmt.Errorf("runtime %s: %w", id, store.ErrNotFound)
	}
	return &rt, nil
}

func (s *Store) DeleteRuntime(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.runtimes[id]; !ok {
		return fmt.Errorf("runtime %s: %w", id, store.ErrNotFound)
	}
	delete(s.runtimes, id)
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func matchServer(srv *model.Server, f store.ServerFilter) bool {
	if f.Region != "" && srv.Region != f.Region {
		return false
	}
	if f.Realm != "" {
		if srv.RealmID == nil || *srv.RealmID != f.Realm {
			return false
		}
	}
	if f.Shard != "" {
		if srv.ShardID == nil || *srv.ShardID != f.Shard {
			return false
		}
	}
	if f.Version != "" && srv.Version != f.Version {
		return false
	}
	if f.Platform != "" && srv.Platform != f.Platform {
		return false
	}
	if f.Status != "" && srv.Status != f.Status {
		return false
	}
	return true
}