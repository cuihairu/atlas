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

// ── Realms & Shards (TODO v0.1.14) ──────────────────────────────

// CreateRealmRequest is the DTO for creating a realm.
type CreateRealmRequest struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"region"`
	Status string `json:"status"`
}

// CreateShardRequest is the DTO for creating a shard.
type CreateShardRequest struct {
	ID      string `json:"id"`
	RealmID string `json:"realm_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
}

// CreateRealm inserts a new realm. Name and ID are required; status defaults
// to "active". Returns model.ErrConflict when the ID already exists.
func (s *Service) CreateRealm(ctx context.Context, req CreateRealmRequest) (*model.Realm, error) {
	if req.ID == "" || req.Name == "" {
		return nil, fmt.Errorf("%w: realm id and name are required", model.ErrInvalid)
	}
	status := req.Status
	if status == "" {
		status = "active"
	}
	r := &model.Realm{ID: req.ID, Name: req.Name, Region: req.Region, Status: status}
	if err := s.store.CreateRealm(ctx, r); err != nil {
		return nil, fmt.Errorf("create realm %s: %w", req.ID, err)
	}
	return r, nil
}

// ListRealms returns realms ordered by creation time descending.
func (s *Service) ListRealms(ctx context.Context, limit int) ([]*model.Realm, error) {
	realms, err := s.store.ListRealms(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list realms: %w", err)
	}
	return realms, nil
}

// CreateShard inserts a new shard under an existing realm. Name, ID and
// realm_id are required; the realm must exist (model.ErrNotFound otherwise).
func (s *Service) CreateShard(ctx context.Context, req CreateShardRequest) (*model.Shard, error) {
	if req.ID == "" || req.Name == "" || req.RealmID == "" {
		return nil, fmt.Errorf("%w: shard id, realm_id and name are required", model.ErrInvalid)
	}
	if _, err := s.store.GetRealm(ctx, req.RealmID); err != nil {
		return nil, fmt.Errorf("create shard %s: %w", req.ID, err)
	}
	status := req.Status
	if status == "" {
		status = "active"
	}
	sh := &model.Shard{ID: req.ID, RealmID: req.RealmID, Name: req.Name, Status: status}
	if err := s.store.CreateShard(ctx, sh); err != nil {
		return nil, fmt.Errorf("create shard %s: %w", req.ID, err)
	}
	return sh, nil
}

// ListShards returns shards ordered by creation time descending, optionally
// narrowed to a single realm.
func (s *Service) ListShards(ctx context.Context, realmID string, limit int) ([]*model.Shard, error) {
	shards, err := s.store.ListShards(ctx, realmID, limit)
	if err != nil {
		return nil, fmt.Errorf("list shards: %w", err)
	}
	return shards, nil
}

// ── Maintenance windows & announcements (TODO v0.1.20) ──────────

// CreateMaintenanceWindowRequest is the DTO for scheduling a maintenance
// window. Announce defaults to true: a window creates a matching server-
// scoped announcement unless explicitly disabled.
type CreateMaintenanceWindowRequest struct {
	StartAt  time.Time `json:"start_at"`
	EndAt    time.Time `json:"end_at"`
	Announce *bool     `json:"announce,omitempty"`
}

