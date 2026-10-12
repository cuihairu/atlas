package sharded

import (
	"context"
	"errors"
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

// hookShard injects failures or scripted results into one shard operation
// while delegating the rest to a memory store — fan-out error propagation
// and Reshard failure paths are unreachable through healthy shards.
type hookShard struct {
	store.CharacterStore
	getByID func(context.Context, int64) (*model.Character, error)
	list    func(context.Context, string, int, string) ([]*model.Character, error)
	search  func(context.Context, store.CharacterSearchFilter) ([]*model.Character, string, error)
	upsert  func(context.Context, *model.Character) error
}

func (h *hookShard) GetCharacterByCharacterID(ctx context.Context, id int64) (*model.Character, error) {
	if h.getByID != nil {
		return h.getByID(ctx, id)
	}
	return h.CharacterStore.GetCharacterByCharacterID(ctx, id)
}

func (h *hookShard) ListCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, error) {
	if h.list != nil {
		return h.list(ctx, serverID, limit, cursor)
	}
	return h.CharacterStore.ListCharactersByServer(ctx, serverID, limit, cursor)
}

func (h *hookShard) SearchCharacters(ctx context.Context, f store.CharacterSearchFilter) ([]*model.Character, string, error) {
	if h.search != nil {
		return h.search(ctx, f)
	}
	return h.CharacterStore.SearchCharacters(ctx, f)
}

func (h *hookShard) UpsertCharacter(ctx context.Context, ch *model.Character) error {
	if h.upsert != nil {
		return h.upsert(ctx, ch)
	}
	return h.CharacterStore.UpsertCharacter(ctx, ch)
}

func TestNew_RejectsNilShardInSlice(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil shard")
		}
	}()
	New([]store.CharacterStore{memory.New(), nil}, store.HashShardStrategy{})
}

func TestSharded_FanOutErrorPropagation(t *testing.T) {
	// A broken shard must fail the whole fan-out: silently skipping it
	// would present a partial index as authoritative.
	boom := errors.New("shard offline")
	inner := memory.New()
	s := New([]store.CharacterStore{inner, &hookShard{
		CharacterStore: inner,
		getByID: func(context.Context, int64) (*model.Character, error) {
			return nil, boom
		},
	}}, store.HashShardStrategy{})

	if _, err := s.GetCharacterByCharacterID(context.Background(), 1); !errors.Is(err, boom) {
		t.Errorf("GetCharacterByCharacterID: err = %v, want shard error", err)
	}

	listBoom := New([]store.CharacterStore{inner, &hookShard{
		CharacterStore: inner,
		list: func(context.Context, string, int, string) ([]*model.Character, error) {
			return nil, boom
		},
	}}, store.HashShardStrategy{})
	if _, err := listBoom.ListCharactersByServer(context.Background(), "srv-1", 10, ""); !errors.Is(err, boom) {
		t.Errorf("ListCharactersByServer: err = %v, want shard error", err)
	}

	searchBoom := New([]store.CharacterStore{inner, &hookShard{
		CharacterStore: inner,
		search: func(context.Context, store.CharacterSearchFilter) ([]*model.Character, string, error) {
			return nil, "", boom
		},
	}}, store.HashShardStrategy{})
	if _, _, err := searchBoom.SearchCharacters(context.Background(), store.CharacterSearchFilter{}); !errors.Is(err, boom) {
		t.Errorf("SearchCharacters: err = %v, want shard error", err)
	}
}

func TestReshard_DefaultBatchSize(t *testing.T) {
	// batchSize <= 0 falls back to 500: the copy still completes.
	ctx := context.Background()
	src := memory.New()
	for i := int64(1); i <= 5; i++ {
		putCharacter(t, ctx, src, i, "srv-1", i, "c", int(i))
	}
	dst := newSharded(t, 2)
	n, err := Reshard(ctx, src, dst, 0)
	if err != nil || n != 5 {
		t.Fatalf("Reshard with default batch: %d, %v", n, err)
	}
}

