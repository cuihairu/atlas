package migration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newTestService(t *testing.T) (*Service, *memory.Store) {
	t.Helper()
	mem := memory.New()
	return New(mem), mem
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
		t.Fatalf("register %s: %v", id, err)
	}
}

func TestCreateMigration(t *testing.T) {
	svc, mem := newTestService(t)
	ctx := context.Background()
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-1002", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	m, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-1001", "game-1002"},
		TargetServer:  "game-2001",
	})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	if m.ID == "" || m.Status != model.MigrationPending {
		t.Errorf("migration = %+v, want an id + pending", m)
	}
	if len(m.SourceServers) != 2 {
		t.Errorf("sources = %d, want 2", len(m.SourceServers))
	}
	stored, err := mem.GetMigration(ctx, m.ID)
	if err != nil || stored.TargetServer != "game-2001" {
		t.Fatalf("stored = %+v, err %v", stored, err)
	}
}

func TestCreateMigration_Validation(t *testing.T) {
	svc, mem := newTestService(t)
	ctx := context.Background()
	registerServer(t, mem, "game-2001", model.StatusOnline)

	if _, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: nil, TargetServer: "game-2001",
	}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("empty sources err = %v, want ErrInvalid", err)
	}
	if _, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-2001"},
	}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("missing target err = %v, want ErrInvalid", err)
	}
	// Both fail before anything is persisted.
	if m, _ := mem.ListMigrations(ctx, 10); len(m) != 0 {
		t.Errorf("migrations = %d, want 0", len(m))
	}
}

func TestCreateMigration_SourceNotFound(t *testing.T) {
	svc, mem := newTestService(t)
	ctx := context.Background()
	registerServer(t, mem, "game-2001", model.StatusOnline)

	_, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"ghost"}, TargetServer: "game-2001",
	})
	if !store.IsNotFound(err) {
		t.Errorf("unknown source err = %v, want ErrNotFound", err)
	}
	_, err = svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-2001"}, TargetServer: "ghost",
	})
	if !store.IsNotFound(err) {
		t.Errorf("unknown target err = %v, want ErrNotFound", err)
	}
}

func TestGetAndListMigrations(t *testing.T) {
	svc, mem := newTestService(t)
	ctx := context.Background()
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	created, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-1001"}, TargetServer: "game-2001",
	})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	got, err := svc.GetMigration(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("GetMigration = %+v, err %v", got, err)
	}
	if _, err := svc.GetMigration(ctx, "nope"); !store.IsNotFound(err) {
		t.Errorf("missing migration err = %v, want ErrNotFound", err)
	}

	list, err := svc.ListMigrations(ctx, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListMigrations = %d, err %v", len(list), err)
	}
	if got, err := svc.ListMigrations(ctx, 0); err != nil || len(got) != 1 {
		t.Errorf("limit 0 (no cap) = %d, err %v", len(got), err)
	}
}

// mockWindowCreator records calls and can be configured to fail.
type mockWindowCreator struct {
	called    bool
	serverIDs []string
	startAt   time.Time
	endAt     time.Time
	err       error
}

func (m *mockWindowCreator) CreateWindows(ctx context.Context, serverIDs []string, startAt, endAt time.Time) error {
	m.called = true
	m.serverIDs = append([]string(nil), serverIDs...)
	m.startAt = startAt
	m.endAt = endAt
	return m.err
}

func TestCreateMigration_WithWindows(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-1002", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	wc := &mockWindowCreator{}
	svc := New(mem, wc)

	m, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-1001", "game-1002"},
		TargetServer:  "game-2001",
	})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	if !wc.called {
		t.Fatal("expected window creator to be called")
	}
	if len(wc.serverIDs) != 2 || wc.serverIDs[0] != "game-1001" || wc.serverIDs[1] != "game-1002" {
		t.Errorf("window server IDs = %v, want [game-1001 game-1002]", wc.serverIDs)
	}
	if d := wc.endAt.Sub(wc.startAt); d != DefaultMigrationWindowDuration {
		t.Errorf("window duration = %v, want %v", d, DefaultMigrationWindowDuration)
	}
	if m.Status != model.MigrationPending {
		t.Errorf("migration status = %s, want pending", m.Status)
	}
}

func TestCreateMigration_WindowsFailClosed(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	wc := &mockWindowCreator{err: errors.New("window store broken")}
	svc := New(mem, wc)

	_, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-1001"},
		TargetServer:  "game-2001",
	})
	if err == nil {
		t.Fatal("expected error when window creation fails")
	}
	// Fail-closed: no migration record is written.
	if migs, _ := svc.ListMigrations(ctx, 100); len(migs) != 0 {
		t.Errorf("migrations = %d, want 0 (fail-closed)", len(migs))
	}
}

func TestCreateMigration_NoWindowsWithoutCreator(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	// No WindowCreator injected: behavior is unchanged (migration record only).
	svc := New(mem)
	m, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-1001"},
		TargetServer:  "game-2001",
	})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	if m.Status != model.MigrationPending {
		t.Errorf("migration status = %s, want pending", m.Status)
	}
}

func TestRollbackMigration(t *testing.T) {
	svc, mem := newTestService(t)
	ctx := context.Background()
	registerServer(t, mem, "game-1001", model.StatusOnline)
	registerServer(t, mem, "game-2001", model.StatusOnline)

	m, err := svc.CreateMigration(ctx, CreateMigrationRequest{
		SourceServers: []string{"game-1001"}, TargetServer: "game-2001",
	})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}

	// Each rollback-able status succeeds, others are rejected.
	for _, status := range []model.MigrationStatus{
		model.MigrationPending, model.MigrationMigrating, model.MigrationFailed,
	} {
		if err := svc.RollbackMigration(ctx, m.ID); err != nil {
			t.Fatalf("rollback from %s: %v", status, err)
		}
		// Reset the status to exercise the next transition.
		_ = mem.UpdateMigrationStatus(ctx, m.ID, status, nil)
	}

	if err := svc.RollbackMigration(ctx, m.ID); err != nil {
		t.Fatalf("rollback from rolled_back: %v", err)
	}
	_ = mem.UpdateMigrationStatus(ctx, m.ID, model.MigrationCompleted, nil)
	if err := svc.RollbackMigration(ctx, m.ID); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("rollback completed err = %v, want ErrInvalid", err)
	}
	if err := svc.RollbackMigration(ctx, "nope"); !store.IsNotFound(err) {
		t.Errorf("rollback missing err = %v, want ErrNotFound", err)
	}
}
