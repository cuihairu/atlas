// Package registry implements the server registration, heartbeat, and
// unregistration service described in docs/api.md §Registry.
package registry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// RegisterRequest is the DTO for server registration. It mirrors model.Server
// fields but is separate so the handler layer can decode JSON independently.
type RegisterRequest struct {
	ID       string         `json:"server_id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Region   string         `json:"region"`
	RealmID  *string        `json:"realm_id,omitempty"`
	ShardID  *string        `json:"shard_id,omitempty"`
	Version  string         `json:"version"`
	Platform string         `json:"platform"`
	Endpoint model.Endpoint `json:"endpoint"`
	Capacity int            `json:"capacity"`
}

// Service manages server registration and heartbeats.
type Service struct {
	servers store.ServerStore
	runtime store.RuntimeStore
	logger  *slog.Logger
}

// New creates a new registry service.
func New(servers store.ServerStore, runtime store.RuntimeStore, logger *slog.Logger) *Service {
	return &Service{
		servers: servers,
		runtime: runtime,
		logger:  logger,
	}
}

// Register registers a game server. It is idempotent: re-registering the same
// server ID updates its fields rather than returning ErrConflict.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*model.Server, error) {
	srv := &model.Server{
		ID:       req.ID,
		Name:     req.Name,
		Type:     req.Type,
		Region:   req.Region,
		RealmID:  req.RealmID,
		ShardID:  req.ShardID,
		Version:  req.Version,
		Platform: req.Platform,
		Endpoint: req.Endpoint,
		Capacity: req.Capacity,
		Status:   model.StatusStarting,
	}

	if err := srv.Validate(); err != nil {
		return nil, err
	}

	if err := s.servers.RegisterServer(ctx, srv); err != nil {
		return nil, fmt.Errorf("register server: %w", err)
	}

	// Record an initial heartbeat so runtime data is available immediately.
	hb := model.Heartbeat{
		Players: 0,
		Load:    0,
		Status:  model.StatusStarting,
	}
	if err := s.runtime.RecordHeartbeat(ctx, srv.ID, hb); err != nil {
		s.logger.Warn("failed to record initial heartbeat", "server_id", srv.ID, "error", err)
	}

	s.logger.Info("server registered", "server_id", srv.ID, "region", srv.Region, "status", srv.Status)
	return srv, nil
}

// Heartbeat processes a heartbeat from a game server.
func (s *Service) Heartbeat(ctx context.Context, serverID string, hb model.Heartbeat) error {
	if err := hb.Validate(); err != nil {
		return err
	}

	// Verify the server exists.
	srv, err := s.servers.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("heartbeat for unknown server %s: %w", serverID, err)
	}

	// Determine the status to record.
	status := hb.Status
	if status == "" {
		status = srv.Status
	}

	runtimeHB := model.Heartbeat{
		Players: hb.Players,
		Load:    hb.Load,
		Status:  status,
	}
	if err := s.runtime.RecordHeartbeat(ctx, serverID, runtimeHB); err != nil {
		return fmt.Errorf("record heartbeat: %w", err)
	}

	// Auto-promote starting → online on first valid heartbeat.
	if srv.Status == model.StatusStarting {
		if err := s.servers.UpdateServerStatus(ctx, serverID, model.StatusOnline); err != nil {
			return fmt.Errorf("promote to online: %w", err)
		}
		s.logger.Info("server promoted to online", "server_id", serverID)
	}

	return nil
}

// Unregister marks a server as offline and removes its runtime data.
func (s *Service) Unregister(ctx context.Context, serverID string) error {
	// Verify existence.
	if _, err := s.servers.GetServer(ctx, serverID); err != nil {
		return fmt.Errorf("unregister unknown server %s: %w", serverID, err)
	}

	if err := s.servers.UpdateServerStatus(ctx, serverID, model.StatusOffline); err != nil {
		return fmt.Errorf("set offline: %w", err)
	}

	// Best-effort cleanup of runtime data.
	if err := s.runtime.DeleteRuntime(ctx, serverID); err != nil {
		s.logger.Warn("failed to delete runtime during unregister", "server_id", serverID, "error", err)
	}

	s.logger.Info("server unregistered", "server_id", serverID)
	return nil
}

// now is a package-level time function, replaceable in tests.
var now = time.Now