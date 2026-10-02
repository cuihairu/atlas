package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func setupService(t *testing.T) (*Service, *memory.Store) {
	t.Helper()
	mem := memory.New()
	svc := New(mem)
	return svc, mem
}

func registerServer(t *testing.T, mem *memory.Store, id string, status model.ServerStatus) {
	t.Helper()
	srv := &model.Server{
		ID:       id,
		Name:     "Test " + id,
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
		Status:   status,
	}
	if err := mem.RegisterServer(context.Background(), srv); err != nil {
		t.Fatalf("register server %s: %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// SetMaintenance
// ---------------------------------------------------------------------------

func TestSetMaintenance_FromOnline(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)

	if err := svc.SetMaintenance(context.Background(), "game-1001"); err != nil {
		t.Fatalf("SetMaintenance: %v", err)
	}

	srv, _ := mem.GetServer(context.Background(), "game-1001")
	if srv.Status != model.StatusMaintenance {
		t.Errorf("expected maintenance, got %s", srv.Status)
	}
}

func TestSetMaintenance_FromStarting(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusStarting)

	if err := svc.SetMaintenance(context.Background(), "game-1001"); err != nil {
		t.Fatalf("SetMaintenance: %v", err)
	}

	srv, _ := mem.GetServer(context.Background(), "game-1001")
	if srv.Status != model.StatusMaintenance {
		t.Errorf("expected maintenance, got %s", srv.Status)
	}
}

func TestSetMaintenance_FromOffline(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOffline)

	err := svc.SetMaintenance(context.Background(), "game-1001")
	if err == nil {
		t.Fatal("expected error for SetMaintenance from offline")
	}
}

func TestSetMaintenance_NotFound(t *testing.T) {
	svc, _ := setupService(t)

	err := svc.SetMaintenance(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent server")
	}
}

// ---------------------------------------------------------------------------
// SetDrain
// ---------------------------------------------------------------------------

func TestSetDrain_FromOnline(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)

	if err := svc.SetDrain(context.Background(), "game-1001"); err != nil {
		t.Fatalf("SetDrain: %v", err)
	}

	srv, _ := mem.GetServer(context.Background(), "game-1001")
	if srv.Status != model.StatusDraining {
		t.Errorf("expected draining, got %s", srv.Status)
	}
}

func TestSetDrain_FromMaintenance(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusMaintenance)

	err := svc.SetDrain(context.Background(), "game-1001")
	if err == nil {
		t.Fatal("expected error for SetDrain from maintenance")
	}
}

// ---------------------------------------------------------------------------
// Enable
// ---------------------------------------------------------------------------

func TestEnable_FromMaintenance(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusMaintenance)

	if err := svc.Enable(context.Background(), "game-1001"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	srv, _ := mem.GetServer(context.Background(), "game-1001")
	if srv.Status != model.StatusOnline {
		t.Errorf("expected online, got %s", srv.Status)
	}
}

func TestEnable_FromDisabled(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusDisabled)

	if err := svc.Enable(context.Background(), "game-1001"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	srv, _ := mem.GetServer(context.Background(), "game-1001")
	if srv.Status != model.StatusOnline {
		t.Errorf("expected online, got %s", srv.Status)
	}
}

func TestEnable_FromOnline(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)

	err := svc.Enable(context.Background(), "game-1001")
	if err == nil {
		t.Fatal("expected error for Enable from online")
	}
}

// ---------------------------------------------------------------------------
// Disable
// ---------------------------------------------------------------------------

func TestDisable(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)

	// Record heartbeat so runtime data exists.
	mem.RecordHeartbeat(context.Background(), "game-1001", model.Heartbeat{
		Players: 100, Load: 0.5, Status: model.StatusOnline,
	})

	if err := svc.Disable(context.Background(), "game-1001"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	srv, _ := mem.GetServer(context.Background(), "game-1001")
	if srv.Status != model.StatusDisabled {
		t.Errorf("expected disabled, got %s", srv.Status)
	}

	// Runtime should be cleaned up.
	_, err := mem.GetRuntime(context.Background(), "game-1001")
	if err == nil {
		t.Error("expected runtime to be deleted after disable")
	}
}