// CreateAnnouncementRequest is the DTO for creating an announcement. A nil
// ServerID means global.
type CreateAnnouncementRequest struct {
	ServerID *string   `json:"server_id,omitempty"`
	Title    string    `json:"title"`
	Body     string    `json:"body,omitempty"`
	Level    string    `json:"level,omitempty"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

// CreateMaintenanceWindow schedules a maintenance window for a server. The
// health monitor applies it automatically when it opens (docs/lifecycle.md
// §5). With announce (default true) a warning-level announcement covering
// the window is created and linked.
func (s *Service) CreateMaintenanceWindow(ctx context.Context, serverID string, req CreateMaintenanceWindowRequest) (*model.MaintenanceWindow, error) {
	if _, err := s.store.GetServer(ctx, serverID); err != nil {
		return nil, fmt.Errorf("maintenance window for unknown server %s: %w", serverID, err)
	}

	now := time.Now()
	// An omitted start_at means "now" — the common "maintenance tonight until
	// done" flow.
	startAt := req.StartAt
	if startAt.IsZero() {
		startAt = now
	}
	w := &model.MaintenanceWindow{
		ID:       fmt.Sprintf("mwin-%d", now.UnixNano()),
		ServerID: serverID,
		StartAt:  startAt,
		EndAt:    req.EndAt,
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}

	if req.Announce == nil || *req.Announce {
		a := &model.Announcement{
			ID:       fmt.Sprintf("ann-%d", now.UnixNano()),
			ServerID: &serverID,
			Title:    fmt.Sprintf("Maintenance scheduled: %s", serverID),
			Body: fmt.Sprintf("Server %s will be under maintenance from %s to %s.",
				serverID, w.StartAt.UTC().Format(time.RFC3339), w.EndAt.UTC().Format(time.RFC3339)),
			Level:    model.AnnouncementWarning,
			StartsAt: w.StartAt,
			EndsAt:   w.EndAt,
		}
		if err := s.store.CreateAnnouncement(ctx, a); err != nil {
			return nil, fmt.Errorf("create maintenance announcement: %w", err)
		}
		w.AnnouncementID = &a.ID
	}

	if err := s.store.CreateMaintenanceWindow(ctx, w); err != nil {
		return nil, fmt.Errorf("create maintenance window: %w", err)
	}
	return w, nil
}

// ListMaintenanceWindows returns windows, newest first. A non-empty serverID
// narrows to that server.
func (s *Service) ListMaintenanceWindows(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error) {
	return s.store.ListMaintenanceWindows(ctx, serverID, limit)
}

// DeleteMaintenanceWindow cancels a scheduled window. A window already in
// progress keeps the server in maintenance — use the normal enable/maintenance
// transitions to move it out.
func (s *Service) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	return s.store.DeleteMaintenanceWindow(ctx, id)
}

// CreateAnnouncement publishes a client-facing announcement.
func (s *Service) CreateAnnouncement(ctx context.Context, req CreateAnnouncementRequest) (*model.Announcement, error) {
	if req.ServerID != nil && *req.ServerID != "" {
		if _, err := s.store.GetServer(ctx, *req.ServerID); err != nil {
			return nil, fmt.Errorf("announcement for unknown server %s: %w", *req.ServerID, err)
		}
	}
	level := req.Level
	if level == "" {
		level = model.AnnouncementInfo
	}
	a := &model.Announcement{
		ID:       fmt.Sprintf("ann-%d", time.Now().UnixNano()),
		ServerID: req.ServerID,
		Title:    req.Title,
		Body:     req.Body,
		Level:    level,
		StartsAt: req.StartsAt,
		EndsAt:   req.EndsAt,
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if err := s.store.CreateAnnouncement(ctx, a); err != nil {
		return nil, fmt.Errorf("create announcement: %w", err)
	}
	return a, nil
}

// ListAnnouncements returns announcements matching the filter.
func (s *Service) ListAnnouncements(ctx context.Context, f store.AnnouncementFilter) ([]*model.Announcement, error) {
	return s.store.ListAnnouncements(ctx, f)
}

// DeleteAnnouncement removes an announcement.
func (s *Service) DeleteAnnouncement(ctx context.Context, id string) error {
	return s.store.DeleteAnnouncement(ctx, id)
}

// ── Server tags (标记体系) ─────────────────────────────────────────

// AddServerTagRequest is the DTO for adding (or replacing) one tag on a
// server. Public is a pointer so "omitted" can differ from "false": an
// omitted field inherits the preset's default visibility (true for presets,
// false for custom codes).
type AddServerTagRequest struct {
	Code   string        `json:"code"`
	Label  string        `json:"label,omitempty"`
	Tier   model.TagTier `json:"tier,omitempty"`
	Public *bool         `json:"public,omitempty"`
}

// GetServerTags returns a server's full tag list (public and internal — the
// admin API is the only surface that shows internal tags).
func (s *Service) GetServerTags(ctx context.Context, serverID string) ([]model.ServerTag, error) {
	srv, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	if srv.Tags == nil {
		return []model.ServerTag{}, nil
	}
	return srv.Tags, nil
}

// AddServerTag upserts one tag on a server (by code): preset defaults are
// filled in, the resulting full list is validated, then persisted. Re-adding
// an existing code replaces that tag.
func (s *Service) AddServerTag(ctx context.Context, serverID string, req AddServerTagRequest) ([]model.ServerTag, error) {
	srv, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("add tag: %w", err)
	}

	tag := model.FillTag(model.ServerTag{
		Code:   req.Code,
		Label:  req.Label,
		Tier:   req.Tier,
	}, req.Public)

	merged := make([]model.ServerTag, 0, len(srv.Tags)+1)
	replaced := false
	for _, t := range srv.Tags {
		if t.Code == tag.Code {
			merged = append(merged, tag)
			replaced = true
			continue
		}
		merged = append(merged, t)
	}
	if !replaced {
		merged = append(merged, tag)
	}

	if err := model.ValidateServerTags(merged); err != nil {
		return nil, err
	}
	if err := s.store.UpdateServerTags(ctx, serverID, merged); err != nil {
		return nil, fmt.Errorf("add tag: %w", err)
	}
	return merged, nil
}

// RemoveServerTag removes the tag with the given code from a server.
// ErrNotFound when either the server or the tag is missing.
func (s *Service) RemoveServerTag(ctx context.Context, serverID, code string) error {
	srv, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("remove tag: %w", err)
	}
	if !model.HasTag(srv.Tags, code) {
		return fmt.Errorf("%w: no tag %q on server %s", model.ErrNotFound, code, serverID)
	}

	kept := make([]model.ServerTag, 0, len(srv.Tags))
	for _, t := range srv.Tags {
		if t.Code != code {
			kept = append(kept, t)
		}
	}
	if err := s.store.UpdateServerTags(ctx, serverID, kept); err != nil {
		return fmt.Errorf("remove tag: %w", err)
	}
	return nil
}
