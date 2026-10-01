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

func hasLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
