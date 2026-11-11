package directory

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestWriteMetricsObserved asserts every write path lands in
// atlas_directory_write_duration_seconds with its op label: the three REST
// writes plus the event-bus projection apply.
func TestWriteMetricsObserved(t *testing.T) {
	mem := memory.New()
	m := metrics.New(mem)
	svc := New(mem).WithMetrics(m)
	ctx := context.Background()

	if _, err := svc.CreateCharacter(ctx, 1, "s1", 1, "c1", 10, 2); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	if _, err := svc.UpdateCharacter(ctx, 1, "s1", 1, store.CharacterPatch{Name: strPtr("c1x")}); err != nil {
		t.Fatalf("UpdateCharacter: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type:        event.EventCharacterCreated,
		AccountID:   1,
		ServerID:    "s1",
		CharacterID: 2,
		Name:        "c2",
		Level:       intPtr(5),
	}); err != nil {
		t.Fatalf("ApplyEvent: %v", err)
	}
	if err := svc.DeleteCharacter(ctx, 1, "s1", 2); err != nil {
		t.Fatalf("DeleteCharacter: %v", err)
	}

	out := scrapeMetrics(t, m)
	for _, op := range []string{"create", "update", "delete", "created"} {
		want := "atlas_directory_write_duration_seconds_count{op=\"" + op + "\"} 1"
		if !metricsLine(out, want) {
			t.Errorf("missing write observation %q in:\n%s", want, out)
		}
	}
}

func scrapeMetrics(t *testing.T, m *metrics.Metrics) string {
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

func metricsLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }
