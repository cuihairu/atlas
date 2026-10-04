package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// setupTagServer is setupTestServer with the registry maintenance policy
// injected, so the 維護 enforce mode (block | warn) is testable.
func setupTagServer(t *testing.T, enforce string) (*httptest.Server, *memory.Store) {
	t.Helper()
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	regSvc := registry.New(mem, mem, logger).WithMaintenanceEnforce(enforce)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem)
	admSvc := admin.New(mem)
	events := httpadapter.New()
	_ = events.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(context.Background(), e)
		return err
	})

	crossSvc := crossserver.New(mem, mem, events, logger)
	handler := New(regSvc, discSvc, dirSvc, admSvc, routing.New(mem, mem, mem, mem), crossSvc, mem, events, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return httptest.NewServer(mux), mem
}

func addTag(t *testing.T, ts *httptest.Server, id string, body map[string]any) *http.Response {
	t.Helper()
	return postJSON(t, ts, "/v1/admin/servers/"+id+"/tags", body)
}

func TestAdminServerTagCRUD(t *testing.T) {
	ts, _ := setupTagServer(t, "")
	defer ts.Close()
	registerTestServer(t, ts, "game-1")

	// Empty list to start.
	resp := getJSON(t, ts, "/v1/admin/servers/game-1/tags")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tags: expected 200, got %d", resp.StatusCode)
	}
	var listed struct {
		Tags []model.ServerTag `json:"tags"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed.Tags) != 0 {
		t.Fatalf("fresh server tags = %v, want empty", listed.Tags)
	}

	// Preset add: defaults (label/tier/public) are filled server-side.
	resp = addTag(t, ts, "game-1", map[string]any{"code": "hot"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add hot: expected 200, got %d", resp.StatusCode)
	}
	var after struct {
		Tags []model.ServerTag `json:"tags"`
	}
	json.NewDecoder(resp.Body).Decode(&after)
	resp.Body.Close()
	if len(after.Tags) != 1 || after.Tags[0].Label != "火热" || after.Tags[0].Tier != model.TierHot || !after.Tags[0].Public {
		t.Fatalf("hot tag defaults = %+v", after.Tags)
	}

	// Custom tag defaults to internal (public=false).
	resp = addTag(t, ts, "game-1", map[string]any{"code": "ops_note", "label": "内部观察中"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add custom: expected 200, got %d", resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(&after)
	resp.Body.Close()
	if len(after.Tags) != 2 {
		t.Fatalf("after custom add tags = %v", after.Tags)
	}
	var ops *model.ServerTag
	for i := range after.Tags {
		if after.Tags[i].Code == "ops_note" {
			ops = &after.Tags[i]
		}
	}
	if ops == nil || ops.Public {
		t.Fatalf("custom tag should default to internal: %+v", after.Tags)
	}

	// Upsert by code replaces instead of duplicating.
	resp = addTag(t, ts, "game-1", map[string]any{"code": "hot", "label": "超火爆", "public": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-add hot: expected 200, got %d", resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(&after)
	resp.Body.Close()
	if len(after.Tags) != 2 {
		t.Fatalf("re-add should replace, got %v", after.Tags)
	}
	for _, tag := range after.Tags {
		if tag.Code == "hot" && (tag.Label != "超火爆" || tag.Public) {
			t.Fatalf("replaced hot tag = %+v", tag)
		}
	}

	// Delete by code; deleting again is TAG_NOT_FOUND.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/admin/servers/game-1/tags/ops_note", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete tag: expected 200, got %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/v1/admin/servers/game-1/tags/ops_note", nil)
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var e errorBody
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || e.Error.Code != "TAG_NOT_FOUND" {
		t.Fatalf("second delete = %d %+v, want 404 TAG_NOT_FOUND", resp.StatusCode, e.Error)
	}
}

func TestAdminServerTagValidationAndNotFound(t *testing.T) {
	ts, _ := setupTagServer(t, "")
	defer ts.Close()
	registerTestServer(t, ts, "game-1")

	// Unknown server.
	resp := getJSON(t, ts, "/v1/admin/servers/nope/tags")
	var e errorBody
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || e.Error.Code != "SERVER_NOT_FOUND" {
		t.Fatalf("tags on missing server = %d %+v", resp.StatusCode, e.Error)
	}

	// Bad JSON body.
	resp = postJSON(t, ts, "/v1/admin/servers/game-1/tags", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body = %d, want 400", resp.StatusCode)
	}

	// Invalid code shape.
	resp = addTag(t, ts, "game-1", map[string]any{"code": "Not A Code!", "label": "x"})
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || e.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("bad code = %d %+v, want 400 INVALID_ARGUMENT", resp.StatusCode, e.Error)
	}

	// Custom tag without a label.
	resp = addTag(t, ts, "game-1", map[string]any{"code": "custom"})
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("custom without label = %d, want 400", resp.StatusCode)
	}

	// Unknown tier.
	resp = addTag(t, ts, "game-1", map[string]any{"code": "hot", "tier": "sparkly"})
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown tier = %d, want 400", resp.StatusCode)
	}

	// Delete on unknown server.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/admin/servers/nope/tags/hot", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || e.Error.Code != "SERVER_NOT_FOUND" {
		t.Fatalf("delete on missing server = %d %+v", resp.StatusCode, e.Error)
	}
}

func TestDiscoveryShowsOnlyPublicTags(t *testing.T) {
	ts, _ := setupTagServer(t, "")
	defer ts.Close()
	registerTestServer(t, ts, "game-1")
	heatServer(t, ts, "game-1")

	if resp := addTag(t, ts, "game-1", map[string]any{"code": "hot"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("add hot: %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := addTag(t, ts, "game-1", map[string]any{"code": "ops_note", "label": "内部"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("add ops_note: %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// List view.
	resp := getJSON(t, ts, "/v1/discovery/servers")
	var list struct {
		Servers []model.Server `json:"servers"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Servers) != 1 || len(list.Servers[0].Tags) != 1 || list.Servers[0].Tags[0].Code != "hot" {
		t.Fatalf("discovery tags = %+v, want only public hot", list.Servers)
	}

	// Detail view.
	resp = getJSON(t, ts, "/v1/discovery/servers/game-1")
	var srv model.Server
	json.NewDecoder(resp.Body).Decode(&srv)
	resp.Body.Close()
	if len(srv.Tags) != 1 || srv.Tags[0].Code != "hot" {
		t.Fatalf("detail tags = %+v, want only public hot", srv.Tags)
	}

	// Routing recommendation is a public surface too.
	heatServer(t, ts, "game-1")
	resp = getJSON(t, ts, "/v1/routing/recommended?region=cn-east")
	var rec struct {
		Server model.Server `json:"server"`
	}
	json.NewDecoder(resp.Body).Decode(&rec)
	resp.Body.Close()
	if len(rec.Server.Tags) != 1 || rec.Server.Tags[0].Code != "hot" {
		t.Fatalf("recommended tags = %+v, want only public hot", rec.Server.Tags)
	}
}

