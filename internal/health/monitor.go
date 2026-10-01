// Package health implements the automatic server health monitor described in
// docs/lifecycle.md §4 (automatic offline detection).
package health

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cuihairu/atlas/internal/metrics"
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
	metrics      *metrics.Metrics
	alerter      *Alerter
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

// WithMetrics attaches Prometheus instrumentation (optional).
func (m *Monitor) WithMetrics(mm *metrics.Metrics) *Monitor {
	m.metrics = mm
	return m
}

// WithAlerts attaches fleet-level ratio alerting (optional, TODO v0.1.15).
func (m *Monitor) WithAlerts(a *Alerter) *Monitor {
	m.alerter = a
	return m
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

	// Scheduled maintenance windows (TODO v0.1.20) run before the stale-
	// heartbeat pass: entering a window moves the server to maintenance,
	// which the pass below then leaves alone (operator-set status).
	if err := m.applyMaintenanceWindows(ctx, servers, now); err != nil {
		m.logger.Error("maintenance window application failed", "error", err)
	}

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
				m.metrics.ObserveHeartbeatLag(age)
				if age > m.offlineAfter {
					if err := m.transition(ctx, srv, model.StatusOffline, "no heartbeat since creation (age %v)", age); err != nil {
						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
					}
				}
			}
			continue
		}

		age := now.Sub(rt.LastSeenAt)
		m.metrics.ObserveHeartbeatLag(age)

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

	// Fleet-level alerting on the post-sweep status distribution.
	if m.alerter != nil {
		total, suspect, offline := countStatuses(servers)
		m.alerter.Evaluate(ctx, total, suspect, offline)
	}

	return nil
}

func (m *Monitor) transition(ctx context.Context, srv *model.Server, newStatus model.ServerStatus, reasonFmt string, args ...any) error {
	m.logger.Warn(fmt.Sprintf(reasonFmt, args...),
		"server_id", srv.ID,
		"from", srv.Status,
		"to", newStatus,
	)
	m.metrics.CountHealthTransition(srv.Status, newStatus)
	srv.Status = newStatus // keep the in-memory copy fresh for alert counting
	return m.store.UpdateServerStatus(ctx, srv.ID, newStatus)
}

// applyMaintenanceWindows applies due maintenance windows (TODO v0.1.20):
//
//   - A window that has ended restores the server's pre-window status — but
//     only if the server is still in maintenance where the window left it;
//     an operator's move in the meantime wins. The window is then deleted.
//   - A window that just opened moves an auto-managed server
//     (starting/online/suspect) to maintenance and records what to restore.
//     Servers in operator-owned states (draining/maintenance/disabled) or
//     offline are left alone; the window is marked applied with an empty
//     previous status so the monitor does not retry every sweep.
func (m *Monitor) applyMaintenanceWindows(ctx context.Context, servers []*model.Server, now time.Time) error {
	windows, err := m.store.ListMaintenanceWindows(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("list maintenance windows: %w", err)
	}
	if len(windows) == 0 {
		return nil
	}

	byID := make(map[string]*model.Server, len(servers))
	for _, srv := range servers {
		byID[srv.ID] = srv
	}

	for _, w := range windows {
		srv := byID[w.ServerID]
		switch {
		case !now.Before(w.EndAt):
			// Window over: restore, then drop the window.
			if srv != nil && w.PreviousStatus != "" && srv.Status == model.StatusMaintenance {
				if err := m.transition(ctx, srv, w.PreviousStatus,
					"maintenance window %s ended, restoring status", w.ID); err != nil {
					m.logger.Error("failed to exit maintenance window",
						"window_id", w.ID, "server_id", w.ServerID, "error", err)
					continue // keep the window so the restore can be retried
				}
			}
			if err := m.store.DeleteMaintenanceWindow(ctx, w.ID); err != nil {
				m.logger.Warn("failed to delete expired maintenance window",
					"window_id", w.ID, "error", err)
			}
		case w.Active(now) && w.PreviousStatus == "":
			// Window open, not applied yet.
			previous := model.ServerStatus("")
			if srv != nil && srv.Status.AutoManaged() {
				previous = srv.Status
				if err := m.transition(ctx, srv, model.StatusMaintenance,
					"maintenance window %s started", w.ID); err != nil {
					m.logger.Error("failed to enter maintenance window",
						"window_id", w.ID, "server_id", w.ServerID, "error", err)
					continue // unapplied: retry next sweep
				}
			}
			if err := m.store.MarkMaintenanceWindowApplied(ctx, w.ID, previous); err != nil {
				m.logger.Error("failed to mark maintenance window applied",
					"window_id", w.ID, "error", err)
			}
		}
	}
	return nil
}

// countStatuses tallies monitor-owned servers by post-sweep status for
// alerting. The denominator includes offline servers — the monitor took them
// down — but excludes operator-set statuses (maintenance / disabled) that it
// does not manage.
func countStatuses(servers []*model.Server) (total, suspect, offline int) {
	for _, srv := range servers {
		switch srv.Status {
		case model.StatusStarting, model.StatusOnline, model.StatusSuspect, model.StatusOffline:
			total++
			switch srv.Status {
			case model.StatusSuspect:
				suspect++
			case model.StatusOffline:
				offline++
			}
		}
	}
	return total, suspect, offline
}
