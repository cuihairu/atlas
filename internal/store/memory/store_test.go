package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

func newStore() *Store { return New() }

func mkServer(id, region string, status model.ServerStatus) *model.Server {
	return &model.Server{
		ID:       id,
		Name:     id,
		Type:     "game",
		Region:   region,
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 1000,
		Status:   status,
	}
}

// TestServerCRUDAndFilter covers registration (idempotent), reads, status
// updates, deletion and filter matching.
func TestServerCRUDAndFilter(t *testing.T) {
	s := newStore()
	ctx := context.Background()

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	if err := s.RegisterServer(ctx, mkServer("srv-1", "cn-east", model.StatusOnline)); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Idempotent re-register overwrites mutable fields, keeps creation time.
	first, _ := s.GetServer(ctx, "srv-1")
	updated := mkServer("srv-1", "us-west", model.StatusOnline)
	updated.Capacity = 2000
	if err := s.RegisterServer(ctx, updated); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	second, _ := s.GetServer(ctx, "srv-1")
	if second.Region != "us-west" || second.Capacity != 2000 {
		t.Fatalf("re-register did not overwrite: %+v", second)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Error("re-register changed CreatedAt")
	}

	// Unknown server.
	if _, err := s.GetServer(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get ghost = %v, want ErrNotFound", err)
	}
	if err := s.UpdateServerStatus(ctx, "ghost", model.StatusOffline); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update ghost = %v, want ErrNotFound", err)
	}

	// Status update.
	if err := s.UpdateServerStatus(ctx, "srv-1", model.StatusMaintenance); err != nil {
		t.Fatalf("update status: %v", err)
	}
	if srv, _ := s.GetServer(ctx, "srv-1"); srv.Status != model.StatusMaintenance {
		t.Errorf("status = %s, want maintenance", srv.Status)
	}

	// List + filter matching.
	_ = s.RegisterServer(ctx, mkServer("srv-2", "us-west", model.StatusOnline))
	_ = s.RegisterServer(ctx, mkServer("srv-3", "cn-east", model.StatusOffline))

	// srv-1 re-registered into us-west and later moved to maintenance;
	// srv-2 is us-west online; srv-3 is cn-east offline.
	for region, want := range map[string]int{"cn-east": 1, "us-west": 2, "eu-west": 0} {
		got, err := s.ListServers(ctx, store.ServerFilter{Region: region})
		if err != nil {
			t.Fatalf("list %s: %v", region, err)
		}
		if len(got) != want {
			t.Errorf("region=%s: got %d, want %d", region, len(got), want)
		}
	}
	got, _ := s.ListServers(ctx, store.ServerFilter{Status: model.StatusOnline})
	if len(got) != 1 {
		t.Errorf("status filter: got %d, want 1 (only srv-2)", len(got))
	}

	// Delete.
	if err := s.DeleteServer(ctx, "srv-3"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetServer(ctx, "srv-3"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after delete = %v, want ErrNotFound", err)
	}
}

// TestCharacterIndexCRUD covers the character index projection.
func TestCharacterIndexCRUD(t *testing.T) {
	s := newStore()
	ctx := context.Background()

	ch := &model.Character{
		AccountID:   42,
		ServerID:    "srv-1",
		CharacterID: 9001,
		Name:        "galadriel",
		Level:       60,
	}
	if err := s.UpsertCharacter(ctx, ch); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetCharacter(ctx, 42, "srv-1", 9001)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "galadriel" || got.Level != 60 {
		t.Fatalf("got = %+v", got)
	}

	if _, err := s.GetCharacter(ctx, 42, "srv-1", 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing character = %v, want ErrNotFound", err)
	}
	if _, err := s.GetCharacterByCharacterID(ctx, 777); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing by char id = %v, want ErrNotFound", err)
	}

	// Patch.
	name := "galadriel-of-light"
	level := 61
	err = s.UpdateCharacter(ctx, 42, "srv-1", 9001, store.CharacterPatch{Name: &name, Level: &level})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if err := s.UpdateCharacter(ctx, 42, "srv-1", 1, store.CharacterPatch{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("patch missing = %v, want ErrNotFound", err)
	}
	got, _ = s.GetCharacter(ctx, 42, "srv-1", 9001)
	if got.Name != name || got.Level != level {
		t.Errorf("patch not applied: %+v", got)
	}

	// Listing by account and by server.
	if err := s.UpsertCharacter(ctx, &model.Character{AccountID: 42, ServerID: "srv-2", CharacterID: 9002, Name: "second"}); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	byAccount, err := s.ListCharactersByAccount(ctx, 42)
	if err != nil || len(byAccount) != 2 {
		t.Fatalf("by account = %d, %v; want 2", len(byAccount), err)
	}
	byServer, err := s.ListCharactersByServer(ctx, "srv-2", 10, "")
	if err != nil || len(byServer) != 1 {
		t.Fatalf("by server = %d, %v; want 1", len(byServer), err)
	}

	// Delete.
	if err := s.DeleteCharacter(ctx, 42, "srv-1", 9001); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetCharacter(ctx, 42, "srv-1", 9001); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after delete = %v, want ErrNotFound", err)
	}
}