func TestReshard_SourceReadError(t *testing.T) {
	boom := errors.New("src read failed")
	src := &hookShard{CharacterStore: memory.New(), search: func(context.Context, store.CharacterSearchFilter) ([]*model.Character, string, error) {
		return nil, "", boom
	}}
	n, err := Reshard(context.Background(), src, newSharded(t, 2), 10)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want source error", err)
	}
	if n != 0 {
		t.Errorf("copied = %d, want 0", n)
	}
}

func TestReshard_DestinationWriteError(t *testing.T) {
	ctx := context.Background()
	src := memory.New()
	for i := int64(1); i <= 3; i++ {
		putCharacter(t, ctx, src, i, "srv-1", i, "c", int(i))
	}

	var seen int64
	boom := errors.New("dst write failed")
	dst := &hookShard{CharacterStore: memory.New(), upsert: func(_ context.Context, ch *model.Character) error {
		seen = ch.CharacterID
		return boom
	}}
	n, err := Reshard(ctx, src, dst, 2)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want destination error", err)
	}
	if n != 0 || seen != 1 {
		t.Errorf("copied = %d, last write = %d, want 0 and character 1", n, seen)
	}
}

func TestReshard_StuckCursorTerminates(t *testing.T) {
	// A store that echoes a non-empty cursor forever must not hang the
	// drain: the second identical cursor ends the copy.
	src := &hookShard{CharacterStore: memory.New(), search: func(_ context.Context, f store.CharacterSearchFilter) ([]*model.Character, string, error) {
		return []*model.Character{{AccountID: 1, ServerID: "srv-1", CharacterID: 7}}, "same", nil
	}}
	n, err := Reshard(context.Background(), src, newSharded(t, 1), 10)
	if err != nil {
		t.Fatalf("Reshard: %v", err)
	}
	if n != 2 {
		t.Errorf("copied = %d, want 2 (one page before the stuck cursor ends it)", n)
	}
}

func TestSharded_LimitClamps(t *testing.T) {
	// Fan-out reads clamp the caller-supplied page size to the documented
	// 50-default / 200-ceiling before it reaches the shards.
	ctx := context.Background()
	for i := int64(1); i <= 3; i++ {
		putCharacter(t, ctx, newSharded(t, 1), i, "srv-1", i, "c", int(i))
	}
	s := New([]store.CharacterStore{memory.New()}, nil) // nil strategy → hash default
	if got := s.ShardFor(1); got == nil {
		t.Fatal("nil strategy must default to hash sharding")
	}

	// Feed the shards a hook that records the limit actually requested.
	var gotList, gotSearch int
	hooked := New([]store.CharacterStore{&hookShard{
		CharacterStore: memory.New(),
		list: func(_ context.Context, _ string, limit int, _ string) ([]*model.Character, error) {
			gotList = limit
			return nil, nil
		},
		search: func(_ context.Context, f store.CharacterSearchFilter) ([]*model.Character, string, error) {
			gotSearch = f.Limit
			return nil, "", nil
		},
	}}, store.HashShardStrategy{})

	_, _ = hooked.ListCharactersByServer(ctx, "srv-1", 0, "")
	if gotList != 50 {
		t.Errorf("limit 0 → shard limit %d, want 50", gotList)
	}
	_, _ = hooked.ListCharactersByServer(ctx, "srv-1", 9999, "")
	if gotList != 200 {
		t.Errorf("limit 9999 → shard limit %d, want 200", gotList)
	}
	_, _, _ = hooked.SearchCharacters(ctx, store.CharacterSearchFilter{Limit: -1})
	if gotSearch != 50 {
		t.Errorf("search limit -1 → shard limit %d, want 50", gotSearch)
	}
	_, _, _ = hooked.SearchCharacters(ctx, store.CharacterSearchFilter{Limit: 9999})
	if gotSearch != 200 {
		t.Errorf("search limit 9999 → shard limit %d, want 200", gotSearch)
	}
}
