package health

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// offlineAlerts collects the per-server offline alerts the sweep emitted.
func (c *recordCapture) offlineAlerts() []map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]string
	for _, r := range c.records {
		if r.Message != "permanent registration offline" {
			continue
		}
		fields := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			fields[a.Key] = a.Value.String()
			return true
		})
		out = append(out, fields)
	}
	return out
}

// registerRegServer registers a server carrying an explicit registration
// lifecycle class (the registerServer helper writes legacy "" rows).
func registerRegServer(t *testing.T, ctx context.Context, mem *memory.Store, id string, status model.ServerStatus, regKind string, ttlSeconds int) {
	t.Helper()
	srv := &model.Server{
		ID:            id,
		Name:          id,
		Type:          "game",
		Region:        "cn-east",
		Version:       "1.0.0",
		Platform:      "android",
		Endpoint:      model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity:      2000,
		Status:        status,
		RegKind:       regKind,
		RegTTLSeconds: ttlSeconds,
	}
	if err := mem.RegisterServer(ctx, srv); err != nil {
		t.Fatalf("register server %s: %v", id, err)
	}
}

// TestSweep_PermanentOfflineAlertFires covers the alert contract: a permanent
// registration leaving the auto-managed rotation for offline reports the
// server id, the registration class, the offline duration and the maintenance
// flag — once per outage, not on every sweep.
func TestSweep_PermanentOfflineAlertFires(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	cap := &recordCapture{}

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second).WithAlerts(
		newTestAlerter(AlertConfig{}, cap))

	registerRegServer(t, ctx, mem, "srv-perm", model.StatusOnline, model.RegPermanent, 0)
	recordHeartbeat(t, ctx, mem, "srv-perm", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	alerts := cap.offlineAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected exactly 1 offline alert, got %d: %v", len(alerts), alerts)
	}
	a := alerts[0]
	if a["server_id"] != "srv-perm" {
		t.Errorf("alert server_id = %q, want srv-perm", a["server_id"])
	}
	if a["reg_kind"] != model.RegPermanent {
		t.Errorf("alert reg_kind = %q, want permanent", a["reg_kind"])
	}
	offlineFor, err := time.ParseDuration(a["offline_for"])
	if err != nil || offlineFor < 65*time.Second || offlineFor > 66*time.Second {
		t.Errorf("alert offline_for = %q, want a duration of about 1m5s", a["offline_for"])
	}
	if a["maintenance"] != "false" {
		t.Errorf("alert maintenance = %q, want false", a["maintenance"])
	}

	// A second sweep must not repeat the alert: the server is offline now
	// (not auto-managed), so no transition fires and no spam follows.
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if got := cap.offlineAlerts(); len(got) != 1 {
		t.Errorf("offline alert must fire once per outage, got %d", len(got))
	}
}

// TestSweep_LegacyRegKindAlertsAsPermanent: rows written before the marker
// existed (empty RegKind) behave as permanent — including in the alert.
func TestSweep_LegacyRegKindAlertsAsPermanent(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	cap := &recordCapture{}

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second).WithAlerts(
		newTestAlerter(AlertConfig{}, cap))

	registerRegServer(t, ctx, mem, "srv-old", model.StatusOnline, "", 0)
	recordHeartbeat(t, ctx, mem, "srv-old", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	alerts := cap.offlineAlerts()
	if len(alerts) != 1 || alerts[0]["reg_kind"] != model.RegPermanent {
		t.Fatalf("legacy row should alert as permanent, got %v", alerts)
	}
}

