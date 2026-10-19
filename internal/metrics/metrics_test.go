package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newTestMetrics(t *testing.T) (*Metrics, *memory.Store) {
	t.Helper()
	mem := memory.New()
	return New(mem), mem
}

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	ts := httptest.NewServer(m.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(body)
}

func TestScrapeTimeGauges(t *testing.T) {
	m, mem := newTestMetrics(t)
	ctx := context.Background()

	for _, id := range []string{"s1", "s2", "s3"} {
		if err := mem.RegisterServer(ctx, &model.Server{ID: id, Name: id, Status: model.StatusOnline}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	if err := mem.UpsertCharacter(ctx, &model.Character{AccountID: 1, ServerID: "s1", CharacterID: 1, Name: "c"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	out := scrape(t, m)
	if !hasLine(out, `atlas_registry_servers_total{status="online"} 3`) {
		t.Errorf("expected servers gauge = 3, got:\n%s", out)
	}
	if !hasLine(out, `atlas_directory_characters_total 1`) {
		t.Errorf("expected characters gauge = 1, got:\n%s", out)
	}
}

func TestInProcessCollectors(t *testing.T) {
	m, _ := newTestMetrics(t)

	m.CountDiscovery(store.ServerFilter{Status: model.StatusOnline})
	m.CountDiscovery(store.ServerFilter{})
	m.ObserveHeartbeatLag(42 * time.Second)
	m.CountHealthTransition(model.StatusOnline, model.StatusSuspect)

	out := scrape(t, m)
	if !hasLine(out, `atlas_discovery_requests_total{filter="status=online"} 1`) {
		t.Errorf("missing discovery counter for status filter:\n%s", out)
	}
	if !hasLine(out, `atlas_discovery_requests_total{filter="none"} 1`) {
		t.Errorf("missing discovery counter for empty filter:\n%s", out)
	}
	if !hasLine(out, `atlas_health_transitions_total{from="online",to="suspect"} 1`) {
		t.Errorf("missing health transition counter:\n%s", out)
	}
	if !strings.Contains(out, "atlas_registry_heartbeat_lag_seconds_bucket{le=\"60\"} 1") {
		t.Errorf("expected heartbeat lag observed in 60s bucket:\n%s", out)
	}
}

func TestRequestCounterMiddleware(t *testing.T) {
	m, _ := newTestMetrics(t)
	handler := RequestCounter(m.AdminRequests)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/stats", nil)
	// httptest requests carry no route pattern; middleware falls back to "unmatched".
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := scrape(t, m)
	if !hasLine(out, `atlas_admin_requests_total{endpoint="unmatched",status="418"} 1`) {
		t.Errorf("missing admin request counter:\n%s", out)
	}
}

func TestObserveDirectoryWrite(t *testing.T) {
	m, _ := newTestMetrics(t)

	m.ObserveDirectoryWrite("create", 3*time.Millisecond)
	m.ObserveDirectoryWrite("create", 2*time.Millisecond)
	m.ObserveDirectoryWrite("event", 800*time.Microsecond)

	out := scrape(t, m)
	if !hasLine(out, `atlas_directory_write_duration_seconds_count{op="create"} 2`) {
		t.Errorf("expected 2 create observations:\n%s", out)
	}
	if !hasLine(out, `atlas_directory_write_duration_seconds_count{op="event"} 1`) {
		t.Errorf("expected 1 event observation:\n%s", out)
	}
	if !strings.Contains(out, `atlas_directory_write_duration_seconds_bucket{op="create",le="0.005"} 2`) {
		t.Errorf("expected create observations in the 5ms bucket:\n%s", out)
	}

	// Nil receiver must be a no-op (services run without metrics in tests).
	var nilMetrics *Metrics
	nilMetrics.ObserveDirectoryWrite("create", time.Millisecond)
}

func TestObserveRegistryWrite(t *testing.T) {
	m, _ := newTestMetrics(t)

	m.ObserveRegistryWrite("register", 3*time.Millisecond)
	m.ObserveRegistryWrite("register", 2*time.Millisecond)
	m.ObserveRegistryWrite("heartbeat", 800*time.Microsecond)
	m.ObserveRegistryWrite("unregister", 250*time.Microsecond)

	out := scrape(t, m)
	if !hasLine(out, `atlas_registry_write_duration_seconds_count{op="register"} 2`) {
		t.Errorf("expected 2 register observations:\n%s", out)
	}
	if !hasLine(out, `atlas_registry_write_duration_seconds_count{op="heartbeat"} 1`) {
		t.Errorf("expected 1 heartbeat observation:\n%s", out)
	}
	if !hasLine(out, `atlas_registry_write_duration_seconds_count{op="unregister"} 1`) {
		t.Errorf("expected 1 unregister observation:\n%s", out)
	}
	if !strings.Contains(out, `atlas_registry_write_duration_seconds_bucket{op="register",le="0.005"} 2`) {
		t.Errorf("expected register observations in the 5ms bucket:\n%s", out)
	}

	// Nil receiver must be a no-op.
	var nilMetrics *Metrics
	nilMetrics.ObserveRegistryWrite("heartbeat", time.Millisecond)
}

func hasLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// TestEventDeliveryCounters pins the event delivery-limit counters (the
// observability contract for ATLAS_EVENT_MAX_DELIVERY) and the nil-receiver
// safety the adapters rely on.
func TestEventDeliveryCounters(t *testing.T) {
	m, _ := newTestMetrics(t)
	m.ObserveRedelivery("kafka")
	m.ObserveRedelivery("kafka")
	m.ObserveDeadLetter("nats", "atlas.characters")
	out := scrape(t, m)
	if !hasLine(out, `atlas_event_redeliveries_total{adapter="kafka"} 2`) {
		t.Errorf("missing atlas_event_redeliveries_total line:\n%s", out)
	}
	if !hasLine(out, `atlas_event_dead_letters_total{adapter="nats",topic="atlas.characters"} 1`) {
		t.Errorf("missing atlas_event_dead_letters_total line:\n%s", out)
	}

	var nilMetrics *Metrics
	nilMetrics.ObserveRedelivery("kafka")
	nilMetrics.ObserveDeadLetter("nats", "atlas.characters")
}

// TestAdminAuditCounters pins the audit sink counters (the observability
// contract for ATLAS_AUDIT_SINK: the drop counter is the alert that the
// archive is incomplete) and the nil-receiver safety the audit wiring relies
// on (a nil *Metrics satisfies audit.Observer).
func TestAdminAuditCounters(t *testing.T) {
	m, _ := newTestMetrics(t)
	m.ObserveAuditWritten()
	m.ObserveAuditWritten()
	m.ObserveAuditDropped()
	m.ObserveAuditWrite(10 * time.Millisecond)
	m.ObserveAuditRetention(7)
	out := scrape(t, m)
	for _, want := range []string{
		`atlas_admin_audit_sink_written_total 2`,
		`atlas_admin_audit_sink_dropped_total 1`,
		`atlas_admin_audit_retention_deleted_total 7`,
		`atlas_admin_audit_sink_write_seconds_bucket{le="0.01"} 1`,
	} {
		if !hasLine(out, want) {
			t.Errorf("missing %s:\n%s", want, out)
		}
	}

	// Zero deletions must not move the counter (the sweep runs hourly).
	m.ObserveAuditRetention(0)
	out = scrape(t, m)
	if !hasLine(out, `atlas_admin_audit_retention_deleted_total 7`) {
		t.Errorf("zero-deletion sweep moved the counter:\n%s", out)
	}

	var nilMetrics *Metrics
	nilMetrics.ObserveAuditWritten()
	nilMetrics.ObserveAuditDropped()
	nilMetrics.ObserveAuditWrite(time.Second)
	nilMetrics.ObserveAuditRetention(3)
}
