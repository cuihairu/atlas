package health

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// recordCapture collects slog records for assertions.
type recordCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *recordCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r)
	return nil
}

func (c *recordCapture) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (c *recordCapture) WithAttrs(_ []slog.Attr) slog.Handler        { return c }
func (c *recordCapture) WithGroup(_ string) slog.Handler             { return c }

func (c *recordCapture) alerts() []map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]string
	for _, r := range c.records {
		if r.Message != "health alert" {
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

func newTestAlerter(cfg AlertConfig, cap *recordCapture) *Alerter {
	return NewAlerter(cfg, slog.New(cap))
}

func TestAlerter_FiresOnceThenRecovers(t *testing.T) {
	cap := &recordCapture{}
	a := newTestAlerter(AlertConfig{SuspectRatio: 0.3, OfflineRatio: 0.2}, cap)
	ctx := context.Background()

	// 1/10 suspect = 0.1 < 0.3: nothing.
	a.Evaluate(ctx, 10, 1, 0)
	if got := cap.alerts(); len(got) != 0 {
		t.Fatalf("expected no alerts below threshold, got %v", got)
	}

	// 4/10 suspect = 0.4 >= 0.3: one firing alert.
	a.Evaluate(ctx, 10, 4, 0)
	alerts := cap.alerts()
	if len(alerts) != 1 {
		t.Fatalf("expected exactly 1 firing alert, got %d: %v", len(alerts), alerts)
	}
	if alerts[0]["alert"] != "suspect_ratio" || alerts[0]["state"] != "firing" {
		t.Errorf("unexpected alert: %v", alerts[0])
	}

	// Still above threshold: latched, no repeat.
	a.Evaluate(ctx, 10, 5, 0)
	if got := cap.alerts(); len(got) != 1 {
		t.Fatalf("expected latch (no repeat), got %d", len(got))
	}

	// Drops back to 1/10: one recovered alert.
	a.Evaluate(ctx, 10, 1, 0)
	alerts = cap.alerts()
	if len(alerts) != 2 {
		t.Fatalf("expected recovery alert, got %d: %v", len(alerts), alerts)
	}
	if alerts[1]["state"] != "recovered" {
		t.Errorf("expected recovered state, got %v", alerts[1])
	}
}

func TestAlerter_OfflineRatioAndZeroDisables(t *testing.T) {
	cap := &recordCapture{}
	// Offline enabled, suspect disabled (0).
	a := newTestAlerter(AlertConfig{SuspectRatio: 0, OfflineRatio: 0.5}, cap)
	ctx := context.Background()

	a.Evaluate(ctx, 4, 4, 0) // suspect 1.0 — would fire if enabled; offline 0 < 0.5
	if got := cap.alerts(); len(got) != 0 {
		t.Fatalf("suspect alert should be disabled, got %v", got)
	}

	a.Evaluate(ctx, 4, 0, 3) // offline 0.75 >= 0.5
	alerts := cap.alerts()
	if len(alerts) != 1 || alerts[0]["alert"] != "offline_ratio" || alerts[0]["state"] != "firing" {
		t.Fatalf("expected offline_ratio firing alert, got %v", alerts)
	}
}

func TestAlerter_EmptyFleetRecovers(t *testing.T) {
	cap := &recordCapture{}
	a := newTestAlerter(AlertConfig{OfflineRatio: 0.2}, cap)
	ctx := context.Background()

	a.Evaluate(ctx, 2, 0, 2) // fires
	a.Evaluate(ctx, 0, 0, 0) // fleet drained: ratio 0, must recover (no divide-by-zero)
	alerts := cap.alerts()
	if len(alerts) != 2 || alerts[1]["state"] != "recovered" {
		t.Fatalf("expected firing then recovered on empty fleet, got %v", alerts)
	}
}

func TestAlerter_WebhookPayload(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p map[string]any
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		payloads = append(payloads, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cap := &recordCapture{}
	a := newTestAlerter(AlertConfig{
		SuspectRatio:   0.5,
		WebhookURL:     srv.URL,
		WebhookTimeout: 2 * time.Second,
	}, cap)
	a.now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }

	a.Evaluate(context.Background(), 4, 2, 0) // suspect 0.5 >= 0.5 fires
	a.Evaluate(context.Background(), 4, 0, 0) // recovers

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 2 {
		t.Fatalf("expected 2 webhook deliveries (firing + recovered), got %d", len(payloads))
	}
	if payloads[0]["alert"] != "suspect_ratio" || payloads[0]["state"] != "firing" {
		t.Errorf("unexpected firing payload: %v", payloads[0])
	}
	if payloads[0]["ratio"].(float64) != 0.5 || payloads[0]["threshold"].(float64) != 0.5 {
		t.Errorf("unexpected ratio fields: %v", payloads[0])
	}
	if payloads[0]["count"].(float64) != 2 || payloads[0]["auto_managed_total"].(float64) != 4 {
		t.Errorf("unexpected count fields: %v", payloads[0])
	}
	if payloads[0]["fired_at"] != "2026-10-01T12:00:00Z" {
		t.Errorf("unexpected fired_at: %v", payloads[0]["fired_at"])
	}
	if payloads[1]["state"] != "recovered" {
		t.Errorf("unexpected recovery payload: %v", payloads[1])
	}
}

func TestAlerter_WebhookFailureNonFatal(t *testing.T) {
	// Server that panics on every request — delivery fails but Evaluate must
	// not panic and the log alert still fires.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))
	defer srv.Close()

	cap := &recordCapture{}
	a := newTestAlerter(AlertConfig{OfflineRatio: 0.1, WebhookURL: srv.URL, WebhookTimeout: time.Second}, cap)

	a.Evaluate(context.Background(), 2, 0, 2)

	if got := cap.alerts(); len(got) != 1 {
		t.Fatalf("log alert should still be emitted, got %d", len(got))
	}
}

func TestSweep_TriggersRatioAlert(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	cap := &recordCapture{}

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second).WithAlerts(
		newTestAlerter(AlertConfig{SuspectRatio: 0.5, OfflineRatio: 0.5}, cap))

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
	registerServer(t, ctx, mem, "srv-2", model.StatusOnline)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)
	recordHeartbeat(t, ctx, mem, "srv-2", 100)

	// 65s later both go offline → offline ratio 1.0 >= 0.5.
	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	var offlineAlert map[string]string
	for _, al := range cap.alerts() {
		if al["alert"] == "offline_ratio" {
			offlineAlert = al
		}
	}
	if offlineAlert == nil {
		t.Fatalf("expected offline_ratio alert, got %v", cap.alerts())
	}
	if offlineAlert["state"] != "firing" || offlineAlert["count"] != "2" || offlineAlert["auto_managed_total"] != "2" {
		t.Errorf("unexpected offline alert fields: %v", offlineAlert)
	}

	// Second sweep, still offline: latched, no repeat firing.
	before := len(cap.alerts())
	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep 2: %v", err)
	}
	for _, al := range cap.alerts()[before:] {
		if al["alert"] == "offline_ratio" {
			t.Errorf("offline_ratio re-fired while still firing: %v", al)
		}
	}
}

func TestCountStatuses_IgnoresNonAutoManaged(t *testing.T) {
	servers := []*model.Server{
		{ID: "a", Status: model.StatusOnline},
		{ID: "b", Status: model.StatusSuspect},
		{ID: "c", Status: model.StatusSuspect},
		{ID: "d", Status: model.StatusOffline},
		{ID: "e", Status: model.StatusMaintenance}, // not auto-managed
		{ID: "f", Status: model.StatusDisabled},    // not auto-managed
	}
	total, suspect, offline := countStatuses(servers)
	if total != 4 || suspect != 2 || offline != 1 {
		t.Errorf("expected 4/2/1, got %d/%d/%d", total, suspect, offline)
	}
}
