package admin

import (
	"context"
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