// heatServer pushes the server online with a heartbeat so routing has a
// recommendable candidate.
func heatServer(t *testing.T, ts *httptest.Server, id string) {
	t.Helper()
	resp := postJSON(t, ts, "/v1/registry/servers/"+id+"/heartbeat", map[string]any{"players": 5, "load": 0.1})
	resp.Body.Close()
}

func TestCreateCharacterNoRegisterTag(t *testing.T) {
	ts, _ := setupTagServer(t, "")
	defer ts.Close()
	registerTestServer(t, ts, "game-1")
	heatServer(t, ts, "game-1")

	if resp := addTag(t, ts, "game-1", map[string]any{"code": "no_register"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("add no_register: %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Player-facing badge is visible even while registration is rejected.
	resp := getJSON(t, ts, "/v1/discovery/servers/game-1")
	var srv model.Server
	json.NewDecoder(resp.Body).Decode(&srv)
	resp.Body.Close()
	if !model.HasTag(srv.Tags, model.TagNoRegister) {
		t.Fatalf("no_register should be public: %+v", srv.Tags)
	}

	// 创角 rejected with a clear error.
	resp = postJSON(t, ts, "/v1/directory/characters", map[string]any{
		"account_id": 1001, "server_id": "game-1", "character_id": 90001, "name": "被拒角色",
	})
	var e errorBody
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || e.Error.Code != "REGISTRATION_FORBIDDEN" {
		t.Fatalf("create on no_register server = %d %+v, want 403 REGISTRATION_FORBIDDEN", resp.StatusCode, e.Error)
	}

	// Removing the tag restores creation.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/admin/servers/game-1/tags/no_register", nil)
	delResp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()

	resp = postJSON(t, ts, "/v1/directory/characters", map[string]any{
		"account_id": 1001, "server_id": "game-1", "character_id": 90001, "name": "恢复角色",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create after tag removal = %d, want 201", resp.StatusCode)
	}
}

func TestCreateCharacterMaintenanceGate(t *testing.T) {
	// Default mode: a 维护中 tag (server otherwise online) blocks creation.
	ts, _ := setupTagServer(t, "")
	defer ts.Close()
	registerTestServer(t, ts, "game-1")
	heatServer(t, ts, "game-1")

	if resp := addTag(t, ts, "game-1", map[string]any{"code": "maintenance"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("add maintenance: %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	resp := postJSON(t, ts, "/v1/directory/characters", map[string]any{
		"account_id": 1001, "server_id": "game-1", "character_id": 90002, "name": "维护角色",
	})
	var e errorBody
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || e.Error.Code != "SERVER_IN_MAINTENANCE" {
		t.Fatalf("create on maintenance-tagged server = %d %+v, want 403 SERVER_IN_MAINTENANCE", resp.StatusCode, e.Error)
	}

	// Warn mode: the same request succeeds, with a Warning header attached.
	tsWarn, _ := setupTagServer(t, "warn")
	defer tsWarn.Close()
	registerTestServer(t, tsWarn, "game-2")
	heatServer(t, tsWarn, "game-2")
	if resp := addTag(t, tsWarn, "game-2", map[string]any{"code": "maintenance"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("add maintenance: %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	resp = postJSON(t, tsWarn, "/v1/directory/characters", map[string]any{
		"account_id": 1002, "server_id": "game-2", "character_id": 90003, "name": "提示角色",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("warn-mode create = %d, want 201", resp.StatusCode)
	}

	// 禁止注册 ignores the warn mode and still rejects.
	if resp := addTag(t, tsWarn, "game-2", map[string]any{"code": "no_register"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("add no_register: %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	resp = postJSON(t, tsWarn, "/v1/directory/characters", map[string]any{
		"account_id": 1002, "server_id": "game-2", "character_id": 90004, "name": "又拒了",
	})
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || e.Error.Code != "REGISTRATION_FORBIDDEN" {
		t.Fatalf("warn-mode no_register = %d %+v, want 403 REGISTRATION_FORBIDDEN", resp.StatusCode, e.Error)
	}
}
