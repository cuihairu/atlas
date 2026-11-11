package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestAdminSetServerWeightEndpoint covers the operator weight surface:
// set, read back through the admin server list, clear, and the two error
// shapes (out-of-range weight, unknown server).
func TestAdminSetServerWeightEndpoint(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1")

	// Set an above-neutral weight.
	resp := postJSON(t, ts, "/v1/admin/servers/game-1/weight", map[string]any{"weight": 200})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set weight: expected 200, got %d", resp.StatusCode)
	}
	var set struct {
		ServerID string `json:"server_id"`
		Weight   int    `json:"weight"`
	}
	json.NewDecoder(resp.Body).Decode(&set)
	resp.Body.Close()
	if set.ServerID != "game-1" || set.Weight != 200 {
		t.Fatalf("set weight response = %+v", set)
	}

	// The weight is visible on the admin server list row.
	list := getJSON(t, ts, "/v1/admin/servers?region=cn-east")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list servers: expected 200, got %d", list.StatusCode)
	}
	var page struct {
		Servers []struct {
			ID     string `json:"id"`
			Weight int    `json:"weight"`
		} `json:"servers"`
	}
	json.NewDecoder(list.Body).Decode(&page)
	list.Body.Close()
	if len(page.Servers) != 1 || page.Servers[0].Weight != 200 {
		t.Fatalf("listed server weight = %+v, want weight 200", page.Servers)
	}

	// Out-of-range weight is rejected.
	resp = postJSON(t, ts, "/v1/admin/servers/game-1/weight", map[string]any{"weight": 1001})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("weight 1001: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 0 clears back to neutral.
	resp = postJSON(t, ts, "/v1/admin/servers/game-1/weight", map[string]any{"weight": 0})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear weight: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown server.
	resp = postJSON(t, ts, "/v1/admin/servers/ghost/weight", map[string]any{"weight": 100})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing server: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