// TestRuntimeLifecycle covers heartbeat recording, reads and cleanup.
func TestRuntimeLifecycle(t *testing.T) {
	s := newStore()
	ctx := context.Background()

	if err := s.RecordHeartbeat(ctx, "srv-1", model.Heartbeat{Players: 10, Load: 0.5}); err != nil {
		t.Fatalf("record: %v", err)
	}
	rt, err := s.GetRuntime(ctx, "srv-1")
	if err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	if rt.Players != 10 || rt.Load != 0.5 {
		t.Fatalf("runtime = %+v", rt)
	}
	if rt.LastSeenAt.IsZero() {
		t.Error("last_seen_at not stamped")
	}

	if err := s.DeleteRuntime(ctx, "srv-1"); err != nil {
		t.Fatalf("delete runtime: %v", err)
	}
	if _, err := s.GetRuntime(ctx, "srv-1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after delete = %v, want ErrNotFound", err)
	}
}

// TestMigrations covers migration CRUD + status transitions.
func TestMigrations(t *testing.T) {
	s := newStore()
	ctx := context.Background()

	mig := &model.Migration{
		ID:            "mig-1",
		SourceServers: []string{"srv-a", "srv-b"},
		TargetServer:  "srv-c",
		Status:        model.MigrationPending,
		StartedAt:     time.Now(),
	}
	if err := s.CreateMigration(ctx, mig); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetMigration(ctx, "mig-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Source slice must be deep-copied on write.
	got.SourceServers[0] = "mutated"
	if mig.SourceServers[0] != "srv-a" {
		t.Error("stored migration aliases caller's slice")
	}

	if _, err := s.GetMigration(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing migration = %v, want ErrNotFound", err)
	}

	now := time.Now()
	if err := s.UpdateMigrationStatus(ctx, "mig-1", model.MigrationCompleted, &now); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, _ = s.GetMigration(ctx, "mig-1")
	if got.Status != model.MigrationCompleted || got.CompletedAt == nil {
		t.Errorf("status update not applied: %+v", got)
	}

	list, err := s.ListMigrations(ctx, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d, %v; want 1", len(list), err)
	}
}

// TestRealmsAndShards covers realm/shard CRUD with conflict and FK checks.
func TestRealmsAndShards(t *testing.T) {
	s := newStore()
	ctx := context.Background()

	if err := s.CreateRealm(ctx, &model.Realm{ID: "realm-1", Name: "r1", Region: "cn-east"}); err != nil {
		t.Fatalf("create realm: %v", err)
	}
	if err := s.CreateRealm(ctx, &model.Realm{ID: "realm-1", Name: "dup"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("dup realm = %v, want ErrConflict", err)
	}
	if _, err := s.GetRealm(ctx, "realm-1"); err != nil {
		t.Fatalf("get realm: %v", err)
	}
	if _, err := s.GetRealm(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing realm = %v, want ErrNotFound", err)
	}
	realms, err := s.ListRealms(ctx, 10)
	if err != nil || len(realms) != 1 {
		t.Fatalf("realms = %d, %v; want 1", len(realms), err)
	}

	if err := s.CreateShard(ctx, &model.Shard{ID: "shard-1", RealmID: "realm-1", Name: "s1"}); err != nil {
		t.Fatalf("create shard: %v", err)
	}
	// The realm FK check lives in the admin service, not the store — the
	// store only enforces shard-ID conflicts.
	if err := s.CreateShard(ctx, &model.Shard{ID: "shard-1", RealmID: "realm-1", Name: "dup"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("dup shard = %v, want ErrConflict", err)
	}
	shards, err := s.ListShards(ctx, "realm-1", 10)
	if err != nil || len(shards) != 1 {
		t.Fatalf("shards = %d, %v; want 1", len(shards), err)
	}
	if _, err := s.GetShard(ctx, "shard-1"); err != nil {
		t.Fatalf("get shard: %v", err)
	}
}

// TestStatsAggregation covers the dashboard counters.
func TestStatsAggregation(t *testing.T) {
	s := newStore()
	ctx := context.Background()

	_ = s.RegisterServer(ctx, mkServer("srv-1", "cn-east", model.StatusOnline))
	_ = s.RecordHeartbeat(ctx, "srv-1", model.Heartbeat{Players: 100, Load: 0.5})
	_ = s.UpsertCharacter(ctx, &model.Character{AccountID: 1, ServerID: "srv-1", CharacterID: 1, Name: "x"})

	stats, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalServers != 1 || stats.ServersByStatus["online"] != 1 || stats.ServersByRegion["cn-east"] != 1 {
		t.Errorf("server aggregates wrong: %+v", stats)
	}
	if stats.TotalPlayers != 100 || stats.TotalCharacters != 1 {
		t.Errorf("player/character totals wrong: %+v", stats)
	}
}

// TestCloseIsNoop: the in-memory store has nothing to release.
func TestCloseIsNoop(t *testing.T) {
	s := newStore()
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
