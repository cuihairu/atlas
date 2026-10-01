package sharded

import (
	"context"
	"fmt"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newSharded(t *testing.T, n int) *Store {
	t.Helper()
	shards := make([]store.CharacterStore, n)
	for i := range shards {
		shards[i] = memory.New()
	}
	return New(shards, store.HashShardStrategy{})
}

func putCharacter(t *testing.T, ctx context.Context, s store.CharacterStore, accountID int64, serverID string, characterID int64, name string, level int) {
	t.Helper()
	ch := &model.Character{
		AccountID:   accountID,
		ServerID:    serverID,
		CharacterID: characterID,
		Name:        name,
		Level:       level,
		ClassID:     1,
	}
	if err := s.UpsertCharacter(ctx, ch); err != nil {
		t.Fatalf("upsert character %d: %v", characterID, err)
	}
}

func TestHashShardStrategy_StableAndBounded(t *testing.T) {
	strat := store.HashShardStrategy{}
	for _, n := range []int{1, 2, 3, 7, 16} {
		seen := map[int]int{}
		for id := int64(1); id <= 1000; id++ {
			idx := strat.ShardForAccount(id, n)
			if idx < 0 || idx >= n {
				t.Fatalf("shard index %d out of range [0,%d) for account %d", idx, n, id)
			}
			seen[idx]++
		}
		// With 1000 accounts and n <= 16, every shard must receive a non-trivial
		// share (deterministic hash, so no flakiness).
		minShare := 1000 / n / 4
		for idx, count := range seen {
			if count < minShare {
				t.Errorf("n=%d shard %d under-loaded: %d < %d", n, idx, count, minShare)
			}
		}
	}
	if strat.ShardForAccount(42, 1) != 0 {
		t.Error("single shard must always map to index 0")
	}
	if strat.ShardForAccount(42, 3) != strat.ShardForAccount(42, 3) {
		t.Error("strategy must be deterministic")
	}
}

func TestSharded_KeyRouting(t *testing.T) {
	ctx := context.Background()
	s := newSharded(t, 3)

	putCharacter(t, ctx, s, 1, "srv-1", 101, "alpha", 10)
	putCharacter(t, ctx, s, 2, "srv-1", 102, "beta", 20)
	putCharacter(t, ctx, s, 3, "srv-2", 103, "gamma", 30)

	// Each account routes to exactly one shard; verify the routing is
	// consistent by checking the per-shard distribution directly below.

	// Exactly one shard holds each character.
	total := 0
	for i, sh := range s.Shards() {
		page, _, err := sh.SearchCharacters(ctx, store.CharacterSearchFilter{})
		if err != nil {
			t.Fatalf("shard %d: %v", i, err)
		}
		total += len(page)
	}
	if total != 3 {
		t.Errorf("expected 3 characters across shards, got %d", total)
	}

	// Reads route by account and return the stored row.
	got, err := s.GetCharacter(ctx, 1, "srv-1", 101)
	if err != nil {
		t.Fatalf("GetCharacter: %v", err)
	}
	if got.Name != "alpha" {
		t.Errorf("unexpected name %q", got.Name)
	}

	// Patch routes to the owning shard.
	lvl := 99
	if err := s.UpdateCharacter(ctx, 1, "srv-1", 101, store.CharacterPatch{Level: &lvl}); err != nil {
		t.Fatalf("UpdateCharacter: %v", err)
	}
	got, _ = s.GetCharacter(ctx, 1, "srv-1", 101)
	if got.Level != 99 {
		t.Errorf("expected level 99, got %d", got.Level)
	}

	// Per-account listing hits only the owning shard.
	list, err := s.ListCharactersByAccount(ctx, 1)
	if err != nil {
		t.Fatalf("ListCharactersByAccount: %v", err)
	}
	if len(list) != 1 || list[0].CharacterID != 101 {
		t.Errorf("unexpected account listing: %+v", list)
	}

	// Delete routes to the owning shard.
	if err := s.DeleteCharacter(ctx, 1, "srv-1", 101); err != nil {
		t.Fatalf("DeleteCharacter: %v", err)
	}
	if _, err := s.GetCharacter(ctx, 1, "srv-1", 101); !store.IsNotFound(err) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSharded_GetByCharacterID_FanOut(t *testing.T) {
	ctx := context.Background()
	s := newSharded(t, 4)

	putCharacter(t, ctx, s, 7, "srv-1", 777, "lucky", 5)

	got, err := s.GetCharacterByCharacterID(ctx, 777)
	if err != nil {
		t.Fatalf("GetCharacterByCharacterID: %v", err)
	}
	if got.AccountID != 7 || got.Name != "lucky" {
		t.Errorf("unexpected character: %+v", got)
	}

	if _, err := s.GetCharacterByCharacterID(ctx, 999); !store.IsNotFound(err) {
		t.Errorf("expected ErrNotFound for missing id, got %v", err)
	}
}

func TestSharded_ListByServer_MergesShards(t *testing.T) {
	ctx := context.Background()
	s := newSharded(t, 3)

	// Spread one server's characters across accounts so they land in
	// different shards, interleaved IDs to exercise the merge order.
	ids := []int64{105, 101, 103, 102, 104}
	for i, cid := range ids {
		putCharacter(t, ctx, s, int64(i+1), "srv-1", cid, fmt.Sprintf("c%d", cid), i)
	}

	list, err := s.ListCharactersByServer(ctx, "srv-1", 0, "")
	if err != nil {
		t.Fatalf("ListCharactersByServer: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("expected 5 merged characters, got %d", len(list))
	}
	for i, ch := range list {
		if ch.CharacterID != int64(i+101) {
			t.Fatalf("merged order broken at %d: got character %d", i, ch.CharacterID)
		}
	}

	// Cursor pagination across the merge.
	page1, err := s.ListCharactersByServer(ctx, "srv-1", 2, "")
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 || page1[0].CharacterID != 101 || page1[1].CharacterID != 102 {
		t.Fatalf("unexpected page 1: %+v", page1)
	}
	page2, err := s.ListCharactersByServer(ctx, "srv-1", 2, fmt.Sprint(page1[len(page1)-1].CharacterID))
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 2 || page2[0].CharacterID != 103 || page2[1].CharacterID != 104 {
		t.Fatalf("unexpected page 2: %+v", page2)
	}
}

func TestSharded_SearchCharacters_CrossShard(t *testing.T) {
	ctx := context.Background()
	s := newSharded(t, 3)

	// 9 characters named after their ID, spread across accounts/servers so all
	// three shards hold a subset; several match level >= 50.
	want := 0
	for i := int64(1); i <= 9; i++ {
		level := 10 * int(i) // 10..90; >= 50 for ids 5..9
		if level >= 50 {
			want++
		}
		putCharacter(t, ctx, s, i, fmt.Sprintf("srv-%d", i%2), 100+i, fmt.Sprintf("hero%d", i), level)
	}

	filter := store.CharacterSearchFilter{MinLevel: &[]int{50}[0], Limit: 4}
	var all []*model.Character
	for {
		page, next, err := s.SearchCharacters(ctx, filter)
		if err != nil {
			t.Fatalf("SearchCharacters: %v", err)
		}
		all = append(all, page...)
		if next == "" {
			break
		}
		filter.Cursor = next
	}

	if len(all) != want {
		t.Fatalf("expected %d matches across shards, got %d", want, len(all))
	}
	// Global order must hold across the merge.
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if prev.ServerID > cur.ServerID || (prev.ServerID == cur.ServerID && prev.CharacterID >= cur.CharacterID) {
			t.Fatalf("merge order broken at %d: %s:%d then %s:%d", i, prev.ServerID, prev.CharacterID, cur.ServerID, cur.CharacterID)
		}
	}

	// Single page truncated at limit reports the right cursor.
	filter.Cursor = ""
	page, next, err := s.SearchCharacters(ctx, filter)
	if err != nil {
		t.Fatalf("single page: %v", err)
	}
	if len(page) != 4 || next == "" {
		t.Fatalf("expected 4 rows + cursor, got %d rows cursor %q", len(page), next)
	}
	last := page[len(page)-1]
	if next != fmt.Sprintf("%s:%d", last.ServerID, last.CharacterID) {
		t.Errorf("cursor %q does not match last row %s:%d", next, last.ServerID, last.CharacterID)
	}
}

func TestReshard_SingleToSharded(t *testing.T) {
	ctx := context.Background()

	src := memory.New()
	for i := int64(1); i <= 25; i++ {
		putCharacter(t, ctx, src, i, fmt.Sprintf("srv-%d", i%3), 1000+i, fmt.Sprintf("r%d", i), int(i))
	}

	dst := newSharded(t, 3)

	n, err := Reshard(ctx, src, dst, 10)
	if err != nil {
		t.Fatalf("Reshard: %v", err)
	}
	if n != 25 {
		t.Fatalf("expected 25 copied, got %d", n)
	}

	// Every character must be present at the destination via fan-out reads.
	for i := int64(1); i <= 25; i++ {
		if _, err := dst.GetCharacterByCharacterID(ctx, 1000+i); err != nil {
			t.Errorf("character %d missing after reshard: %v", 1000+i, err)
		}
	}

	// Idempotent: re-running repairs rather than duplicates.
	n2, err := Reshard(ctx, src, dst, 10)
	if err != nil || n2 != 25 {
		t.Fatalf("re-reshard: got %d, %v", n2, err)
	}
	total := 0
	for _, sh := range dst.Shards() {
		page, _, err := sh.SearchCharacters(ctx, store.CharacterSearchFilter{Limit: 200})
		if err != nil {
			t.Fatalf("count shard: %v", err)
		}
		total += len(page)
	}
	if total != 25 {
		t.Errorf("expected 25 rows after re-reshard, got %d", total)
	}
}

func TestReshard_ShardedRescale(t *testing.T) {
	ctx := context.Background()

	// 3 shards → 2 shards (shrink) with a different key layout.
	oldStore := newSharded(t, 3)
	for i := int64(1); i <= 40; i++ {
		putCharacter(t, ctx, oldStore, i, "srv-1", i, fmt.Sprintf("x%d", i), int(i)%80)
	}

	newStore := newSharded(t, 2)
	n, err := Reshard(ctx, oldStore, newStore, 7)
	if err != nil {
		t.Fatalf("Reshard: %v", err)
	}
	if n != 40 {
		t.Fatalf("expected 40 copied, got %d", n)
	}

	// Spot-check both routes: composite-key read and global-ID read.
	for _, id := range []int64{1, 17, 40} {
		got, err := newStore.GetCharacter(ctx, id, "srv-1", id)
		if err != nil {
			t.Errorf("character %d: %v", id, err)
			continue
		}
		if got.CharacterID != id {
			t.Errorf("character %d corrupted: %+v", id, got)
		}
	}
}

func TestNew_RejectsBadConstruction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on zero shards")
		}
	}()
	New(nil, store.HashShardStrategy{})
}
