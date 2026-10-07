// Package registry implements the server registration, heartbeat, and
// unregistration service described in docs/api.md §Registry.
package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
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
	// NotifyMode / NotifyCallbackURL declare how this server receives
	// cross-server config change signals (config center): subscribe |
	// callback | poll. Callback mode requires an absolute http(s) URL
	// atlas POSTs the change signal to. See docs/config-center.md.
	NotifyMode        string `json:"notify_mode,omitempty"`
	NotifyCallbackURL string `json:"notify_callback_url,omitempty"`
	// Players seeds the initial player count (runtime data), so a server that
	// re-registers mid-session (Atlas restart) does not report 0 players
	// until its next heartbeat. TODO v0.1.20.
	Players int `json:"players,omitempty"`
	// StartedAt is the game server process start time. Zero means "now" —
	// the registration time is used. TODO v0.1.20.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// Metadata carries arbitrary key-value pairs (engine hints, cluster /
	// zone / language markers — see model.Server). Re-registration replaces
	// the whole map: undeclared keys are dropped, mirroring the profile
	// upsert semantics of the other mutable fields.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Service manages server registration and heartbeats.
type Service struct {
	servers store.ServerStore
	runtime store.RuntimeStore
	logger  *slog.Logger
	// maintenanceEnforce is the 维护中 policy for registration gating
	// ("block" rejects 创角, "warn" allows it with a notice). See
	// WithMaintenanceEnforce.
	maintenanceEnforce string
	// metrics records registry write-path latency (register/heartbeat/
	// unregister); nil means unobserved (tests, embedded use).
	metrics *metrics.Metrics
}

// New creates a new registry service.
func New(servers store.ServerStore, runtime store.RuntimeStore, logger *slog.Logger) *Service {
	return &Service{
		servers: servers,
		runtime: runtime,
		logger:  logger,
	}
}

// WithMetrics attaches Prometheus instrumentation (optional). Registry
// write-path latency is observed per operation — see metrics.RegistryWrites.
func (s *Service) WithMetrics(m *metrics.Metrics) *Service {
	s.metrics = m
	return s
}

// MaintenanceEnforceBlock / MaintenanceEnforceWarn are the accepted values
// of ATLAS_MAINTENANCE_ENFORCE.
const (
	MaintenanceEnforceBlock = "block"
	MaintenanceEnforceWarn  = "warn"
)

// WithMaintenanceEnforce sets the 维护中 policy used by CheckRegistration:
// "block" (default) rejects character creation on servers in maintenance,
// "warn" lets it through so the caller can attach a notice. Any other value
// is treated as "block" — fail closed.
func (s *Service) WithMaintenanceEnforce(mode string) *Service {
	s.maintenanceEnforce = mode
	return s
}

// RegistrationVerdict is the outcome of registration gating for one request.
type RegistrationVerdict struct {
	// Code is the API error code to reject with; empty means "allowed".
	Code string
	// Message is the human-readable rejection reason for Code.
	Message string
	// Warn is set when the server is in maintenance but the enforce mode is
	// "warn": the request proceeds, and the caller should attach a notice
	// (e.g. an HTTP Warning header) so operators see the soft rejection.
	Warn bool
}

// CheckRegistration evaluates registration gating (server tags) for a
// new-character request against serverID. An unknown server is not a gate:
// character projections may outlive server records, so ErrNotFound yields an
// empty (allow) verdict; any other store error propagates.
//
// The gate honours the 禁止注册 tag unconditionally and the 维护中 tag or
// maintenance status per the configured enforce mode.
func (s *Service) CheckRegistration(ctx context.Context, serverID string) (RegistrationVerdict, error) {
	srv, err := s.servers.GetServer(ctx, serverID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return RegistrationVerdict{}, nil
		}
		return RegistrationVerdict{}, fmt.Errorf("check registration for %s: %w", serverID, err)
	}
	code, message := srv.RegistrationBlocked(s.maintenanceEnforce == MaintenanceEnforceWarn)
	return RegistrationVerdict{
		Code:    code,
		Message: message,
		Warn:    code == "" && s.maintenanceEnforce == MaintenanceEnforceWarn && srv.InMaintenance(),
	}, nil
}

