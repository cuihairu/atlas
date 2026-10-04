package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// decodeInto is a helper that decodes a response body and closes it.
func decodeInto(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// TestAdminLifecycleEndpoints covers maintenance/drain/enable/disable on both
// happy and unknown-server paths.
func TestAdminLifecycleEndpoints(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-1")

	// Unknown server → 404 SERVER_NOT_FOUND on every lifecycle route.
	for _, path := range []string{
		"/v1/admin/servers/ghost/maintenance",
		"/v1/admin/servers/ghost/drain",
		"/v1/admin/servers/ghost/enable",
		"/v1/admin/servers/ghost/disable",
	} {
		if resp := postJSON(t, ts, path, map[string]any{}); resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, resp.StatusCode)
			resp.Body.Close()
		}
	}

	// Lifecycle walk in fixed order — each step's validity depends on the
	// previous status, so this cannot be a map iteration:
	// online → maintenance → enable(online) → drain → disable.
	steps := []struct {
		path   string
		status string
	}{
		{"/v1/admin/servers/srv-1/maintenance", "maintenance"},
		{"/v1/admin/servers/srv-1/enable", "online"},
		{"/v1/admin/servers/srv-1/drain", "draining"},
		{"/v1/admin/servers/srv-1/disable", "disabled"},
	}
	for _, step := range steps {
		resp := postJSON(t, ts, step.path, map[string]any{})
		var body struct {
			Status string `json:"status"`
		}
		decodeInto(t, resp, &body)
		if resp.StatusCode != http.StatusOK || body.Status != step.status {
			t.Errorf("POST %s = %d %q, want 200 %q", step.path, resp.StatusCode, body.Status, step.status)
		}
	}
}

