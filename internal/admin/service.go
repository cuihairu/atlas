// Package admin implements the administrative operations described in
// docs/api.md §Admin: server state management, stats, character search,
// and migration orchestration.
package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// CreateMigrationRequest is the DTO for creating a migration.
type CreateMigrationRequest struct {
	SourceServers []string `json:"source_servers"`
	TargetServer  string   `json:"target_server"`
}

// Service provides administrative operations.
type Service struct {
	store store.Store
}

// New creates a new admin service.
func New(s store.Store) *Service {
	return &Service{store: s}
}

// SetMaintenance transitions a server into maintenance state.
// Only servers that are online, starting, or already in maintenance can be set
// to maintenance.
func (s *Service) SetMaintenance(ctx context.Context, serverID string) error {
	srv, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("set maintenance: %w", err)
	}

	switch srv.Status {
	case model.StatusOnline, model.StatusStarting, model.StatusMaintenance:
		// OK to transition.
	default:
		return fmt.Errorf("%w: cannot set maintenance from status %q", model.ErrInvalid, srv.Status)
	}

	return s.store.UpdateServerStatus(ctx, serverID, model.StatusMaintenance)
}

// SetDrain transitions a server into draining state.
// Only servers that are online, suspect, or already draining can be set
// to drain.
func (s *Service) SetDrain(ctx context.Context, serverID string) error {
	srv, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("set drain: %w", err)
	}

	switch srv.Status {
	case model.StatusOnline, model.StatusSuspect, model.StatusDraining:
		// OK to transition.
	default:
		return fmt.Errorf("%w: cannot set drain from status %q", model.ErrInvalid, srv.Status)
	}

	return s.store.UpdateServerStatus(ctx, serverID, model.StatusDraining)
}

// Enable transitions a server back to online from maintenance or disabled.
func (s *Service) Enable(ctx context.Context, serverID string) error {
	srv, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("enable: %w", err)
	}

	switch srv.Status {
	case model.StatusMaintenance, model.StatusDisabled:
		// OK to transition.
	default:
		return fmt.Errorf("%w: cannot enable from status %q", model.ErrInvalid, srv.Status)
	}

	return s.store.UpdateServerStatus(ctx, serverID, model.StatusOnline)
}

// Disable transitions a server to disabled and removes its runtime data.
func (s *Service) Disable(ctx context.Context, serverID string) error {
	// Verify the server exists.
	if _, err := s.store.GetServer(ctx, serverID); err != nil {
		return fmt.Errorf("disable: %w", err)
	}

	if err := s.store.UpdateServerStatus(ctx, serverID, model.StatusDisabled); err != nil {
		return fmt.Errorf("disable: %w", err)
	}

	// Best-effort cleanup of runtime data.
	_ = s.store.DeleteRuntime(ctx, serverID)

	return nil
}

// GetStats returns aggregated server and character statistics.
func (s *Service) GetStats(ctx context.Context) (*model.Stats, error) {
	stats, err := s.store.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}
	return stats, nil
}

// SearchCharacters searches characters with the given filter.
func (s *Service) SearchCharacters(ctx context.Context, filter store.CharacterSearchFilter) ([]*model.Character, string, error) {
	chars, nextCursor, err := s.store.SearchCharacters(ctx, filter)
	if err != nil {
		return nil, "", fmt.Errorf("search characters: %w", err)
	}
	return chars, nextCursor, nil
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