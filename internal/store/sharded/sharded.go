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

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// compile-time interface check
var _ store.CharacterStore = (*Store)(nil)

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
