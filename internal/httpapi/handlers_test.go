package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func setupTestServer(t *testing.T) (*httptest.Server, *memory.Store) {
	t.Helper()
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	regSvc := registry.New(mem, mem, logger)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem)
	admSvc := admin.New(mem)
	events := httpadapter.New()
	_ = events.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(context.Background(), e)
		return err
	})

	rtSvc := routing.New(mem, mem, mem)
	handler := New(regSvc, discSvc, dirSvc, admSvc, rtSvc, mem, events, logger)
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

// ---------------------------------------------------------------------------
// Admin: server state management
// ---------------------------------------------------------------------------

func TestAdminMaintenance(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")

	// Heartbeat to promote to online.
	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{
		"players": 100, "load": 0.5,
	})
	resp.Body.Close()

	// Set maintenance.
	resp = postJSON(t, ts, "/v1/admin/servers/game-1001/maintenance", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("maintenance: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify status changed.
	resp = getJSON(t, ts, "/v1/discovery/servers/game-1001")
	var srv model.Server
	json.NewDecoder(resp.Body).Decode(&srv)
	resp.Body.Close()
	if srv.Status != model.StatusMaintenance {
		t.Errorf("expected maintenance, got %s", srv.Status)
	}
}

func TestAdminDrain(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")

	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{
		"players": 100, "load": 0.5,
	})
	resp.Body.Close()

	resp = postJSON(t, ts, "/v1/admin/servers/game-1001/drain", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminEnable(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")

	// First set to maintenance.
	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{
		"players": 100, "load": 0.5,
	})
	resp.Body.Close()

	resp = postJSON(t, ts, "/v1/admin/servers/game-1001/maintenance", nil)
	resp.Body.Close()

	// Now enable.
	resp = postJSON(t, ts, "/v1/admin/servers/game-1001/enable", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminDisable(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")

	resp := postJSON(t, ts, "/v1/admin/servers/game-1001/disable", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminMaintenance_NotFound(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := postJSON(t, ts, "/v1/admin/servers/nonexistent/maintenance", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminDisable_NotFound(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := postJSON(t, ts, "/v1/admin/servers/nonexistent/disable", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Admin: stats
// ---------------------------------------------------------------------------

func TestAdminStats(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")
	registerTestServer(t, ts, "game-1002")

	resp := getJSON(t, ts, "/v1/admin/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: expected 200, got %d", resp.StatusCode)
	}

	var stats struct {
		TotalServers    int            `json:"total_servers"`
		ServersByStatus map[string]int `json:"servers_by_status"`
	}
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()

	if stats.TotalServers != 2 {
		t.Errorf("expected 2 servers, got %d", stats.TotalServers)
	}
}

// ---------------------------------------------------------------------------
// Admin: character search
// ---------------------------------------------------------------------------

func TestAdminSearchCharacters(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Create a character first.
	chBody := map[string]any{
		"account_id":   10001,
		"server_id":    "game-1001",
		"character_id": 823712,
		"name":         "剑无尘",
		"level":        50,
		"class_id":     3,
	}
	resp := postJSON(t, ts, "/v1/directory/characters", chBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create char: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Search by name.
	resp = getJSON(t, ts, "/v1/admin/characters/search?q=剑无尘")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", resp.StatusCode)
	}

	var searchResp struct {
		Characters []model.Character `json:"characters"`
	}
	json.NewDecoder(resp.Body).Decode(&searchResp)
	resp.Body.Close()

	if len(searchResp.Characters) != 1 {
		t.Fatalf("expected 1 character, got %d", len(searchResp.Characters))
	}
	if searchResp.Characters[0].Name != "剑无尘" {
		t.Errorf("expected name 剑无尘, got %s", searchResp.Characters[0].Name)
	}
}

func TestAdminSearchCharacters_ByLevel(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Create characters with different levels.
	for i, level := range []int{10, 50, 100} {
		chBody := map[string]any{
			"account_id":   10000 + i,
			"server_id":    "game-1001",
			"character_id": 823710 + i,
			"name":         "Char",
			"level":        level,
			"class_id":     3,
		}
		resp := postJSON(t, ts, "/v1/directory/characters", chBody)
		resp.Body.Close()
	}

	// Search by level range.
	resp := getJSON(t, ts, "/v1/admin/characters/search?min_level=30&max_level=60")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", resp.StatusCode)
	}

	var searchResp struct {
		Characters []model.Character `json:"characters"`
	}
	json.NewDecoder(resp.Body).Decode(&searchResp)
	resp.Body.Close()

	if len(searchResp.Characters) != 1 {
		t.Fatalf("expected 1 character, got %d", len(searchResp.Characters))
	}
}

// ---------------------------------------------------------------------------
// Admin: migrations
// ---------------------------------------------------------------------------

func TestAdminCreateMigration(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")
	registerTestServer(t, ts, "game-1002")
	registerTestServer(t, ts, "game-2001")

	body := map[string]any{
		"source_servers": []string{"game-1001", "game-1002"},
		"target_server":  "game-2001",
	}
	resp := postJSON(t, ts, "/v1/admin/migrations", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create migration: expected 201, got %d", resp.StatusCode)
	}

	var mig struct {
		ID            string   `json:"id"`
		SourceServers []string `json:"source_servers"`
		TargetServer  string   `json:"target_server"`
		Status        string   `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&mig)
	resp.Body.Close()

	if mig.Status != "pending" {
		t.Errorf("expected pending, got %s", mig.Status)
	}
	if mig.TargetServer != "game-2001" {
		t.Errorf("expected game-2001, got %s", mig.TargetServer)
	}
}

func TestAdminCreateMigration_Invalid(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Missing source servers.
	body := map[string]any{
		"source_servers": []string{},
		"target_server":  "game-2001",
	}
	resp := postJSON(t, ts, "/v1/admin/migrations", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminGetMigration(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")
	registerTestServer(t, ts, "game-2001")

	// Create migration.
	body := map[string]any{
		"source_servers": []string{"game-1001"},
		"target_server":  "game-2001",
	}
	resp := postJSON(t, ts, "/v1/admin/migrations", body)
	var mig struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&mig)
	resp.Body.Close()

	// Get migration.
	resp = getJSON(t, ts, "/v1/admin/migrations/"+mig.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get migration: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminGetMigration_NotFound(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := getJSON(t, ts, "/v1/admin/migrations/nonexistent")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminListMigrations(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")
	registerTestServer(t, ts, "game-2001")

	// Create a migration.
	body := map[string]any{
		"source_servers": []string{"game-1001"},
		"target_server":  "game-2001",
	}
	resp := postJSON(t, ts, "/v1/admin/migrations", body)
	resp.Body.Close()

	// List migrations.
	resp = getJSON(t, ts, "/v1/admin/migrations")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list migrations: expected 200, got %d", resp.StatusCode)
	}

	var listResp struct {
		Migrations []struct {
			ID string `json:"id"`
		} `json:"migrations"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()

	if len(listResp.Migrations) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(listResp.Migrations))
	}
}

func TestAdminRollbackMigration(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")
	registerTestServer(t, ts, "game-2001")

	// Create migration.
	body := map[string]any{
		"source_servers": []string{"game-1001"},
		"target_server":  "game-2001",
	}
	resp := postJSON(t, ts, "/v1/admin/migrations", body)
	var mig struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&mig)
	resp.Body.Close()

	// Rollback.
	resp = postJSON(t, ts, "/v1/admin/migrations/"+mig.ID+"/rollback", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rollback: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify status.
	resp = getJSON(t, ts, "/v1/admin/migrations/"+mig.ID)
	var got struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Status != "rolled_back" {
		t.Errorf("expected rolled_back, got %s", got.Status)
	}
}

func TestAdminRollbackMigration_NotFound(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := postJSON(t, ts, "/v1/admin/migrations/nonexistent/rollback", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

func TestRoutingRecommended(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")
	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{
		"players": 10,
		"load":    0.25,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err := ts.Client().Get(ts.URL + "/v1/routing/recommended?region=cn-east")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out struct {
		Server model.Server `json:"server"`
		Reason string       `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Server.ID != "game-1001" {
		t.Errorf("expected game-1001, got %q", out.Server.ID)
	}
	if out.Reason != routing.ReasonLowestLoad {
		t.Errorf("expected %q, got %q", routing.ReasonLowestLoad, out.Reason)
	}
}

func TestRoutingRecommended_NoServerAvailable(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v1/routing/recommended?region=nowhere")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestRoutingRecommended_InvalidAccountID(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v1/routing/recommended?account_id=abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Admin: realms & shards (TODO v0.1.14)
// ---------------------------------------------------------------------------

func TestAdminRealmsAndShards(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Create a realm.
	resp := postJSON(t, ts, "/v1/admin/realms", map[string]any{
		"id": "realm-01", "name": "华东", "region": "cn-east",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create realm: expected 201, got %d", resp.StatusCode)
	}
	var realm struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&realm)
	resp.Body.Close()
	if realm.ID != "realm-01" || realm.Status != "active" {
		t.Errorf("unexpected realm: %+v", realm)
	}

	// Duplicate realm → 409.
	resp = postJSON(t, ts, "/v1/admin/realms", map[string]any{
		"id": "realm-01", "name": "dup",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate realm: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Missing name → 400.
	resp = postJSON(t, ts, "/v1/admin/realms", map[string]any{"id": "realm-02"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing name: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Create a shard under the realm.
	resp = postJSON(t, ts, "/v1/admin/shards", map[string]any{
		"id": "shard-0101", "realm_id": "realm-01", "name": "一区",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create shard: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Shard referencing a missing realm → 404.
	resp = postJSON(t, ts, "/v1/admin/shards", map[string]any{
		"id": "shard-9901", "realm_id": "missing", "name": "x",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("shard with missing realm: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// List realms.
	resp = getJSON(t, ts, "/v1/admin/realms")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list realms: expected 200, got %d", resp.StatusCode)
	}
	var realmList struct {
		Realms []struct {
			ID string `json:"id"`
		} `json:"realms"`
	}
	json.NewDecoder(resp.Body).Decode(&realmList)
	resp.Body.Close()
	if len(realmList.Realms) != 1 || realmList.Realms[0].ID != "realm-01" {
		t.Errorf("unexpected realm list: %+v", realmList.Realms)
	}

	// List shards filtered by realm.
	resp = getJSON(t, ts, "/v1/admin/shards?realm_id=realm-01")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list shards: expected 200, got %d", resp.StatusCode)
	}
	var shardList struct {
		Shards []struct {
			ID      string `json:"id"`
			RealmID string `json:"realm_id"`
		} `json:"shards"`
	}
	json.NewDecoder(resp.Body).Decode(&shardList)
	resp.Body.Close()
	if len(shardList.Shards) != 1 || shardList.Shards[0].ID != "shard-0101" {
		t.Errorf("unexpected shard list: %+v", shardList.Shards)
	}

	// List shards for an empty realm → empty (non-null) array.
	resp = getJSON(t, ts, "/v1/admin/shards?realm_id=realm-none")
	var emptyList struct {
		Shards []json.RawMessage `json:"shards"`
	}
	json.NewDecoder(resp.Body).Decode(&emptyList)
	resp.Body.Close()
	if emptyList.Shards == nil || len(emptyList.Shards) != 0 {
		t.Errorf("expected empty shards array, got %v", emptyList.Shards)
	}
}

// ---------------------------------------------------------------------------
// Edge-branch tests: argument validation, not-found paths, invalid transitions
// ---------------------------------------------------------------------------

type apiErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, resp *http.Response) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	resp.Body.Close()
	return body
}

func TestRegisterConfigManagedConflict(t *testing.T) {
	ts, mem := setupTestServer(t)
	defer ts.Close()

	// A config-managed server owns its profile fields; the register API
	// must reject an API-side registration with 409.
	if err := mem.RegisterServer(nil, &model.Server{
		ID: "game-cfg", Name: "Declared", Type: "game", Region: "cn-east",
		Version: "1.0.0", Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30009},
		Status:   model.StatusOnline, Source: "config",
	}); err != nil {
		t.Fatalf("seed config server: %v", err)
	}

	resp := postJSON(t, ts, "/v1/registry/servers/register", map[string]any{
		"server_id": "game-cfg", "name": "Hijack", "type": "game", "region": "cn-east",
		"version": "1.0.0", "platform": "android",
		"endpoint": map[string]any{"host": "10.0.0.9", "port": 30009},
		"capacity": 100,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for config-managed server, got %d", resp.StatusCode)
	}
	if code := decodeError(t, resp).Error.Code; code != "SERVER_MANAGED_BY_CONFIG" {
		t.Errorf("expected SERVER_MANAGED_BY_CONFIG, got %q", code)
	}
}

func TestRegisterValidation(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Missing endpoint host fails domain validation → 400.
	resp := postJSON(t, ts, "/v1/registry/servers/register", map[string]any{
		"server_id": "game-bad", "name": "Bad", "type": "game", "region": "cn-east",
		"endpoint": map[string]any{"host": "", "port": 30001}, "capacity": 100,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if code := decodeError(t, resp).Error.Code; code != "INVALID_ARGUMENT" {
		t.Errorf("expected INVALID_ARGUMENT, got %q", code)
	}
}

func TestCharacterInvalidArguments(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")

	// Create: invalid JSON and missing required fields.
	resp, err := ts.Client().Post(ts.URL+"/v1/directory/characters", "application/json",
		bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("create invalid JSON: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSON(t, ts, "/v1/directory/characters", map[string]any{
		"account_id": 0, "server_id": "game-1001", "character_id": 0,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("create missing fields: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET: non-numeric ids → 400, missing character → 404.
	resp = getJSON(t, ts, "/v1/directory/characters/abc")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("get non-numeric id: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = getJSON(t, ts, "/v1/directory/characters/999999")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get missing character: expected 404, got %d", resp.StatusCode)
	}
	if code := decodeError(t, resp).Error.Code; code != "CHARACTER_NOT_FOUND" {
		t.Errorf("expected CHARACTER_NOT_FOUND, got %q", code)
	}

	resp = getJSON(t, ts, "/v1/directory/accounts/abc/characters")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("list non-numeric account: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// PATCH: bad id, bad body, missing character.
	patch := func(path string, body any) *http.Response {
		t.Helper()
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest("PATCH", ts.URL+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("PATCH: %v", err)
		}
		return resp
	}

	if resp := patch("/v1/directory/characters/abc", map[string]any{"level": 2}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("patch non-numeric id: expected 400, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := patch("/v1/directory/characters/1", "not json"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("patch invalid JSON: expected 400, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := patch("/v1/directory/characters/999999", map[string]any{"level": 2}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("patch missing character: expected 404, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// DELETE missing character → 404.
	req, _ := http.NewRequest("DELETE", ts.URL+"/v1/directory/characters/999999", nil)
	delResp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if delResp.StatusCode != http.StatusNotFound {
		t.Errorf("delete missing character: expected 404, got %d", delResp.StatusCode)
	}
	delResp.Body.Close()
}

func TestAdminEnableInvalidTransition(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Enable on a missing server → 404.
	resp := postJSON(t, ts, "/v1/admin/servers/nonexistent/enable", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("enable missing server: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Enable only accepts maintenance/disabled; online → 400.
	registerTestServer(t, ts, "game-1001")
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{"players": 1, "load": 0.1})
	resp.Body.Close()
	resp = postJSON(t, ts, "/v1/admin/servers/game-1001/enable", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("enable from online: expected 400, got %d", resp.StatusCode)
	}
	if code := decodeError(t, resp).Error.Code; code != "INVALID_ARGUMENT" {
		t.Errorf("expected INVALID_ARGUMENT, got %q", code)
	}
}

func TestAdminSearchInvalidFilters(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	for name, q := range map[string]string{
		"class_id":  "?class_id=abc",
		"min_level": "?min_level=abc",
		"max_level": "?max_level=abc",
		"limit":     "?limit=abc",
		"limit<=0":  "?limit=0",
	} {
		resp := getJSON(t, ts, "/v1/admin/characters/search"+q)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestAdminShardErrors(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Post(ts.URL+"/v1/admin/shards", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Missing fields → 400.
	resp = postJSON(t, ts, "/v1/admin/shards", map[string]any{"id": "shard-1"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing fields: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown realm → 404 REALM_NOT_FOUND.
	resp = postJSON(t, ts, "/v1/admin/shards", map[string]any{
		"id": "shard-1", "name": "Shard", "realm_id": "no-such-realm",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown realm: expected 404, got %d", resp.StatusCode)
	}
	if code := decodeError(t, resp).Error.Code; code != "REALM_NOT_FOUND" {
		t.Errorf("expected REALM_NOT_FOUND, got %q", code)
	}

	// Duplicate shard within a realm → 409 SHARD_EXISTS.
	resp = postJSON(t, ts, "/v1/admin/realms", map[string]any{"id": "realm-1", "name": "Realm"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create realm: expected 201, got %d", resp.StatusCode)
	}
	for i := 0; i < 2; i++ {
		resp = postJSON(t, ts, "/v1/admin/shards", map[string]any{
			"id": "shard-1", "name": "Shard", "realm_id": "realm-1",
		})
		want := http.StatusCreated
		if i == 1 {
			want = http.StatusConflict
		}
		if resp.StatusCode != want {
			t.Fatalf("shard attempt %d: expected %d, got %d", i+1, want, resp.StatusCode)
		}
		if i == 1 {
			if code := decodeError(t, resp).Error.Code; code != "SHARD_EXISTS" {
				t.Errorf("expected SHARD_EXISTS, got %q", code)
			}
			continue
		}
		resp.Body.Close()
	}

	// Duplicate realm → 409 REALM_EXISTS.
	resp = postJSON(t, ts, "/v1/admin/realms", map[string]any{"id": "realm-1", "name": "Realm"})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate realm: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// List endpoints tolerate a bad limit (fall back to the default).
	resp = getJSON(t, ts, "/v1/admin/realms?limit=abc")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("realms with bad limit: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = getJSON(t, ts, "/v1/admin/shards?realm_id=realm-1&limit=0")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("shards with bad limit: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminRollbackInvalidState(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")

	resp := postJSON(t, ts, "/v1/admin/migrations", map[string]any{
		"source_servers": []string{"game-1001"}, "target_server": "game-1001",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create migration: expected 201, got %d", resp.StatusCode)
	}
	var mig struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&mig)
	resp.Body.Close()

	// First rollback is fine (pending), a second one is rejected.
	resp = postJSON(t, ts, "/v1/admin/migrations/"+mig.ID+"/rollback", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first rollback: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSON(t, ts, "/v1/admin/migrations/"+mig.ID+"/rollback", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("second rollback: expected 400, got %d", resp.StatusCode)
	}
	if code := decodeError(t, resp).Error.Code; code != "INVALID_ARGUMENT" {
		t.Errorf("expected INVALID_ARGUMENT, got %q", code)
	}
}

func TestMaintenanceWindowLifecycle(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Unknown server → 404.
	resp := postJSON(t, ts, "/v1/admin/servers/no-such/maintenance-window", map[string]any{
		"start_at": "2030-01-01T00:00:00Z", "end_at": "2030-01-01T02:00:00Z",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown server: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	registerTestServer(t, ts, "game-1001")

	// end_at not after start_at → 400.
	resp = postJSON(t, ts, "/v1/admin/servers/game-1001/maintenance-window", map[string]any{
		"start_at": "2030-01-01T02:00:00Z", "end_at": "2030-01-01T02:00:00Z",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("end before start: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Happy path → 201, then listable and deletable.
	resp = postJSON(t, ts, "/v1/admin/servers/game-1001/maintenance-window", map[string]any{
		"start_at": "2030-01-01T00:00:00Z", "end_at": "2030-01-01T02:00:00Z",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create window: expected 201, got %d", resp.StatusCode)
	}
	var mw struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&mw)
	resp.Body.Close()

	resp = getJSON(t, ts, "/v1/admin/maintenance-windows?server_id=game-1001&limit=abc")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list windows: expected 200, got %d", resp.StatusCode)
	}
	var listResp struct {
		Windows []model.MaintenanceWindow `json:"maintenance_windows"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()
	if len(listResp.Windows) != 1 || listResp.Windows[0].ID != mw.ID {
		t.Fatalf("expected the created window, got %+v", listResp.Windows)
	}

	req, _ := http.NewRequest("DELETE", ts.URL+"/v1/admin/maintenance-windows/"+mw.ID, nil)
	delResp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("delete window: expected 204, got %d", delResp.StatusCode)
	}
}

func TestAnnouncementValidation(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Post(ts.URL+"/v1/admin/announcements", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Missing title → 400.
	resp = postJSON(t, ts, "/v1/admin/announcements", map[string]any{
		"body": "b", "starts_at": "2030-01-01T00:00:00Z", "ends_at": "2030-01-01T02:00:00Z",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing title: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown level → 400.
	resp = postJSON(t, ts, "/v1/admin/announcements", map[string]any{
		"title": "t", "level": "loud", "starts_at": "2030-01-01T00:00:00Z", "ends_at": "2030-01-01T02:00:00Z",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown level: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown server → 404.
	missing := "no-such-server"
	resp = postJSON(t, ts, "/v1/admin/announcements", map[string]any{
		"title": "t", "server_id": missing,
		"starts_at": "2030-01-01T00:00:00Z", "ends_at": "2030-01-01T02:00:00Z",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown server: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
