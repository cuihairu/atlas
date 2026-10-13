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

// Service provides migration orchestration.
type Service struct {
	store Store
}

// New creates a new migration service.
func New(s Store) *Service {
	return &Service{store: s}
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
