// Package sharded implements horizontal partitioning of the character index
// (TODO v0.1.16): N backing CharacterStore shards with key-based routing by
// account, plus cross-shard fan-out for the queries that cannot be routed
// (global character-ID lookup, per-server listing, admin search).
//
// Key-routed operations (every write, and reads by composite key or account)
// cost exactly one shard hop. Fan-out operations query every shard and merge;
// their cost grows linearly with the shard count, which is the accepted
// trade-off — writes and per-account reads are the hot path.
package sharded

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// compile-time interface check
var _ store.CharacterStore = (*Store)(nil)
var _ store.PlayerSessionStore = (*Store)(nil)

// ps unwraps a backing shard as a PlayerSessionStore. Every shard Atlas
// builds (memory / PostgreSQL / MySQL) implements it; a shard that does not
// is a wiring bug, reported as an error rather than a silent no-op.
func (s *Store) ps(sh store.CharacterStore) (store.PlayerSessionStore, error) {
	ps, ok := sh.(store.PlayerSessionStore)
	if !ok {
		return nil, fmt.Errorf("sharded: shard %T does not implement PlayerSessionStore", sh)
	}
	return ps, nil
}

// Store is a character index spread across N shards. All shards must be
// non-nil and independent; routing is delegated to the strategy.
type Store struct {
	shards   []store.CharacterStore
	strategy store.CharacterShardStrategy
}

// New builds a sharded character store. Panics without shards — a zero-shard
// index is always a construction bug.
func New(shards []store.CharacterStore, strategy store.CharacterShardStrategy) *Store {
	if len(shards) == 0 {
		panic("sharded: at least one shard is required")
	}
	if strategy == nil {
		strategy = store.HashShardStrategy{}
	}
	for i, sh := range shards {
		if sh == nil {
			panic(fmt.Sprintf("sharded: shard %d is nil", i))
		}
	}
	return &Store{shards: shards, strategy: strategy}
}

// Shards exposes the backing shards (used by tests and the resharding tool).
func (s *Store) Shards() []store.CharacterStore { return s.shards }

// ShardFor returns the shard that owns the given account.
func (s *Store) ShardFor(accountID int64) store.CharacterStore {
	return s.shards[s.strategy.ShardForAccount(accountID, len(s.shards))]
}

// ---------------------------------------------------------------------------
// Key-routed operations — exactly one shard hop
// ---------------------------------------------------------------------------

func (s *Store) UpsertCharacter(ctx context.Context, ch *model.Character) error {
	return s.ShardFor(ch.AccountID).UpsertCharacter(ctx, ch)
}

func (s *Store) GetCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	return s.ShardFor(accountID).GetCharacter(ctx, accountID, serverID, characterID)
}

func (s *Store) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	return s.ShardFor(accountID).UpdateCharacter(ctx, accountID, serverID, characterID, patch)
}

func (s *Store) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	return s.ShardFor(accountID).DeleteCharacter(ctx, accountID, serverID, characterID)
}

func (s *Store) ListCharactersByAccount(ctx context.Context, accountID int64) ([]*model.Character, error) {
	return s.ShardFor(accountID).ListCharactersByAccount(ctx, accountID)
}

// ---------------------------------------------------------------------------
// Fan-out operations — every shard, merged
// ---------------------------------------------------------------------------

// GetCharacterByCharacterID probes shard by shard until the global character
// ID is found. There is no account to route by, so worst case touches every
// shard; expected case for a random hit is half.
func (s *Store) GetCharacterByCharacterID(ctx context.Context, characterID int64) (*model.Character, error) {
	for _, sh := range s.shards {
		ch, err := sh.GetCharacterByCharacterID(ctx, characterID)
		if err == nil {
			return ch, nil
		}
		if !store.IsNotFound(err) {
			return nil, fmt.Errorf("get character by id %d: %w", characterID, err)
		}
	}
	return nil, fmt.Errorf("character_id %d: %w", characterID, store.ErrNotFound)
}

// ListCharactersByServer merges every shard's page for the server. The cursor
// (a character ID) is passed through to each shard unchanged — it is a global
// ordering key, so every shard correctly resumes past it.
func (s *Store) ListCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	pages := make([][]*model.Character, 0, len(s.shards))
	for _, sh := range s.shards {
		page, err := sh.ListCharactersByServer(ctx, serverID, limit, cursor)
		if err != nil {
			return nil, fmt.Errorf("list characters by server %s: %w", serverID, err)
		}
		pages = append(pages, page)
	}

	merged := merge(pages, func(a, b *model.Character) bool { return a.CharacterID < b.CharacterID })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// CountByServerPresence sums the count over every shard: a server's characters
