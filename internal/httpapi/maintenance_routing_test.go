package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
)

// TestRoutingAvoidsMaintenanceWindowedServer walks the 维护前引导 contract
// end to end: a server inside an active maintenance window (status still
// "online" — the monitor has not swept yet) is steered away from by
// /v1/routing/recommended, and /v1/admin/diagnose/routing names the window
// on the rejected verdict.
func TestRoutingAvoidsMaintenanceWindowedServer(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")
	registerTestServer(t, ts, "game-1002")
	for _, id := range []string{"game-1001", "game-1002"} {
		resp := postJSON(t, ts, "/v1/registry/servers/"+id+"/heartbeat", map[string]any{
			"players": 10, "load": 0.2, "status": "online",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("heartbeat %s: %d", id, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// game-1001 gets an active maintenance window (started an hour ago) —
	// its lifecycle status stays "online" until the health monitor sweeps.
	now := time.Now()
	resp := postJSON(t, ts, "/v1/admin/servers/game-1001/maintenance-window", map[string]any{
		"start_at": now.Add(-time.Hour).Format(time.RFC3339),
		"end_at":   now.Add(time.Hour).Format(time.RFC3339),
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create window: %d", resp.StatusCode)
	}
	var mw model.MaintenanceWindow
	if err := json.NewDecoder(resp.Body).Decode(&mw); err != nil {
		t.Fatalf("decode window: %v", err)
	}
	resp.Body.Close()

	// The recommended endpoint steers to the clean server.
	rec := getJSON(t, ts, "/v1/routing/recommended?region=cn-east")
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("recommended: %d", rec.StatusCode)
	}
	var recBody struct {
		Server struct {
			ID string `json:"id"`
		} `json:"server"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&recBody)
	rec.Body.Close()
	if recBody.Server.ID != "game-1002" {
		t.Errorf("recommended = %q, want game-1002 (game-1001 windowed)", recBody.Server.ID)
	}

	// The diagnose endpoint names the window on the rejected verdict.
	diag := getJSON(t, ts, "/v1/admin/diagnose/routing?region=cn-east")
	if diag.StatusCode != http.StatusOK {
		t.Fatalf("diagnose: %d", diag.StatusCode)
	}
	var diagBody struct {
		Diagnosis struct {
			WinnerID string `json:"winner_id"`
			Servers  []struct {
				Server            map[string]any          `json:"server"`
				Rank              int                     `json:"rank"`
				Eligible          bool                    `json:"eligible"`
				Reason            string                  `json:"reason"`
				MaintenanceWindow *model.MaintenanceWindow `json:"maintenance_window"`
			} `json:"servers"`
		} `json:"diagnosis"`
	}
	_ = json.NewDecoder(diag.Body).Decode(&diagBody)
	diag.Body.Close()

	if diagBody.Diagnosis.WinnerID != "game-1002" {
		t.Errorf("diagnose winner = %q, want game-1002", diagBody.Diagnosis.WinnerID)
	}
	checked := false
	for _, v := range diagBody.Diagnosis.Servers {
		if v.Server["id"] == "game-1001" {
			checked = true
			if v.Rank != 0 || v.Eligible || v.Reason != "maintenance_window=active" {
				t.Errorf("game-1001 verdict = rank %d eligible %v reason %q, want rejected/active",
					v.Rank, v.Eligible, v.Reason)
			}
			if v.MaintenanceWindow == nil || v.MaintenanceWindow.ID != mw.ID {
				t.Errorf("game-1001 maintenance_window = %+v, want %s attached", v.MaintenanceWindow, mw.ID)
			}
		}
	}
	if !checked {
		t.Error("game-1001 verdict missing from diagnose response")
	}
}