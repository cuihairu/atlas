// Package migration implements the migration orchestration state machine
// (plan / rollback) documented in docs/migration.md §9. It owns the
// server_migrations records and stays separate from the Directory's
// character index projection: the two concerns must never be fused into
// one package.
package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Store is the persistence surface this module needs.
type Store interface {
	GetServer(ctx context.Context, id string) (*model.Server, error)
	CreateMigration(ctx context.Context, m *model.Migration) error
	GetMigration(ctx context.Context, id string) (*model.Migration, error)
	UpdateMigrationStatus(ctx context.Context, id string, status model.MigrationStatus, completedAt *time.Time) error
	ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error)
}

// CreateMigrationRequest is the DTO for creating a migration.
type CreateMigrationRequest struct {
	SourceServers []string `json:"source_servers"`
	TargetServer  string   `json:"target_server"`
}

// DefaultMigrationWindowDuration is how long an auto-created migration
// maintenance window lasts. Migrations are open-ended state records (the
// status moves pending → migrating → completed but the record itself has no
// end); the window is a protection convenience that auto-expires, so the
// source servers are never locked in maintenance forever. Tunable later via
// config if ops needs longer coverage.
const DefaultMigrationWindowDuration = 24 * time.Hour

// WindowCreator is the optional dependency for the migration-window
// linkage: when a migration is created, maintenance windows are
// auto-created on the source servers so they enter protection during the
// migration. The migration module owns the when (at creation, fail-closed
// before the migration record is written); the caller provides the how
// (which window API to use).
type WindowCreator interface {
	CreateWindows(ctx context.Context, serverIDs []string, startAt, endAt time.Time) error
}

// Service provides migration orchestration.
type Service struct {
	store   Store
	windows WindowCreator // nil = no linkage (migration record only)
}

// New creates a new migration service. The variadic windows argument is
// optional; when provided, CreateMigration auto-creates maintenance
// windows on the source servers (fail-closed). Existing callers that
// pass only the store get the unchanged behavior.
func New(s Store, windows ...WindowCreator) *Service {
	var w WindowCreator
	if len(windows) > 0 {
		w = windows[0]
	}
	return &Service{store: s, windows: w}
}

// CreateMigration creates a new migration record.
func (s *Service) CreateMigration(ctx context.Context, req CreateMigrationRequest) (*model.Migration, error) {
	if len(req.SourceServers) == 0 {
		return nil, fmt.Errorf("%w: at least one source server is required", model.ErrInvalid)
	}
	if req.TargetServer == "" {
		return nil, fmt.Errorf("%w: target server is required", model.ErrInvalid)
	}

	// Validate source servers exist.
	for _, srcID := range req.SourceServers {
		if _, err := s.store.GetServer(ctx, srcID); err != nil {
			return nil, fmt.Errorf("source server %s: %w", srcID, err)
		}
	}

	// Validate target server exists.
	if _, err := s.store.GetServer(ctx, req.TargetServer); err != nil {
		return nil, fmt.Errorf("target server %s: %w", req.TargetServer, err)
	}

	now := time.Now()

	// Linkage: auto-create maintenance windows on source servers so they
	// enter protection during the migration. Fail-closed: if windows can't
	// be created, the migration record is not written (sources would be
	// unprotected).
	if s.windows != nil {
		if err := s.windows.CreateWindows(ctx, req.SourceServers, now, now.Add(DefaultMigrationWindowDuration)); err != nil {
			return nil, fmt.Errorf("create maintenance windows: %w", err)
		}
	}

	m := &model.Migration{
		ID:            fmt.Sprintf("mig-%d", now.UnixNano()),
		SourceServers: req.SourceServers,
		TargetServer:  req.TargetServer,
		Status:        model.MigrationPending,
		StartedAt:     now,
	}

	if err := s.store.CreateMigration(ctx, m); err != nil {
		return nil, fmt.Errorf("create migration: %w", err)
	}

	return m, nil
}

// GetMigration returns a migration by ID.
func (s *Service) GetMigration(ctx context.Context, id string) (*model.Migration, error) {
	m, err := s.store.GetMigration(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get migration: %w", err)
	}
	return m, nil
}

// RollbackMigration transitions a migration to rolled_back status.
// Only migrations in pending, migrating, or failed status can be rolled back.
func (s *Service) RollbackMigration(ctx context.Context, id string) error {
	m, err := s.store.GetMigration(ctx, id)
	if err != nil {
		return fmt.Errorf("rollback migration: %w", err)
	}

	switch m.Status {
	case model.MigrationPending, model.MigrationMigrating, model.MigrationFailed:
		// OK to rollback.
	default:
		return fmt.Errorf("%w: cannot rollback migration in status %q", model.ErrInvalid, m.Status)
	}

	return s.store.UpdateMigrationStatus(ctx, id, model.MigrationRolledBack, nil)
}

// ListMigrations returns recent migrations.
func (s *Service) ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error) {
	migrations, err := s.store.ListMigrations(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	return migrations, nil
}

var _ Store = store.Store(nil)
