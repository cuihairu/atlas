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
	// List all servers regardless of status, cursor-paginated at the
	// store's list cap. A bare call takes the store's 50-row default
	// page: servers past it would never be checked, so they would never
	// transition to suspect/offline no matter how long they stay silent.
	var servers []*model.Server
	cursor := ""
	for {
		page, err := m.store.ListServers(ctx, store.ServerFilter{Limit: store.ListServersMaxLimit, Cursor: cursor})
		if err != nil {
			return fmt.Errorf("list servers: %w", err)
		}
		servers = append(servers, page...)
		if len(page) < store.ListServersMaxLimit {
			break
		}
		cursor = page[len(page)-1].ID
	}

	now := m.now()

	// Scheduled maintenance windows (TODO v0.1.20) run before the stale-
	// heartbeat pass: entering a window moves the server to maintenance,
	// which the pass below then leaves alone (operator-set status).
	if err := m.applyMaintenanceWindows(ctx, servers, now); err != nil {
		m.logger.Error("maintenance window application failed", "error", err)
	}

	// Auto-managed servers only, then one batched runtime read for the
	// whole set (N single-key reads → 1–2 round trips, the same
	// GetRuntimes call Discovery's list path uses). An absent key means
	// the server has never sent a heartbeat; a store failure fails the
	// whole sweep — judging liveness on unknown state would mark live
	// servers offline.
	var autoManaged []*model.Server
	for _, srv := range servers {
		if srv.Status.AutoManaged() {
			autoManaged = append(autoManaged, srv)
		}
	}
	rtMap := map[string]model.Runtime{}
	if len(autoManaged) > 0 {
		ids := make([]string, len(autoManaged))
		for i, srv := range autoManaged {
			ids[i] = srv.ID
		}
		var err error
		rtMap, err = m.store.GetRuntimes(ctx, ids)
		if err != nil {
			return fmt.Errorf("read runtimes: %w", err)
		}
	}

	for _, srv := range autoManaged {
		rt, ok := rtMap[srv.ID]
		if !ok {
			// No runtime data; this server has never sent a heartbeat.
			// If it's been starting for too long, mark offline.
			if srv.Status == model.StatusStarting {
				age := now.Sub(srv.CreatedAt)
				m.metrics.ObserveHeartbeatLag(age)
				if age > m.offlineAfter {
					if applied, err := m.transition(ctx, srv, model.StatusOffline, "no heartbeat since creation (age %v)", age); err != nil {
						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
					} else if applied {
						m.alertPermanentOffline(ctx, srv, age)
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
				if applied, err := m.transition(ctx, srv, model.StatusOffline, "heartbeat age %v exceeds offline threshold %v", age, m.offlineAfter); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				} else if applied {
					m.alertPermanentOffline(ctx, srv, age)
				}
			} else if age > m.suspectAfter {
				if _, err := m.transition(ctx, srv, model.StatusSuspect, "heartbeat age %v exceeds suspect threshold %v", age, m.suspectAfter); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				}
			}

		case model.StatusSuspect:
			if age > m.offlineAfter {
				if applied, err := m.transition(ctx, srv, model.StatusOffline, "heartbeat age %v exceeds offline threshold %v", age, m.offlineAfter); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				} else if applied {
					m.alertPermanentOffline(ctx, srv, age)
				}
			} else if age <= m.suspectAfter {
				// Heartbeat recovered.
				if _, err := m.transition(ctx, srv, model.StatusOnline, "heartbeat recovered (age %v)", age); err != nil {
					m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
				}
			}
		}
	}

	// Temporary registrations expire on their own path — separate from the
	// status machine above and unreachable for any other class
	// (docs/lifecycle.md §3 注册生命周期). The lease counts from the
	// registration time (created_at + reg_ttl_seconds), so reclaiming needs
	// no runtime data and behaves identically on every store backend. Only
	// an explicit RegTemporary marker enters this loop: permanent and
	// legacy (unset) records can never be auto-deleted here.
	m.expireTemporary(ctx, servers, now)

	// Fleet-level alerting on the post-sweep status distribution.
	if m.alerter != nil {
		total, suspect, offline := countStatuses(servers)
		m.alerter.Evaluate(ctx, total, suspect, offline)
	}

	return nil
}

// expireTemporary reclaims temporary registrations whose lease has run out.
// The lease is absolute (registration time + reg_ttl_seconds), not a
// heartbeat-renewed one: reclaiming therefore needs no runtime reads and
// works the same on memory / PostgreSQL / MySQL. A failed reclaim retries
// on the next sweep.
func (m *Monitor) expireTemporary(ctx context.Context, servers []*model.Server, now time.Time) {
	for _, srv := range servers {
		if !srv.IsTemporary() {
			continue
		}
		ttl := time.Duration(srv.RegTTLSeconds) * time.Second
		if ttl <= 0 {
			continue
		}
		age := now.Sub(srv.CreatedAt)
		if age <= ttl {
			continue
		}
		if err := m.store.DeleteServer(ctx, srv.ID); err != nil {
			if store.IsNotFound(err) {
				continue // already gone; runtime cleanup still follows
			}
			m.logger.Error("temporary registration reclaim failed",
				"server_id", srv.ID, "ttl", ttl.String(), "age", age.String(), "error", err)
			continue
		}
		if err := m.store.DeleteRuntime(ctx, srv.ID); err != nil && !store.IsNotFound(err) {
			m.logger.Warn("temporary runtime reclaim failed", "server_id", srv.ID, "error", err)
		}
		m.logger.Info("temporary registration expired",
			"server_id", srv.ID, "ttl", ttl.String(), "age", age.String())
	}
}