func TestDisable_NotFound(t *testing.T) {
	svc, _ := setupService(t)

	err := svc.Disable(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent server")
	}
}

// ---------------------------------------------------------------------------
// GetStats
// ---------------------------------------------------------------------------

func TestGetStats(t *testing.T) {
	svc, mem := setupService(t)

	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-1002", model.StatusMaintenance)

	// Add a character.
	ch := &model.Character{
		AccountID:   10001,
		ServerID:    "game-1001",
		CharacterID: 823712,
		Name:        "TestChar",
		Level:       50,
		ClassID:     3,
	}
	mem.UpsertCharacter(context.Background(), ch)

	stats, err := svc.GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if stats.TotalServers != 2 {
		t.Errorf("expected 2 servers, got %d", stats.TotalServers)
	}
	if stats.ServersByStatus["online"] != 1 {
		t.Errorf("expected 1 online server, got %d", stats.ServersByStatus["online"])
	}
	if stats.ServersByStatus["maintenance"] != 1 {
		t.Errorf("expected 1 maintenance server, got %d", stats.ServersByStatus["maintenance"])
	}
	if stats.TotalCharacters != 1 {
		t.Errorf("expected 1 character, got %d", stats.TotalCharacters)
	}
	if stats.TotalCapacity != 4000 {
		t.Errorf("expected capacity 4000, got %d", stats.TotalCapacity)
	}
}

// ---------------------------------------------------------------------------
// SearchCharacters
// ---------------------------------------------------------------------------

func TestSearchCharacters(t *testing.T) {
	svc, mem := setupService(t)

	// Add characters.
	for i, name := range []string{"Alpha", "Beta", "Gamma"} {
		ch := &model.Character{
			AccountID:   int64(10000 + i),
			ServerID:    "game-1001",
			CharacterID: int64(823710 + i),
			Name:        name,
			Level:       10 + i*20,
			ClassID:     3,
		}
		mem.UpsertCharacter(context.Background(), ch)
	}

	// Search by name.
	chars, _, err := svc.SearchCharacters(context.Background(), store.CharacterSearchFilter{
		Name: "alpha",
	})
	if err != nil {
		t.Fatalf("SearchCharacters: %v", err)
	}
	if len(chars) != 1 {
		t.Fatalf("expected 1 result, got %d", len(chars))
	}
	if chars[0].Name != "Alpha" {
		t.Errorf("expected Alpha, got %s", chars[0].Name)
	}

	// Search by level range.
	minLevel := 20
	maxLevel := 30
	chars, _, err = svc.SearchCharacters(context.Background(), store.CharacterSearchFilter{
		MinLevel: &minLevel,
		MaxLevel: &maxLevel,
	})
	if err != nil {
		t.Fatalf("SearchCharacters by level: %v", err)
	}
	if len(chars) != 1 {
		t.Fatalf("expected 1 result, got %d", len(chars))
	}

	// Search by server.
	chars, _, err = svc.SearchCharacters(context.Background(), store.CharacterSearchFilter{
		ServerID: "game-1001",
	})
	if err != nil {
		t.Fatalf("SearchCharacters by server: %v", err)
	}
	if len(chars) != 3 {
		t.Fatalf("expected 3 results, got %d", len(chars))
	}
}

// ---------------------------------------------------------------------------
// CreateMigration / GetMigration / ListMigrations / RollbackMigration
// ---------------------------------------------------------------------------

func TestCreateMigration(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-1002", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	req := CreateMigrationRequest{
		SourceServers: []string{"game-1001", "game-1002"},
		TargetServer:  "game-2001",
	}

	m, err := svc.CreateMigration(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	if m.Status != model.MigrationPending {
		t.Errorf("expected pending, got %s", m.Status)
	}
	if len(m.SourceServers) != 2 {
		t.Errorf("expected 2 source servers, got %d", len(m.SourceServers))
	}
}

