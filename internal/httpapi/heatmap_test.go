package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAdminLoadHeatmap covers the envelope, the row join with live server
// records, bucket counts per window tier, sampled vs. never-sampled servers,
// the default window, and the 400 on an unknown tier.
func TestAdminLoadHeatmap(t *testing.T) {
	fx := setupFleetFixture(t)
	defer fx.ts.Close()

	registerFleetServer(t, fx.ts, "game-1001", "game", nil)
	registerFleetServer(t, fx.ts, "game-1002", "game", nil)
	resp := postJSON(t, fx.ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{
		"players": 500, "load": 0.5, "status": "online",
	})
	resp.Body.Close()

	// Wait for a tick AFTER the heartbeat so game-1001 has a sampled point.
	deadline := time.Now().Add(2 * time.Second)
	for {
		pts := fx.sampler.Series("load.server.game-1001.load", time.Hour)
		if len(pts) > 0 && pts[len(pts)-1].V == 0.5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sampler never observed game-1001 load: %+v", pts)
		}
		time.Sleep(5 * time.Millisecond)
	}

	type rowOut struct {
		ServerID string  `json:"server_id"`
		Region   string  `json:"region"`
		Status   string  `json:"status"`
		Players  int     `json:"players"`
		Capacity int     `json:"capacity"`
		Load     float64 `json:"load"`
		Buckets  []struct {
			T       time.Time `json:"t"`
			Load    float64   `json:"load"`
			Players float64   `json:"players"`
			Samples int       `json:"samples"`
		} `json:"buckets"`
	}
	resp = getJSON(t, fx.ts, "/v1/admin/load-heatmap?window=24h")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("load-heatmap: expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Window      string   `json:"window"`
		Bucket      string   `json:"bucket"`
		GeneratedAt string   `json:"generated_at"`
		Servers     []rowOut `json:"servers"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	if out.Window != "24h" || out.Bucket != "1h" || out.GeneratedAt == "" {
		t.Errorf("envelope: window=%q bucket=%q generated_at=%q", out.Window, out.Bucket, out.GeneratedAt)
	}
	if len(out.Servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(out.Servers))
	}
	if out.Servers[0].ServerID != "game-1001" || out.Servers[1].ServerID != "game-1002" {
		t.Errorf("rows not sorted by server id: %q, %q", out.Servers[0].ServerID, out.Servers[1].ServerID)
	}
	for _, row := range out.Servers {
		if len(row.Buckets) != 24 {
			t.Errorf("%s buckets = %d, want 24", row.ServerID, len(row.Buckets))
		}
		if row.Region != "cn-east" || row.Capacity != 2000 {
			t.Errorf("%s record lost registry fields: %+v", row.ServerID, row)
		}
	}
	if got := out.Servers[0]; got.Status != "online" {
		t.Errorf("game-1001 status = %q, want online after heartbeat", got.Status)
	}

	got := out.Servers[0]
	if got.Players != 500 || got.Load != 0.5 {
		t.Errorf("game-1001 live record lost the heartbeat: %+v", got)
	}
	// The sampled point lands in the last bucket: samples counted, load
	// averaged, players carried.
	last := got.Buckets[len(got.Buckets)-1]
	if last.Samples == 0 {
		t.Fatalf("game-1001 last bucket has no samples: %+v", got.Buckets)
	}
	if last.Load != 0.5 || last.Players != 500 {
		t.Errorf("last bucket = %+v, want the heartbeat values", last)
	}

	// game-1002 never heartbeated: the probe still samples it from the
	// index, at the idle tier — zero load, zero players, counted samples.
	other := out.Servers[1]
	otherLast := other.Buckets[len(other.Buckets)-1]
	if otherLast.Samples == 0 || otherLast.Load != 0 || otherLast.Players != 0 {
		t.Errorf("game-1002 last bucket = %+v, want idle-tier zeros with samples", otherLast)
	}

	// Default window is 1h → 12 buckets of 5m.
	resp = getJSON(t, fx.ts, "/v1/admin/load-heatmap")
	var def struct {
		Window  string   `json:"window"`
		Bucket  string   `json:"bucket"`
		Servers []rowOut `json:"servers"`
	}
	json.NewDecoder(resp.Body).Decode(&def)
	resp.Body.Close()
	if def.Window != "1h" || def.Bucket != "5m" || len(def.Servers) != 2 || len(def.Servers[0].Buckets) != 12 {
		t.Errorf("default window: %q/%q/%d rows/%d buckets", def.Window, def.Bucket, len(def.Servers), len(def.Servers[0].Buckets))
	}

	// Unknown window is a 400, not a silent default.
	resp = getJSON(t, fx.ts, "/v1/admin/load-heatmap?window=7d")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("window=7d: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAdminLoadHeatmapWithoutTelemetry pins the embedder shape: with no
// sampler the rows still come back with the live record and empty (never
// nil) buckets.
func TestAdminLoadHeatmapWithoutTelemetry(t *testing.T) {
	fx := setupFleetFixture(t)
	defer fx.ts.Close()
	registerFleetServer(t, fx.ts, "game-1001", "game", nil)

	h := fx.handler
	h.telemetry = nil

	rec := httptest.NewRecorder()
	h.handleAdminLoadHeatmap(rec, httptest.NewRequest("GET", "/v1/admin/load-heatmap", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Servers []struct {
			ServerID string `json:"server_id"`
			Players  int    `json:"players"`
			Buckets  []struct {
				Samples int `json:"samples"`
			} `json:"buckets"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(out.Servers))
	}
	row := out.Servers[0]
	if row.ServerID != "game-1001" || row.Buckets == nil || len(row.Buckets) != 0 {
		t.Errorf("nil sampler should return the live record with empty (non-nil) buckets: %+v", row)
	}
}