// TestSweep_TemporaryOfflineNoAlert: a temporary registration going offline
// mid-lease transitions like any server but does not alert — its departure
// is expected lifecycle (the TTL will reclaim it), not an incident.
func TestSweep_TemporaryOfflineNoAlert(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	cap := &recordCapture{}

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second).WithAlerts(
		newTestAlerter(AlertConfig{}, cap))

	registerRegServer(t, ctx, mem, "srv-temp", model.StatusOnline, model.RegTemporary, 3600)
	recordHeartbeat(t, ctx, mem, "srv-temp", 100)

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// The status machine still ran…
	srv, err := mem.GetServer(ctx, "srv-temp")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusOffline {
		t.Errorf("expected offline transition, got %s", srv.Status)
	}
	// …but no offline alert fired.
	if got := cap.offlineAlerts(); len(got) != 0 {
		t.Errorf("temporary registration must not alert offline, got %v", got)
	}
}

// TestSweep_MaintenanceExemptsOfflineAlert: an operator-set maintenance
// status with a dead heartbeat neither transitions nor alerts — maintenance
// is the exemption that keeps a maintenance window from paging.
func TestSweep_MaintenanceExemptsOfflineAlert(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	cap := &recordCapture{}

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second).WithAlerts(
		newTestAlerter(AlertConfig{}, cap))

	registerRegServer(t, ctx, mem, "srv-maint", model.StatusMaintenance, model.RegPermanent, 0)
	recordHeartbeat(t, ctx, mem, "srv-maint", 100) // runtime exists but goes stale

	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(10 * time.Minute) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-maint")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.Status != model.StatusMaintenance {
		t.Errorf("maintenance must not be transitioned, got %s", srv.Status)
	}
	if got := cap.offlineAlerts(); len(got) != 0 {
		t.Errorf("maintenance must exempt offline alerting, got %v", got)
	}
}

// TestSweep_TemporaryRegistrationExpires: once created_at + ttl_seconds has
// passed, the record and its runtime data are reclaimed.
func TestSweep_TemporaryRegistrationExpires(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerRegServer(t, ctx, mem, "srv-exp", model.StatusStarting, model.RegTemporary, 10)
	recordHeartbeat(t, ctx, mem, "srv-exp", 100)

	registeredAt := time.Now()
	mon.now = func() time.Time { return registeredAt.Add(65 * time.Second) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if _, err := mem.GetServer(ctx, "srv-exp"); !store.IsNotFound(err) {
		t.Errorf("expired temporary record should be gone, got err=%v", err)
	}
	if _, err := mem.GetRuntime(ctx, "srv-exp"); !store.IsNotFound(err) {
		t.Errorf("expired temporary runtime should be gone, got err=%v", err)
	}
}

// TestSweep_PermanentNeverExpires is the core safety property: nothing the
// monitor does may delete a permanent registration — not time, and not even
// a stray TTL on a permanent row (bypass-written rows included).
func TestSweep_PermanentNeverExpires(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)

	registerRegServer(t, ctx, mem, "srv-perm", model.StatusOnline, model.RegPermanent, 0)
	recordHeartbeat(t, ctx, mem, "srv-perm", 100)
	registerRegServer(t, ctx, mem, "srv-legacy", model.StatusOnline, "", 0)
	recordHeartbeat(t, ctx, mem, "srv-legacy", 100)
	// A bypass-written permanent row carrying a stray TTL: validation rejects
	// this combination, but if it somehow exists it still must not expire.
	registerRegServer(t, ctx, mem, "srv-stray", model.StatusOnline, model.RegPermanent, 5)
	recordHeartbeat(t, ctx, mem, "srv-stray", 100)
	// A temporary row whose lease has not run out yet (48h TTL, sweep at 24h).
	registerRegServer(t, ctx, mem, "srv-young", model.StatusOnline, model.RegTemporary, 48*3600)
	recordHeartbeat(t, ctx, mem, "srv-young", 100)

	registeredAt := time.Now()
	mon.now = func() time.Time { return registeredAt.Add(24 * time.Hour) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	for _, id := range []string{"srv-perm", "srv-legacy", "srv-stray", "srv-young"} {
		if _, err := mem.GetServer(ctx, id); err != nil {
			t.Errorf("server %s must survive the sweep, got err=%v", id, err)
		}
	}
}
