// Package admin implements the administrative operations described in
// docs/api.md §Admin: server state management, stats, and character search.
// Migration orchestration lives in its own package (internal/migration) per
// the boundary fixed in docs/migration.md §9; admin only delegates to it.
package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/atlas/internal/migration"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// CreateMigrationRequest is the DTO for creating a migration.
type CreateMigrationRequest = migration.CreateMigrationRequest

// Service provides administrative operations.
type Service struct {
	store     store.Store
	migration *migration.Service
}

// New creates a new admin service.
func New(s store.Store) *Service {
	return &Service{store: s, migration: migration.New(s)}
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
// See internal/migration: the orchestration state machine lives there.
func (s *Service) CreateMigration(ctx context.Context, req CreateMigrationRequest) (*model.Migration, error) {
	return s.migration.CreateMigration(ctx, req)
}

// GetMigration returns a migration by ID.
// See internal/migration: the orchestration state machine lives there.
func (s *Service) GetMigration(ctx context.Context, id string) (*model.Migration, error) {
	return s.migration.GetMigration(ctx, id)
}

// RollbackMigration transitions a migration to rolled_back status.
// Only migrations in pending, migrating, or failed status can be rolled back.
// See internal/migration: the orchestration state machine lives there.
func (s *Service) RollbackMigration(ctx context.Context, id string) error {
	return s.migration.RollbackMigration(ctx, id)
}

// ListMigrations returns recent migrations.
// See internal/migration: the orchestration state machine lives there.
func (s *Service) ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error) {
	return s.migration.ListMigrations(ctx, limit)
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

// BatchMaintenanceWindowRequest schedules one window template across a fleet
// of servers in a single call — the per-server flow
// (POST /v1/admin/servers/{id}/maintenance-window) turns a fleet-wide change
// into N hand-built requests.
type BatchMaintenanceWindowRequest struct {
	ServerIDs []string  `json:"server_ids"`
	StartAt   time.Time `json:"start_at"` // zero = now
	EndAt     time.Time `json:"end_at"`   // required
	// StaggerSeconds shifts server i's window (both ends, duration preserved)
	// by i*StaggerSeconds — rolling maintenance instead of taking the whole
	// fleet down at once. Zero = simultaneous.
	StaggerSeconds int `json:"stagger_seconds,omitempty"`
	// Announce mirrors the single-window default: true (or omitted) links a
	// warning-level announcement to each window.
	Announce *bool `json:"announce,omitempty"`
	// DryRun validates the whole plan (existence, ranges) and returns the
	// planned windows without creating anything — the preview step before a
	// fleet-wide change.
	DryRun bool `json:"dry_run,omitempty"`
}

// BatchWindowFailure pairs a server with the reason its window was not
// scheduled.
type BatchWindowFailure struct {
	ServerID string `json:"server_id"`
	Error    string `json:"error"`
}

// BatchMaintenanceWindowResult reports what the batch did. Under DryRun,
// Windows is the validated plan (ids are preview values) and nothing was
// persisted.
type BatchMaintenanceWindowResult struct {
	Windows []*model.MaintenanceWindow `json:"windows"`
	Failed  []BatchWindowFailure       `json:"failed,omitempty"`
}

// BatchCreateMaintenanceWindows schedules windows for a list of servers from
// one template. Template-level mistakes (empty ServerIDs, missing end_at,
// inverted range) fail the whole call; per-server problems (unknown server,
// store error) land in the result's Failed slice so one bad server does not
// roll back the rest — the stores expose no cross-server transaction.
func (s *Service) BatchCreateMaintenanceWindows(ctx context.Context, req BatchMaintenanceWindowRequest) (*BatchMaintenanceWindowResult, error) {
	if len(req.ServerIDs) == 0 {
		return nil, fmt.Errorf("%w: server_ids is empty", model.ErrInvalid)
	}
	if req.EndAt.IsZero() {
		return nil, fmt.Errorf("%w: end_at is required", model.ErrInvalid)
	}

	now := time.Now()
	startAt := req.StartAt
	if startAt.IsZero() {
		startAt = now
	}
	if !startAt.Before(req.EndAt) {
		return nil, fmt.Errorf("%w: maintenance window end_at must be after start_at", model.ErrInvalid)
	}
	announce := req.Announce == nil || *req.Announce
	// Captured once and suffixed per entry: now alone would give every
	// window (and its announcement) the same id inside the batch.
	base := now.UnixNano()

	result := &BatchMaintenanceWindowResult{}
	seen := make(map[string]bool, len(req.ServerIDs))
	for i, serverID := range req.ServerIDs {
		if seen[serverID] {
			continue
		}
		seen[serverID] = true
		if serverID == "" {
			result.Failed = append(result.Failed, BatchWindowFailure{ServerID: serverID, Error: "server_id is empty"})
			continue
		}

		offset := time.Duration(i) * time.Duration(req.StaggerSeconds) * time.Second
		w := &model.MaintenanceWindow{
			ID:       fmt.Sprintf("mwin-%d-%d", base, i),
			ServerID: serverID,
			StartAt:  startAt.Add(offset),
			EndAt:    req.EndAt.Add(offset),
		}
		if _, err := s.store.GetServer(ctx, serverID); err != nil {
			result.Failed = append(result.Failed, BatchWindowFailure{ServerID: serverID, Error: err.Error()})
			continue
		}
		if err := w.Validate(); err != nil {
			result.Failed = append(result.Failed, BatchWindowFailure{ServerID: serverID, Error: err.Error()})
			continue
		}
		if req.DryRun {
			result.Windows = append(result.Windows, w)
			continue
		}

		if announce {
			a := &model.Announcement{
				ID:       fmt.Sprintf("ann-%d-%d", base, i),
				ServerID: &serverID,
				Title:    fmt.Sprintf("Maintenance scheduled: %s", serverID),
				Body: fmt.Sprintf("Server %s will be under maintenance from %s to %s.",
					serverID, w.StartAt.UTC().Format(time.RFC3339), w.EndAt.UTC().Format(time.RFC3339)),
				Level:    model.AnnouncementWarning,
				StartsAt: w.StartAt,
				EndsAt:   w.EndAt,
			}
			if err := s.store.CreateAnnouncement(ctx, a); err != nil {
				result.Failed = append(result.Failed, BatchWindowFailure{ServerID: serverID, Error: err.Error()})
				continue
			}
			w.AnnouncementID = &a.ID
		}
		if err := s.store.CreateMaintenanceWindow(ctx, w); err != nil {
			result.Failed = append(result.Failed, BatchWindowFailure{ServerID: serverID, Error: err.Error()})
			continue
		}
		result.Windows = append(result.Windows, w)
	}
	return result, nil
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
		Code:  req.Code,
		Label: req.Label,
		Tier:  req.Tier,
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

// MinServerWeight / MaxServerWeight bound the operator routing weight
// (model.Server.Weight). 0 clears back to neutral; 100 is neutral.
const (
	MinServerWeight = 1
	MaxServerWeight = 1000
)

// SetServerWeight sets the operator routing weight used by the routing
// service to rank candidates on effective load (load × 100 / weight).
// 0 clears the weight (neutral); 1..1000 is the accepted range.
func (s *Service) SetServerWeight(ctx context.Context, serverID string, weight int) error {
	if weight < 0 || weight > MaxServerWeight {
		return fmt.Errorf("%w: weight %d out of range (0 to clear, or %d..%d)", model.ErrInvalid, weight, MinServerWeight, MaxServerWeight)
	}
	if err := s.store.UpdateServerWeight(ctx, serverID, weight); err != nil {
		return fmt.Errorf("set weight: %w", err)
	}
	return nil
}