// are scattered across shards, so the true online total is the sum — never a
// single shard's number.
func (s *Store) CountByServerPresence(ctx context.Context, serverID string, presence model.PresenceStatus) (int64, error) {
	var n int64
	for _, sh := range s.shards {
		part, err := sh.CountByServerPresence(ctx, serverID, presence)
		if err != nil {
			return 0, fmt.Errorf("count characters by server presence %s: %w", serverID, err)
		}
		n += part
	}
	return n, nil
}

// SearchCharacters fans the filter out to every shard and merges the pages by
// the global cursor order (server_id, character_id). Passing the cursor and
// limit to each shard is correct because an item's rank within its shard is
// never worse than its global rank — the global first `limit` items always
// appear within each shard's own first `limit` items.
func (s *Store) SearchCharacters(ctx context.Context, filter store.CharacterSearchFilter) ([]*model.Character, string, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}

	pages := make([][]*model.Character, 0, len(s.shards))
	for _, sh := range s.shards {
		page, _, err := sh.SearchCharacters(ctx, filter)
		if err != nil {
			return nil, "", fmt.Errorf("search characters: %w", err)
		}
		pages = append(pages, page)
	}

	merged := merge(pages, func(a, b *model.Character) bool {
		if a.ServerID != b.ServerID {
			return a.ServerID < b.ServerID
		}
		return a.CharacterID < b.CharacterID
	})

	nextCursor := ""
	if len(merged) > filter.Limit {
		merged = merged[:filter.Limit]
	}
	if len(merged) > 0 {
		last := merged[len(merged)-1]
		nextCursor = last.ServerID + ":" + fmt.Sprint(last.CharacterID)
	}
	return merged, nextCursor, nil
}

// merge concatenates sorted pages and re-sorts. Each page is bounded by the
// query limit (<= 200), so concat + sort beats a k-way heap at these sizes.
func merge(pages [][]*model.Character, less func(a, b *model.Character) bool) []*model.Character {
	total := 0
	for _, p := range pages {
		total += len(p)
	}
	merged := make([]*model.Character, 0, total)
	for _, p := range pages {
		merged = append(merged, p...)
	}
	sort.Slice(merged, func(i, j int) bool { return less(merged[i], merged[j]) })
	return merged
}

// ---------------------------------------------------------------------------
// PlayerSessionStore — sessions co-locate with their character's shard
// ---------------------------------------------------------------------------

// CreateSession routes by the session's account: the session lands on the
// same shard as its character's index row, so the presence update is a
// single-shard hop.
func (s *Store) CreateSession(ctx context.Context, sess *model.PlayerSession) error {
	ps, err := s.ps(s.ShardFor(sess.AccountID))
	if err != nil {
		return err
	}
	return ps.CreateSession(ctx, sess)
}

// EndSession probes shard by shard: only the shard holding the session
// succeeds, every other reports ErrNotFound. A non-not-found error (e.g. the
// conflict raised by the shard that actually holds it) is returned as-is.
func (s *Store) EndSession(ctx context.Context, sessionID string, endedAt time.Time, reason model.SessionEndReason) (*model.PlayerSession, error) {
	for _, sh := range s.shards {
		ps, err := s.ps(sh)
		if err != nil {
			return nil, err
		}
		sess, err := ps.EndSession(ctx, sessionID, endedAt, reason)
		if err == nil {
			return sess, nil
		}
		if !store.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("session %s: %w", sessionID, store.ErrNotFound)
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (*model.PlayerSession, error) {
	for _, sh := range s.shards {
		ps, err := s.ps(sh)
		if err != nil {
			return nil, err
		}
		sess, err := ps.GetSession(ctx, sessionID)
		if err == nil {
			return sess, nil
		}
		if !store.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("session %s: %w", sessionID, store.ErrNotFound)
}

func (s *Store) GetActiveSessionByCharacter(ctx context.Context, characterID int64) (*model.PlayerSession, error) {
	for _, sh := range s.shards {
		ps, err := s.ps(sh)
		if err != nil {
			return nil, err
		}
		sess, err := ps.GetActiveSessionByCharacter(ctx, characterID)
		if err == nil {
			return sess, nil
		}
		if !store.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("character %d: %w", characterID, store.ErrNotFound)
}

// ListSessionsByCharacter fans out and merges by started_at descending. A
// character lives on exactly one shard, so only one shard contributes; the
// merge keeps the implementation correct even if a character were ever
// spread across shards.
func (s *Store) ListSessionsByCharacter(ctx context.Context, characterID int64, limit int) ([]*model.PlayerSession, error) {
	var all []*model.PlayerSession
	for _, sh := range s.shards {
		ps, err := s.ps(sh)
		if err != nil {
			return nil, err
		}
		sess, err := ps.ListSessionsByCharacter(ctx, characterID, limit)
		if err != nil {
			return nil, err
		}
		all = append(all, sess...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].StartedAt.After(all[j].StartedAt) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