func TestCreateMigration_MissingSources(t *testing.T) {
	svc, _ := setupService(t)

	req := CreateMigrationRequest{
		SourceServers: []string{},
		TargetServer:  "game-2001",
	}

	_, err := svc.CreateMigration(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty source servers")
	}
}

func TestCreateMigration_MissingTarget(t *testing.T) {
	svc, _ := setupService(t)

	req := CreateMigrationRequest{
		SourceServers: []string{"game-1001"},
		TargetServer:  "",
	}

	_, err := svc.CreateMigration(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
}

func TestCreateMigration_SourceNotFound(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	req := CreateMigrationRequest{
		SourceServers: []string{"nonexistent"},
		TargetServer:  "game-2001",
	}

	_, err := svc.CreateMigration(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestGetMigration(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	m, _ := svc.CreateMigration(context.Background(), CreateMigrationRequest{
		SourceServers: []string{"game-1001"},
		TargetServer:  "game-2001",
	})

	got, err := svc.GetMigration(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("GetMigration: %v", err)
	}
	if got.ID != m.ID {
		t.Errorf("expected %s, got %s", m.ID, got.ID)
	}
}

func TestGetMigration_NotFound(t *testing.T) {
	svc, _ := setupService(t)

	_, err := svc.GetMigration(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent migration")
	}
}

func TestListMigrations(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	svc.CreateMigration(context.Background(), CreateMigrationRequest{
		SourceServers: []string{"game-1001"},
		TargetServer:  "game-2001",
	})

	migrations, err := svc.ListMigrations(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListMigrations: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(migrations))
	}
}

func TestRollbackMigration_FromPending(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	m, _ := svc.CreateMigration(context.Background(), CreateMigrationRequest{
		SourceServers: []string{"game-1001"},
		TargetServer:  "game-2001",
	})

	if err := svc.RollbackMigration(context.Background(), m.ID); err != nil {
		t.Fatalf("RollbackMigration: %v", err)
	}

	got, _ := svc.GetMigration(context.Background(), m.ID)
	if got.Status != model.MigrationRolledBack {
		t.Errorf("expected rolled_back, got %s", got.Status)
	}
}

func TestRollbackMigration_FromCompleted(t *testing.T) {
	svc, mem := setupService(t)
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	m, _ := svc.CreateMigration(context.Background(), CreateMigrationRequest{
		SourceServers: []string{"game-1001"},
		TargetServer:  "game-2001",
	})

	// Manually set to completed.
	now := time.Now()
	mem.UpdateMigrationStatus(context.Background(), m.ID, model.MigrationCompleted, &now)

	err := svc.RollbackMigration(context.Background(), m.ID)
	if err == nil {
		t.Fatal("expected error for rollback from completed")
	}
}

func TestRollbackMigration_NotFound(t *testing.T) {
	svc, _ := setupService(t)

	err := svc.RollbackMigration(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent migration")
	}
}

// ---------------------------------------------------------------------------
// Realms & Shards (TODO v0.1.14)
// ---------------------------------------------------------------------------

func TestCreateRealm(t *testing.T) {
	svc, mem := setupService(t)

	realm, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "realm-01", Name: "华东", Region: "cn-east"})
	if err != nil {
		t.Fatalf("CreateRealm: %v", err)
	}
	if realm.Status != "active" {
		t.Errorf("expected default status active, got %q", realm.Status)
	}

	stored, err := mem.GetRealm(context.Background(), "realm-01")
	if err != nil {
		t.Fatalf("GetRealm: %v", err)
	}
	if stored.Name != "华东" || stored.Region != "cn-east" {
		t.Errorf("unexpected realm: %+v", stored)
	}
	if stored.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreateRealm_Conflict(t *testing.T) {
	svc, _ := setupService(t)

	if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "realm-01", Name: "华东"}); err != nil {
		t.Fatalf("first CreateRealm: %v", err)
	}
	if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "realm-01", Name: "dup"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCreateRealm_MissingFields(t *testing.T) {
	svc, _ := setupService(t)

	if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "", Name: "no-id"}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("missing id: expected ErrInvalid, got %v", err)
	}
	if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "r", Name: ""}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("missing name: expected ErrInvalid, got %v", err)
	}
}