// Register registers a game server. It is idempotent: re-registering the same
// server ID updates its fields rather than returning ErrConflict.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*model.Server, error) {
	t0 := time.Now()
	defer func() { s.metrics.ObserveRegistryWrite("register", time.Since(t0)) }()
	ctx, span := atlastracing.Start(ctx, "registry.register")
	defer span.End()

	startedAt := time.Now()
	if req.StartedAt != nil {
		startedAt = *req.StartedAt
	}
	srv := &model.Server{
		ID:                req.ID,
		Name:              req.Name,
		Type:              req.Type,
		Region:            req.Region,
		RealmID:           req.RealmID,
		ShardID:           req.ShardID,
		Version:           req.Version,
		Platform:          req.Platform,
		Endpoint:          req.Endpoint,
		Capacity:          req.Capacity,
		NotifyMode:        req.NotifyMode,
		NotifyCallbackURL: req.NotifyCallbackURL,
		Metadata:          req.Metadata,
		StartedAt:         &startedAt,
		Status:            model.StatusStarting,
	}

	if err := srv.Validate(); err != nil {
		return nil, err
	}

	// Config-managed servers (ATLAS_SERVERS_CONFIG) reject API registration:
	// their profile fields are owned by the config file, so an API update
	// would be silently reverted at the next Atlas restart. Heartbeats and
	// lifecycle operations remain the way a declared server participates.
	if existing, err := s.servers.GetServer(ctx, req.ID); err == nil {
		if existing.Source == "config" {
			return nil, fmt.Errorf("%w: server %s is managed by the servers config; update the declaration there (heartbeat is still accepted)", model.ErrConflict, req.ID)
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("lookup server %s: %w", req.ID, err)
	}

	if err := s.servers.RegisterServer(ctx, srv); err != nil {
		return nil, fmt.Errorf("register server: %w", err)
	}

	// Record an initial heartbeat so runtime data is available immediately.
	hb := model.Heartbeat{
		Players: req.Players,
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
// Heartbeat records a heartbeat and returns the server's effective status
// afterwards, so callers can tell online from suspect / offline (an offline
// server must re-register to re-enter rotation).
func (s *Service) Heartbeat(ctx context.Context, serverID string, hb model.Heartbeat) (model.ServerStatus, error) {
	t0 := time.Now()
	defer func() { s.metrics.ObserveRegistryWrite("heartbeat", time.Since(t0)) }()
	ctx, span := atlastracing.Start(ctx, "registry.heartbeat")
	defer span.End()

	if err := hb.Validate(); err != nil {
		return "", err
	}

	// Verify the server exists.
	srv, err := s.servers.GetServer(ctx, serverID)
	if err != nil {
		return "", fmt.Errorf("heartbeat for unknown server %s: %w", serverID, err)
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
		return "", fmt.Errorf("record heartbeat: %w", err)
	}

	// Auto-promote starting → online on first valid heartbeat.
	if srv.Status == model.StatusStarting {
		if err := s.servers.UpdateServerStatus(ctx, serverID, model.StatusOnline); err != nil {
			return "", fmt.Errorf("promote to online: %w", err)
		}
		s.logger.Info("server promoted to online", "server_id", serverID)
		return model.StatusOnline, nil
	}

	// Config-managed servers (ATLAS_SERVERS_CONFIG) cannot re-register — the
	// register API rejects them — so a valid heartbeat is their proof of
	// life: it re-enters rotation from dead-ish states the same way a
	// re-register does for API servers. Without this, a declared server
	// aged offline before its instance booted could never come back.
	if srv.Source == "config" && (srv.Status == model.StatusSuspect || srv.Status == model.StatusOffline) {
		if err := s.servers.UpdateServerStatus(ctx, serverID, model.StatusOnline); err != nil {
			return "", fmt.Errorf("promote to online: %w", err)
		}
		s.logger.Info("config-managed server re-entered rotation", "server_id", serverID, "from", srv.Status)
		return model.StatusOnline, nil
	}

	return srv.Status, nil
}

// Unregister marks a server as offline and removes its runtime data.
func (s *Service) Unregister(ctx context.Context, serverID string) error {
	t0 := time.Now()
	defer func() { s.metrics.ObserveRegistryWrite("unregister", time.Since(t0)) }()
	ctx, span := atlastracing.Start(ctx, "registry.unregister")
	defer span.End()

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
