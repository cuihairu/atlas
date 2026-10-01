package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func setupTestServer(t *testing.T) (*httptest.Server, *memory.Store) {
	t.Helper()
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	regSvc := registry.New(mem, mem, logger)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem)

	handler := New(regSvc, discSvc, dirSvc, mem, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return httptest.NewServer(mux), mem
}

func registerTestServer(t *testing.T, ts *httptest.Server, id string) {
	t.Helper()
	body := map[string]any{
		"server_id": id,
		"name":      "Test Server",
		"type":      "game",
		"region":    "cn-east",
		"version":   "1.0.0",
		"platform":  "android",
		"endpoint":  map[string]any{"host": "10.0.0.1", "port": 30001},
		"capacity":  2000,
	}
	resp := postJSON(t, ts, "/v1/registry/servers/register", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func getJSON(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Full flow test: register → heartbeat → list → get → unregister
// ---------------------------------------------------------------------------

func TestFullFlow(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// 1. Register
	body := map[string]any{
		"server_id": "game-1001",
		"name":      "Test Server",
		"type":      "game",
		"region":    "cn-east",
		"version":   "1.0.0",
		"platform":  "android",
		"endpoint":  map[string]any{"host": "10.0.0.1", "port": 30001},
		"capacity":  2000,
	}
	resp := postJSON(t, ts, "/v1/registry/servers/register", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Heartbeat
	hbBody := map[string]any{
		"players": 100,
		"load":    0.5,
	}
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/heartbeat", hbBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 3. List servers
	resp = getJSON(t, ts, "/v1/discovery/servers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", resp.StatusCode)
	}
	var listResp struct {
		Servers []model.Server `json:"servers"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()
	if len(listResp.Servers) != 1 {
		t.Fatalf("list: expected 1 server, got %d", len(listResp.Servers))
	}
	if listResp.Servers[0].ID != "game-1001" {
		t.Errorf("list: expected game-1001, got %s", listResp.Servers[0].ID)
	}

	// 4. Get single server
	resp = getJSON(t, ts, "/v1/discovery/servers/game-1001")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}
	var srv model.Server
	json.NewDecoder(resp.Body).Decode(&srv)
	resp.Body.Close()
	if srv.ID != "game-1001" {
		t.Errorf("get: expected game-1001, got %s", srv.ID)
	}

	// 5. Unregister
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/unregister", map[string]any{"reason": "shutdown"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unregister: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestRegisterInvalidBody(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Send invalid JSON.
	resp, err := ts.Client().Post(ts.URL+"/v1/registry/servers/register", "application/json",
		bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetServerNotFound(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := getJSON(t, ts, "/v1/discovery/servers/nonexistent")
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Character CRUD flow
// ---------------------------------------------------------------------------

func TestCharacterFlow(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// 1. Create character
	chBody := map[string]any{
		"account_id":   10001,
		"server_id":    "game-1001",
		"character_id": 823712,
		"name":         "TestChar",
		"level":        50,
		"class_id":     3,
	}
	resp := postJSON(t, ts, "/v1/directory/characters", chBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create char: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. List by account
	resp = getJSON(t, ts, "/v1/directory/accounts/10001/characters")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list by account: expected 200, got %d", resp.StatusCode)
	}
	var listResp struct {
		Characters []model.Character `json:"characters"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()
	if len(listResp.Characters) != 1 {
		t.Fatalf("expected 1 character, got %d", len(listResp.Characters))
	}
	if listResp.Characters[0].Name != "TestChar" {
		t.Errorf("expected name 'TestChar', got %q", listResp.Characters[0].Name)
	}

	// 3. Get by character_id
	resp = getJSON(t, ts, "/v1/directory/characters/823712")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get char: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. Delete
	req, _ := http.NewRequest("DELETE", ts.URL+"/v1/directory/characters/823712", nil)
	delResp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Errorf("delete: expected 200, got %d", delResp.StatusCode)
	}

	// 5. Verify gone
	resp = getJSON(t, ts, "/v1/directory/characters/823712")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Discovery visibility filter
// ---------------------------------------------------------------------------

func TestDiscoveryDefaultVisibility(t *testing.T) {
	ts, mem := setupTestServer(t)
	defer ts.Close()

	// Register an online server.
	registerTestServer(t, ts, "game-online")

	// Manually register an offline server.
	srv := &model.Server{
		ID:       "game-offline",
		Name:     "Offline Server",
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.2", Port: 30002},
		Capacity: 1000,
		Status:   model.StatusOffline,
	}
	if err := mem.RegisterServer(nil, srv); err != nil {
		// context.Background() is fine since memory store doesn't use ctx.
		t.Fatalf("register offline server: %v", err)
	}

	// Default listing should only return visible servers.
	resp := getJSON(t, ts, "/v1/discovery/servers")
	var listResp struct {
		Servers []model.Server `json:"servers"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()

	for _, s := range listResp.Servers {
		if s.ID == "game-offline" {
			t.Error("offline server should not appear in default listing")
		}
	}

	// Explicit status=offline should return it.
	resp = getJSON(t, ts, "/v1/discovery/servers?status=offline")
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()

	found := false
	for _, s := range listResp.Servers {
		if s.ID == "game-offline" {
			found = true
		}
	}
	if !found {
		t.Error("offline server should appear when status=offline is requested")
	}
}

// ---------------------------------------------------------------------------
// Health endpoints
// ---------------------------------------------------------------------------

func TestHealthz(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := getJSON(t, ts, "/healthz")
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestReadyz(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := getJSON(t, ts, "/readyz")
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}