// TestHeartbeatAndUnregisterErrors covers registry error branches.
func TestHeartbeatAndUnregisterErrors(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// Heartbeat for unknown server → 404.
	if resp := postJSON(t, ts, "/v1/registry/servers/ghost/heartbeat",
		map[string]any{"players": 1, "load": 0.5}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("heartbeat ghost = %d, want 404", resp.StatusCode)
		resp.Body.Close()
	}

	// Unregister unknown → 404.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/registry/servers/ghost/unregister", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unregister ghost = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Invalid heartbeat payload → 400.
	registerTestServer(t, ts, "srv-1")
	if resp := postJSON(t, ts, "/v1/registry/servers/srv-1/heartbeat",
		map[string]any{"players": -5, "load": 0.5}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("heartbeat players=-5 = %d, want 400", resp.StatusCode)
		resp.Body.Close()
	}

	// Valid unregister → 200.
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/registry/servers/srv-1/unregister", nil)
	resp2, err := ts.Client().Do(req2)
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("unregister = %d, want 200", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// TestDirectoryCharacterEndpoints covers the character CRUD surface.
func TestDirectoryCharacterEndpoints(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-1")

	// Invalid body → 400.
	if resp := postJSON(t, ts, "/v1/directory/characters",
		map[string]any{"account_id": 0}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("create invalid = %d, want 400", resp.StatusCode)
		resp.Body.Close()
	}

	// Unknown server: directory does not FK-validate server existence —
	// characters may reference servers that unregister later (eventual
	// consistency), so the create is accepted.
	if resp := postJSON(t, ts, "/v1/directory/characters",
		map[string]any{"account_id": 42, "server_id": "ghost", "character_id": 8001, "name": "x"}); resp.StatusCode != http.StatusCreated {
		t.Errorf("create on ghost server = %d, want 201 (no FK check)", resp.StatusCode)
		resp.Body.Close()
	}

	// Create (synchronous adapter) → character object.
	resp := postJSON(t, ts, "/v1/directory/characters", map[string]any{
		"account_id":   42,
		"server_id":    "srv-1",
		"character_id": 9001,
		"name":         " galadriel ",
		"level":        60,
		"class_id":     3,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d, want 201", resp.StatusCode)
	}
	var ch model.Character
	decodeInto(t, resp, &ch)
	if ch.CharacterID != 9001 || ch.AccountID != 42 || ch.ServerID != "srv-1" {
		t.Fatalf("created = %+v", ch)
	}

	// Get by character id.
	if resp := getJSON(t, ts, "/v1/directory/characters/9001"); resp.StatusCode != http.StatusOK {
		t.Fatalf("get = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	// Unknown character id → 404.
	if resp := getJSON(t, ts, "/v1/directory/characters/999999"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("get unknown = %d, want 404", resp.StatusCode)
		resp.Body.Close()
	}

	// List by server.
	if resp := getJSON(t, ts, "/v1/directory/servers/srv-1/characters"); resp.StatusCode != http.StatusOK {
		t.Errorf("list-by-server = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}

	// Patch name/level.
	patch := func() *http.Response {
		data, _ := json.Marshal(map[string]any{"account_id": 42, "server_id": "srv-1", "name": "galadriel", "level": 61})
		req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/v1/directory/characters/9001", bytes.NewReader(data))
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		return resp
	}
	if resp := patch(); resp.StatusCode != http.StatusOK {
		t.Errorf("patch = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}
	// Patch invalid character id → 400.
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/v1/directory/characters/notanumber", nil)
	if resp, err := ts.Client().Do(req); err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("patch bad id = %v, want 400", err)
	} else {
		resp.Body.Close()
	}

	// Delete: unknown → 404, known → 200.
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/v1/directory/characters/999999", nil)
	if resp, err := ts.Client().Do(req); err != nil || resp.StatusCode != http.StatusNotFound {
		t.Errorf("delete unknown = %v, want 404", err)
	} else {
		resp.Body.Close()
	}
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/v1/directory/characters/9001", nil)
	if resp, err := ts.Client().Do(req); err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("delete = %v, want 200", err)
	} else {
		resp.Body.Close()
	}
}

// TestAdminMigrationEndpoints covers create/list/get/rollback.
func TestAdminMigrationEndpoints(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-src")
	registerTestServer(t, ts, "srv-dst")

	resp := postJSON(t, ts, "/v1/admin/migrations", map[string]any{
		"source_servers": []string{"srv-src"},
		"target_server":  "srv-dst",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create migration = %d, want 201", resp.StatusCode)
	}
	var mig model.Migration
	decodeInto(t, resp, &mig)
	if mig.ID == "" {
		t.Fatal("migration id empty")
	}

	// List.
	if resp := getJSON(t, ts, "/v1/admin/migrations"); resp.StatusCode != http.StatusOK {
		t.Errorf("list migrations = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}

	// Get by id.
	if resp := getJSON(t, ts, "/v1/admin/migrations/"+mig.ID); resp.StatusCode != http.StatusOK {
		t.Errorf("get migration = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}

	// Rollback.
	resp = postJSON(t, ts, "/v1/admin/migrations/"+mig.ID+"/rollback", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("rollback = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown source server → 4xx.
	if resp := postJSON(t, ts, "/v1/admin/migrations", map[string]any{
		"source_servers": []string{"ghost"},
		"target_server":  "srv-dst",
	}); resp.StatusCode >= 500 {
		t.Errorf("create with ghost source = %d, want 4xx", resp.StatusCode)
		resp.Body.Close()
	}
}

// TestAdminRealmShardEndpoints covers realm + shard CRUD.
func TestAdminRealmShardEndpoints(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp := postJSON(t, ts, "/v1/admin/realms", map[string]any{
		"id": "realm-1", "name": "国服一区", "region": "cn-east",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create realm = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	// Duplicate → 409 REALM_EXISTS.
	if resp := postJSON(t, ts, "/v1/admin/realms", map[string]any{"id": "realm-1", "name": "x"}); resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate realm = %d, want 409", resp.StatusCode)
		resp.Body.Close()
	}

	// Missing name → 400.
	if resp := postJSON(t, ts, "/v1/admin/realms", map[string]any{"id": "realm-2"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("realm missing name = %d, want 400", resp.StatusCode)
		resp.Body.Close()
	}

	resp = postJSON(t, ts, "/v1/admin/shards", map[string]any{
		"id": "shard-1", "realm_id": "realm-1", "name": "s1",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create shard = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown realm → 404 REALM_NOT_FOUND.
	if resp := postJSON(t, ts, "/v1/admin/shards", map[string]any{"id": "shard-x", "realm_id": "ghost", "name": "x"}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("shard ghost realm = %d, want 404", resp.StatusCode)
		resp.Body.Close()
	}

	for _, path := range []string{"/v1/admin/realms", "/v1/admin/shards?realm_id=realm-1", "/v1/admin/stats"} {
		if resp := getJSON(t, ts, path); resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
			resp.Body.Close()
		}
	}
}

// TestAdminAuditEndpoint: WithAudit + AuditLog.Middleware then read the ring.
// The /v1/admin/audit route only registers when WithAudit ran before
// RegisterRoutes, so this builds its own stack (same wiring as
// setupTestServer plus the audit ring).
func TestAdminAuditEndpoint(t *testing.T) {
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	regSvc := registry.New(mem, mem, logger)
	dirSvc := directory.New(mem)
	admSvc := admin.New(mem)
	events := httpadapter.New()
	_ = events.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(context.Background(), e)
		return err
	})
	rtSvc := routing.New(mem, mem, mem, mem)

	audit := NewAuditLog(100, 4096, logger)
	crossSvc := crossserver.New(mem, mem, events, logger)
	handler := New(regSvc, discovery.New(mem, mem), dirSvc, admSvc, rtSvc, crossSvc, mem, events, logger).WithAudit(audit)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ts := httptest.NewServer(audit.Middleware(mux))
	defer ts.Close()

	registerTestServer(t, ts, "srv-1")
	resp := postJSON(t, ts, "/v1/admin/servers/srv-1/maintenance", map[string]any{})
	resp.Body.Close()

	resp = getJSON(t, ts, "/v1/admin/audit?limit=10")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Entries []map[string]any `json:"entries"`
	}
	decodeInto(t, resp, &body)
	if len(body.Entries) == 0 {
		t.Fatal("audit ring empty after admin mutation")
	}
}

// TestReadyzReady covers the readiness endpoint's ready path (the not-ready
// branch lives in handlers_test.go).
func TestReadyzReady(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	if resp := getJSON(t, ts, "/readyz"); resp.StatusCode != http.StatusOK {
		t.Errorf("readyz = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}
}

// TestCORSMiddleware pins the allowlist contract: empty config emits no
// CORS headers at all; only origins from ATLAS_CORS_ORIGINS get headers
// (echoed, with Vary); "*" is explicit allow-all; everything outside the
// allowlist passes through untouched.
func TestCORSMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	allowed := ""
	req := func(origin, method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/x", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		CORSMiddleware(allowed)(next).ServeHTTP(rec, r)
		return rec
	}

	// Unconfigured: nothing opens, preflight is not blessed either.
	rec := req("https://app.example.com", http.MethodOptions)
	if rec.Code != http.StatusTeapot {
		t.Errorf("unconfigured OPTIONS = %d, want passthrough 418", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("unconfigured request must not emit CORS headers")
	}

	// Allowlisted origin: headers echo the origin, preflight is 204.
	allowed = "https://app.example.com, https://admin.example.com"
	rec = req("https://app.example.com", http.MethodGet)
	if rec.Code != http.StatusTeapot {
		t.Errorf("allowlisted GET = %d, want 418 (next not called?)", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("allow-origin = %q, want the echoed origin", got)
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Error("Vary: Origin missing for allowlisted origin")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("allow-methods header missing")
	}
	rec = req("https://app.example.com", http.MethodOptions)
	if rec.Code != http.StatusNoContent {
		t.Errorf("allowlisted OPTIONS = %d, want 204", rec.Code)
	}

	// Origin outside the allowlist: no headers, request untouched.
	rec = req("https://evil.example.com", http.MethodGet)
	if rec.Code != http.StatusTeapot {
		t.Errorf("non-allowlisted GET = %d, want passthrough 418", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("non-allowlisted origin must not get CORS headers")
	}
	rec = req("https://evil.example.com", http.MethodOptions)
	if rec.Code != http.StatusTeapot {
		t.Errorf("non-allowlisted OPTIONS = %d, want passthrough (no 204)", rec.Code)
	}

	// No Origin header (same-origin / machine traffic): untouched.
	rec = req("", http.MethodGet)
	if rec.Code != http.StatusTeapot {
		t.Errorf("originless GET = %d, want passthrough 418", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("originless request must not emit CORS headers")
	}

	// Explicit "*": blanket allow (development configuration).
	allowed = "*"
	rec = req("https://anything.example.net", http.MethodGet)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf(`"*" config allow-origin = %q, want *`, got)
	}
	rec = req("https://anything.example.net", http.MethodOptions)
	if rec.Code != http.StatusNoContent {
		t.Errorf(`"*" config OPTIONS = %d, want 204`, rec.Code)
	}
}

// TestRegistryAuthMiddleware covers token enforcement modes.
func TestRegistryAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// No tokens configured → passthrough (open registry).
	open := RegistryAuth(RegistryAuthConfig{Logger: quietLogger()})
	rec := httptest.NewRecorder()
	open(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/registry/x", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("open registry = %d, want 200", rec.Code)
	}

	// With tokens: missing / wrong / correct.
	secured := RegistryAuth(RegistryAuthConfig{
		Tokens: map[string]struct{}{"secret-token": {}},
		Logger: quietLogger(),
	})

	rec = httptest.NewRecorder()
	secured(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/registry/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing token = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/registry/x", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	secured(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/registry/x", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	secured(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid token = %d, want 200", rec.Code)
	}
}

// quietLogger silences middleware warnings in tests.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestAdminLifecycleInvalidTransitions covers the ErrInvalid → 400 mapping.
func TestAdminLifecycleInvalidTransitions(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-1")

	// disable first (operator state)…
	if resp := postJSON(t, ts, "/v1/admin/servers/srv-1/disable", map[string]any{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("disable = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}

	// …then maintenance / drain from disabled are invalid transitions.
	for _, path := range []string{
		"/v1/admin/servers/srv-1/maintenance",
		"/v1/admin/servers/srv-1/drain",
	} {
		if resp := postJSON(t, ts, path, map[string]any{}); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s from disabled = %d, want 400", path, resp.StatusCode)
			resp.Body.Close()
		}
	}
}

// TestWriteEventErrorMapping covers the event error → status mapping table.
func TestWriteEventErrorMapping(t *testing.T) {
	h := &Handler{}
	cases := []struct {
		err  error
		want int
	}{
		{model.ErrInvalid, http.StatusBadRequest},
		{store.ErrNotFound, http.StatusNotFound},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.writeEventError(rec, tc.err)
		if rec.Code != tc.want {
			t.Errorf("writeEventError(%v) = %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}

// TestDeref covers the nil-safe pointer dereference helper.
func TestDeref(t *testing.T) {
	if got := deref(nil); got != "" {
		t.Errorf("deref(nil) = %q, want empty", got)
	}
	s := "x"
	if got := deref(&s); got != "x" {
		t.Errorf("deref(&x) = %q, want x", got)
	}
}

// TestNewAuditLogDefaults: non-positive limits fall back to defaults.
func TestNewAuditLogDefaults(t *testing.T) {
	a := NewAuditLog(0, -1, nil)
	if a == nil {
		t.Fatal("nil audit log")
	}
}

// TestDiscoveryQueryParams covers the list endpoint's filter parameters.
func TestDiscoveryQueryParams(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-1")

	for _, q := range []string{
		"?status=online",
		"?region=cn-east",
		"?limit=10",
		"?status=offline", // explicit status bypasses visibility default
	} {
		if resp := getJSON(t, ts, "/v1/discovery/servers"+q); resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", q, resp.StatusCode)
			resp.Body.Close()
		}
	}

	// Characters by account (empty list is fine).
	if resp := getJSON(t, ts, "/v1/directory/accounts/42/characters"); resp.StatusCode != http.StatusOK {
		t.Errorf("by-account = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}

	// Admin character search.
	if resp := getJSON(t, ts, "/v1/admin/characters/search?q=gal"); resp.StatusCode != http.StatusOK {
		t.Errorf("search = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}

	// Migration list with limit param.
	if resp := getJSON(t, ts, "/v1/admin/migrations?limit=5"); resp.StatusCode != http.StatusOK {
		t.Errorf("migrations?limit = %d, want 200", resp.StatusCode)
		resp.Body.Close()
	}
}