func TestListRealms(t *testing.T) {
	svc, _ := setupService(t)

	for _, id := range []string{"realm-01", "realm-02", "realm-03"} {
		if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: id, Name: id}); err != nil {
			t.Fatalf("CreateRealm %s: %v", id, err)
		}
	}

	realms, err := svc.ListRealms(context.Background(), 2)
	if err != nil {
		t.Fatalf("ListRealms: %v", err)
	}
	if len(realms) != 2 {
		t.Fatalf("expected 2 realms with limit, got %d", len(realms))
	}

	all, err := svc.ListRealms(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListRealms(all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 realms, got %d", len(all))
	}
}

func TestCreateShard(t *testing.T) {
	svc, mem := setupService(t)

	if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "realm-01", Name: "华东"}); err != nil {
		t.Fatalf("CreateRealm: %v", err)
	}

	shard, err := svc.CreateShard(context.Background(), CreateShardRequest{ID: "shard-0101", RealmID: "realm-01", Name: "一区"})
	if err != nil {
		t.Fatalf("CreateShard: %v", err)
	}
	if shard.RealmID != "realm-01" || shard.Status != "active" {
		t.Errorf("unexpected shard: %+v", shard)
	}

	stored, err := mem.GetShard(context.Background(), "shard-0101")
	if err != nil {
		t.Fatalf("GetShard: %v", err)
	}
	if stored.Name != "一区" {
		t.Errorf("unexpected shard name %q", stored.Name)
	}
}