// alertPermanentOffline reports a permanent registration that just left the
// auto-managed rotation for offline (docs/lifecycle.md §3 离线告警). The
// monitor only drives offline transitions for auto-managed statuses
// (starting / online / suspect) — operator-set maintenance never reaches
// this path, which is the 维修状态豁免 the alert contract requires.
func (m *Monitor) alertPermanentOffline(ctx context.Context, srv *model.Server, age time.Duration) {
	if m.alerter == nil {
		return
	}
	kind := srv.RegKind
	if kind == "" {
		kind = model.RegPermanent // legacy rows behave as permanent
	}
	if kind == model.RegTemporary {
		// A temporary registration leaving the fleet is expected lifecycle
		// (TTL reclaim) — only permanent registrations alert on offline.
		return
	}
	m.alerter.EmitServerOffline(ctx, ServerOfflineEvent{
		ServerID:    srv.ID,
		RegKind:     kind,
		OfflineFor:  age,
		Maintenance: false, // maintenance is operator-set and never transitions here
	})
}

// transition moves a server to newStatus through a compare-and-set guard:
// the sweep decides on a snapshot it listed earlier, and re-registration
// (or any other writer) can move the server between that read and this
// write — an unconditional status store would resurrect offline from a
// heartbeat that no longer exists, clobbering the fresh registration.
// When the guard misses, the newer writer wins: nothing is logged, counted,
// or mutated here, and the caller treats it as a no-op (applied=false),
// not an error. The local copy and the transition counter move only after
// the store accepted the write, so metrics never report a transition that
// did not happen.
func (m *Monitor) transition(ctx context.Context, srv *model.Server, newStatus model.ServerStatus, reasonFmt string, args ...any) (bool, error) {
	applied, err := m.store.UpdateServerStatusIf(ctx, srv.ID, srv.Status, newStatus)
	if err != nil || !applied {
		return applied, err
	}
	m.logger.Warn(fmt.Sprintf(reasonFmt, args...),
		"server_id", srv.ID,
		"from", srv.Status,
		"to", newStatus,
	)
	m.metrics.CountHealthTransition(srv.Status, newStatus)
	srv.Status = newStatus // keep the in-memory copy fresh for alert counting
	return true, nil
}

// applyMaintenanceWindows applies due maintenance windows (TODO v0.1.20):
//
//   - A window that has ended restores the server's pre-window status — but
//     only if the server is still in maintenance where the window left it;
//     an operator's move in the meantime wins. The window is then deleted.
//   - A window that just opened records the status to restore FIRST, then
//     moves an auto-managed server (starting/online/suspect) to
//     maintenance. Persisting the record before touching the server is the
//     invariant that prevents a stranded maintenance state: whichever
//     piece fails — the mark write, the transition, or the process itself
//     in between — the next sweep can always recover, because a recorded
//     previous status is never lost. Servers in operator-owned states
//     (draining/maintenance/disabled) or offline are left alone; the
//     window is marked applied with an empty previous status so the
//     monitor does not retry the move every sweep.
//   - A window that is active and applied but whose server was never moved
//     (the move failed last sweep, or the process crashed between the
//     record and the move) gets its move retried here, once per sweep,
//     while the server still holds the recorded previous status.
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
				applied, err := m.transition(ctx, srv, w.PreviousStatus,
					"maintenance window %s ended, restoring status", w.ID)
				if err != nil {
					m.logger.Error("failed to exit maintenance window",
						"window_id", w.ID, "server_id", w.ServerID, "error", err)
					continue // keep the window so the restore can be retried
				}
				if !applied {
					// The snapshot said maintenance but the live record
					// moved on (another writer intervened between the
					// list and the guarded write): their status wins,
					// so consume the window instead of retrying forever.
					m.logger.Info("maintenance window restore skipped, server moved on",
						"window_id", w.ID, "server_id", w.ServerID)
				}
			}
			if err := m.store.DeleteMaintenanceWindow(ctx, w.ID); err != nil {
				m.logger.Warn("failed to delete expired maintenance window",
					"window_id", w.ID, "error", err)
			}
		case w.Active(now) && w.PreviousStatus == "":
			// Window open, not applied yet. Record first.
			previous := model.ServerStatus("")
			if srv != nil && srv.Status.AutoManaged() {
				previous = srv.Status
			}
			if err := m.store.MarkMaintenanceWindowApplied(ctx, w.ID, previous); err != nil {
				m.logger.Error("failed to mark maintenance window applied",
					"window_id", w.ID, "error", err)
				continue // the server is untouched: retry the whole entry next sweep
			}
			w.PreviousStatus = previous // keep the in-memory copy consistent for this sweep
			if previous != "" {
				// The move may fail here (store outage, guarded against a
				// racing writer) — fine: the window is already marked with
				// the status to restore, and the pending-entry branch below
				// retries the move on subsequent sweeps.
				if _, err := m.transition(ctx, srv, model.StatusMaintenance,
					"maintenance window %s started", w.ID); err != nil {
					m.logger.Error("failed to enter maintenance window",
						"window_id", w.ID, "server_id", w.ServerID, "error", err)
				}
			}
		case w.Active(now):
			// Window active and recorded, server not moved yet: retry the
			// move while it still holds the recorded previous status. An
			// operator-set state (or the already-moved maintenance state)
			// never matches the guard, so this branch cannot resurrect
			// anything it should leave alone.
			if srv != nil && srv.Status == w.PreviousStatus {
				if _, err := m.transition(ctx, srv, model.StatusMaintenance,
					"maintenance window %s started", w.ID); err != nil {
					m.logger.Error("failed to enter maintenance window",
						"window_id", w.ID, "server_id", w.ServerID, "error", err)
				}
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
