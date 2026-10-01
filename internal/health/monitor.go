// Package health implements the automatic server health monitor described in
// docs/lifecycle.md §4 (automatic offline detection).
package health

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Monitor periodically sweeps all auto-managed servers and advances their
// lifecycle state based on heartbeat age.
type Monitor struct {
	store        store.Store
	suspectAfter time.Duration
	offlineAfter time.Duration
	interval     time.Duration
	logger       *slog.Logger
	now          func() time.Time // injectable clock for testing
}

// New creates a new health monitor.
func New(s store.Store, suspectAfter, offlineAfter, interval time.Duration, logger *slog.Logger) *Monitor {
	return &Monitor{
		store:        s,
		suspectAfter: suspectAfter,
		offlineAfter: offlineAfter,
		interval:     interval,
		logger:       logger,
		now:          time.Now,
	}
}

// Run starts the monitor loop. It blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	m.logger.Info("health monitor started",
		"suspect_after", m.suspectAfter,
		"offline_after", m.offlineAfter,
		"interval", m.interval,
	)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("health monitor stopped")
			return
		case <-ticker.C:
			if err := m.sweep(ctx); err != nil {
				m.logger.Error("health sweep failed", "error", err)
			}
		}
	}
}

// sweep checks all auto-managed servers and transitions their status if
// heartbeats have aged past the configured thresholds.
func (m *Monitor) sweep(ctx context.Context) error {
	// List all servers regardless of status.
	servers, err := m.store.ListServers(ctx, store.ServerFilter{})
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}

	now := m.now()

	for _, srv := range servers {
		if !srv.Status.AutoManaged() {
			continue
		}

		rt, err := m.store.GetRuntime(ctx, srv.ID)
		if err != nil {
			// No runtime data; this server has never sent a heartbeat.
			// If it's been starting for too long, mark offline.
			if srv.Status == model.StatusStarting {
				age := now.Sub(srv.CreatedAt)
				if age > m.offlineAfter {
					if err := m.transition(ctx, srv, model.StatusOffline, "no heartbeat since creation (age %v)", age); err != nil {
						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
					}
				}
			}
			continue
		}

		age := now.Sub(rt.LastSeenAt)

		switch srv.Status {
		case model.StatusStarting, model.StatusOnline:
			if age > m.offlineAfter {
				if err := m.transition(ctx, srv, model.StatusOffline, "heartbeat age %v exceeds offline threshold %v", age, m.offlineAfter); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				}
			} else if age > m.suspectAfter {
				if err := m.transition(ctx, srv, model.StatusSuspect, "heartbeat age %v exceeds suspect threshold %v", age, m.suspectAfter); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				}
			}

		case model.StatusSuspect:
			if age > m.offlineAfter {
				if err := m.transition(ctx, srv, model.StatusOffline, "heartbeat age %v exceeds offline threshold %v", age, m.offlineAfter); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				}
			} else if age <= m.suspectAfter {
				// Heartbeat recovered.
				if err := m.transition(ctx, srv, model.StatusOnline, "heartbeat recovered (age %v)", age); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				}
			}
		}
	}

	return nil
}

func (m *Monitor) transition(ctx context.Context, srv *model.Server, newStatus model.ServerStatus, reasonFmt string, args ...any) error {
	m.logger.Warn(fmt.Sprintf(reasonFmt, args...),
		"server_id", srv.ID,
		"from", srv.Status,
		"to", newStatus,
	)
	return m.store.UpdateServerStatus(ctx, srv.ID, newStatus)
}