func TestCreateShard_RealmNotFound(t *testing.T) {
	svc, _ := setupService(t)

	_, err := svc.CreateShard(context.Background(), CreateShardRequest{ID: "shard-0101", RealmID: "missing", Name: "一区"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateShard_MissingFields(t *testing.T) {
	svc, _ := setupService(t)

	if _, err := svc.CreateShard(context.Background(), CreateShardRequest{ID: "s", RealmID: "", Name: "n"}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("missing realm_id: expected ErrInvalid, got %v", err)
	}
}

func TestListShards_ByRealm(t *testing.T) {
	svc, _ := setupService(t)

	if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "realm-01", Name: "华东"}); err != nil {
		t.Fatalf("CreateRealm: %v", err)
	}
	if _, err := svc.CreateRealm(context.Background(), CreateRealmRequest{ID: "realm-02", Name: "华北"}); err != nil {
		t.Fatalf("CreateRealm: %v", err)
	}
	for _, id := range []string{"shard-0101", "shard-0102"} {
		if _, err := svc.CreateShard(context.Background(), CreateShardRequest{ID: id, RealmID: "realm-01", Name: id}); err != nil {
			t.Fatalf("CreateShard %s: %v", id, err)
		}
	}
	if _, err := svc.CreateShard(context.Background(), CreateShardRequest{ID: "shard-0201", RealmID: "realm-02", Name: "x"}); err != nil {
		t.Fatalf("CreateShard: %v", err)
	}

	shards, err := svc.ListShards(context.Background(), "realm-01", 0)
	if err != nil {
		t.Fatalf("ListShards: %v", err)
	}
	if len(shards) != 2 {
		t.Fatalf("expected 2 shards for realm-01, got %d", len(shards))
	}
	for _, sh := range shards {
		if sh.RealmID != "realm-01" {
			t.Errorf("unexpected realm_id %q", sh.RealmID)
		}
	}

	all, err := svc.ListShards(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("ListShards(all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 shards overall, got %d", len(all))
	}
}

// ---------------------------------------------------------------------------
// Server tags (标记体系)
// ---------------------------------------------------------------------------

func TestServerTagCRUD(t *testing.T) {
	svc, mem := setupService(t)
	ctx := context.Background()
	registerServer(t, mem, "game-1", model.StatusOnline)

	// Empty list is an empty slice, not nil (JSON renders as []).
	tags, err := svc.GetServerTags(ctx, "game-1")
	if err != nil || len(tags) != 0 {
		t.Fatalf("fresh tags = %v err=%v", tags, err)
	}

	// Preset defaults are filled; custom defaults to internal.
	tags, err = svc.AddServerTag(ctx, "game-1", AddServerTagRequest{Code: "hot"})
	if err != nil {
		t.Fatalf("add hot: %v", err)
	}
	public := true
	tags, err = svc.AddServerTag(ctx, "game-1", AddServerTagRequest{Code: "watchlist", Label: "内部观察", Public: &public})
	if err != nil {
		t.Fatalf("add custom: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("tags after adds = %v", tags)
	}
	// Preset defaults public; explicit pointer wins; omission defaults an
	// unknown code to internal.
	srv, _ := mem.GetServer(ctx, "game-1")
	for _, tag := range srv.Tags {
		switch tag.Code {
		case "hot":
			if !tag.Public || tag.Label != "火热" || tag.Tier != model.TierHot {
				t.Fatalf("hot = %+v", tag)
			}
		case "watchlist":
			if !tag.Public {
				t.Fatalf("watchlist should honor explicit public: %+v", tag)
			}
		}
	}
	if _, err = svc.AddServerTag(ctx, "game-1", AddServerTagRequest{Code: "silent", Label: "内部"}); err != nil {
		t.Fatalf("add silent: %v", err)
	}
	srv, _ = mem.GetServer(ctx, "game-1")
	for _, tag := range srv.Tags {
		if tag.Code == "silent" && tag.Public {
			t.Fatalf("custom without explicit public must be internal: %+v", tag)
		}
	}

	// Upsert replaces by code.
	if _, err = svc.AddServerTag(ctx, "game-1", AddServerTagRequest{Code: "hot", Label: "超火爆"}); err != nil {
		t.Fatalf("replace hot: %v", err)
	}
	tags, _ = svc.GetServerTags(ctx, "game-1")
	if len(tags) != 3 {
		t.Fatalf("replace should not add: %+v", tags)
	}

	// Remove leaves the other tags intact.
	if err = svc.RemoveServerTag(ctx, "game-1", "hot"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	tags, _ = svc.GetServerTags(ctx, "game-1")
	if len(tags) != 2 || !model.HasTag(tags, "watchlist") || !model.HasTag(tags, "silent") {
		t.Fatalf("after remove = %+v", tags)
	}
}

func TestServerTagErrors(t *testing.T) {
	svc, mem := setupService(t)
	ctx := context.Background()
	registerServer(t, mem, "game-1", model.StatusOnline)

	if _, err := svc.GetServerTags(ctx, "ghost"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("get on missing server = %v", model.ErrNotFound)
	}
	if _, err := svc.AddServerTag(ctx, "ghost", AddServerTagRequest{Code: "hot"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("add on missing server = %v", err)
	}
	if err := svc.RemoveServerTag(ctx, "ghost", "hot"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("remove on missing server = %v", err)
	}
	if _, err := svc.AddServerTag(ctx, "game-1", AddServerTagRequest{Code: "BAD CODE"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("bad code = %v", err)
	}
	if _, err := svc.AddServerTag(ctx, "game-1", AddServerTagRequest{Code: "custom"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("custom without label = %v", err)
	}
	if err := svc.RemoveServerTag(ctx, "game-1", "hot"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("remove missing tag = %v", err)
	}
}
