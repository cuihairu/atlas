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
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// setupFailingServer wires the exact same service stack as setupTestServer,
// but over a store whose every method returns one injected error. Every
// handler branch that turns a store failure into a 5xx response is then
// reachable with a plain request — no per-route fixture surgery.
func setupFailingServer(t *testing.T) *httptest.Server {
	t.Helper()
	forced := errors.New("store unavailable: forced failure")
	fail := &failingStore{Store: memory.New(), err: forced}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	regSvc := registry.New(fail, fail, logger)
	discSvc := discovery.New(fail, fail)
	dirSvc := directory.New(fail)
	admSvc := admin.New(fail)
	events := httpadapter.New()
	_ = events.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(context.Background(), e)
		return err
	})

	rtSvc := routing.New(fail, fail, fail, fail)
	crossSvc := crossserver.New(fail, fail, events, logger)
	handler := New(regSvc, discSvc, dirSvc, admSvc, rtSvc, crossSvc, fail, events, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return httptest.NewServer(mux)
}

func doRequest(t *testing.T, ts *httptest.Server, method, path string, body string) *http.Response {
	t.Helper()
	var rd *bytes.Reader
	if body == "" {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, ts.URL+path, rd)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// TestStoreFailureSweep pins that a failing store surfaces as a 5xx on every
// store-backed route, with a valid body so the failure comes from the store
// and not from request validation.
func TestStoreFailureSweep(t *testing.T) {
	ts := setupFailingServer(t)
	defer ts.Close()

	window := `{"start_at":"2026-01-01T00:00:00Z","end_at":"2026-01-01T01:00:00Z"}`
	sweep := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"register", "POST", "/v1/registry/servers/register", `{"server_id":"srv-1","name":"S","type":"game","region":"cn-east","version":"1.0.0","platform":"android","endpoint":{"host":"10.0.0.1","port":30001},"capacity":100}`, 500},
		{"heartbeat", "POST", "/v1/registry/servers/srv-1/heartbeat", `{"players":1,"load":0.1}`, 500},
		{"unregister", "POST", "/v1/registry/servers/srv-1/unregister", "", 500},
		{"crossserver config pull", "GET", "/v1/crossserver/config", "", 500},
		{"discovery list", "GET", "/v1/discovery/servers", "", 500},
		{"discovery get", "GET", "/v1/discovery/servers/srv-1", "", 500},
		{"discovery announcements", "GET", "/v1/discovery/announcements", "", 500},
		{"routing recommended", "GET", "/v1/routing/recommended", "", 500},
		{"character create", "POST", "/v1/directory/characters", `{"account_id":1,"server_id":"srv-1","character_id":1,"name":"Hero","level":1,"class_id":1}`, 500},
		{"characters by account", "GET", "/v1/directory/accounts/1/characters", "", 500},
		{"character get", "GET", "/v1/directory/characters/1", "", 500},
		{"characters by server", "GET", "/v1/directory/servers/srv-1/characters", "", 500},
		{"character patch", "PATCH", "/v1/directory/characters/1", `{"level":2}`, 500},
		{"character delete", "DELETE", "/v1/directory/characters/1", "", 500},
		{"admin maintenance", "POST", "/v1/admin/servers/srv-1/maintenance", "", 500},
		{"admin drain", "POST", "/v1/admin/servers/srv-1/drain", "", 500},
		{"admin enable", "POST", "/v1/admin/servers/srv-1/enable", "", 500},
		{"admin disable", "POST", "/v1/admin/servers/srv-1/disable", "", 500},
		{"admin delete server", "DELETE", "/v1/admin/servers/srv-1", "", 500},
		{"admin set weight", "POST", "/v1/admin/servers/srv-1/weight", `{"weight":5}`, 500},
		{"admin stats", "GET", "/v1/admin/stats", "", 500},
		{"admin list servers", "GET", "/v1/admin/servers", "", 500},
		{"admin search characters", "GET", "/v1/admin/characters/search?q=Hero", "", 500},
		{"admin diagnose routing", "GET", "/v1/admin/diagnose/routing?account_id=1", "", 500},
		{"admin create migration", "POST", "/v1/admin/migrations", `{"source_servers":["srv-1"],"target_server":"srv-2"}`, 500},
		{"admin list migrations", "GET", "/v1/admin/migrations", "", 500},
		{"admin get migration", "GET", "/v1/admin/migrations/m-1", "", 500},
		{"admin rollback migration", "POST", "/v1/admin/migrations/m-1/rollback", "", 500},
		{"admin create realm", "POST", "/v1/admin/realms", `{"id":"realm-1","name":"Realm","region":"cn","status":"active"}`, 500},
		{"admin list realms", "GET", "/v1/admin/realms", "", 500},
		{"admin create shard", "POST", "/v1/admin/shards", `{"id":"shard-1","realm_id":"realm-1","name":"Shard","status":"active"}`, 500},
		{"admin list shards", "GET", "/v1/admin/shards", "", 500},
		{"admin get tags", "GET", "/v1/admin/servers/srv-1/tags", "", 500},
		{"admin add tag", "POST", "/v1/admin/servers/srv-1/tags", `{"code":"beta","label":"Beta","tier":"new","public":true}`, 500},
		{"admin remove tag", "DELETE", "/v1/admin/servers/srv-1/tags/beta", "", 500},
		{"admin create maintenance window", "POST", "/v1/admin/servers/srv-1/maintenance-window", window, 500},
		{"admin list maintenance windows", "GET", "/v1/admin/maintenance-windows", "", 500},
		{"admin delete maintenance window", "DELETE", "/v1/admin/maintenance-windows/mw-1", "", 500},
		{"admin create announcement", "POST", "/v1/admin/announcements", `{"title":"T","body":"B","level":"info","starts_at":"2027-01-01T00:00:00Z","ends_at":"2027-01-01T01:00:00Z"}`, 500},
		{"admin list announcements", "GET", "/v1/admin/announcements", "", 500},
		{"admin delete announcement", "DELETE", "/v1/admin/announcements/a-1", "", 500},
		{"admin get crossserver config", "GET", "/v1/admin/crossserver/config", "", 500},
		{"admin update crossserver config", "PUT", "/v1/admin/crossserver/config", `{"cross_server":true,"queue":{"uri":"memory://","topics":{"characters":"characters"}}}`, 500},
		{"readyz with failing store", "GET", "/readyz", "", 503},
	}

	for _, tc := range sweep {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, ts, tc.method, tc.path, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("%s %s: expected %d, got %d", tc.method, tc.path, tc.want, resp.StatusCode)
			}
		})
	}
}

// TestBatchMaintenanceWindowsReportsPerServerFailure pins the batch
// endpoint's multi-outcome contract: a store failure on one server lands in
// the response's failed slice (HTTP 200), never masked and never a 5xx that
// would hide the per-server verdicts for the other servers.
func TestBatchMaintenanceWindowsReportsPerServerFailure(t *testing.T) {
	ts := setupFailingServer(t)
	defer ts.Close()

	resp := doRequest(t, ts, "POST", "/v1/admin/maintenance-windows/batch",
		`{"server_ids":["srv-1","srv-2"],"end_at":"2027-01-01T01:00:00Z"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch windows: expected 200 multi-outcome, got %d", resp.StatusCode)
	}
	var body struct {
		Windows []struct {
			ServerID string `json:"server_id"`
		} `json:"windows"`
		Failed []struct {
			ServerID string `json:"server_id"`
			Error    string `json:"error"`
		} `json:"failed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode batch result: %v", err)
	}
	if len(body.Windows) != 0 {
		t.Errorf("expected no windows created, got %d", len(body.Windows))
	}
	if len(body.Failed) != 2 {
		t.Fatalf("expected 2 per-server failures, got %d: %+v", len(body.Failed), body.Failed)
	}
	for _, f := range body.Failed {
		if f.ServerID == "" || f.Error == "" {
			t.Errorf("failed entry missing server_id or error detail: %+v", f)
		}
	}
}

// TestBadJSONSweep pins the decode-error branch on every body-consuming
// route: malformed JSON must be a 400 INVALID_ARGUMENT, never a panic or a
// 500 attributed to the store.
func TestBadJSONSweep(t *testing.T) {
	ts := setupFailingServer(t)
	defer ts.Close()

	bad := []struct {
		name   string
		method string
		path   string
	}{
		{"register", "POST", "/v1/registry/servers/register"},
		{"heartbeat", "POST", "/v1/registry/servers/srv-1/heartbeat"},
		{"character create", "POST", "/v1/directory/characters"},
		{"character patch", "PATCH", "/v1/directory/characters/1"},
		{"admin set weight", "POST", "/v1/admin/servers/srv-1/weight"},
		{"admin create migration", "POST", "/v1/admin/migrations"},
		{"admin create realm", "POST", "/v1/admin/realms"},
		{"admin create shard", "POST", "/v1/admin/shards"},
		{"admin add tag", "POST", "/v1/admin/servers/srv-1/tags"},
		{"admin create maintenance window", "POST", "/v1/admin/servers/srv-1/maintenance-window"},
		{"admin batch maintenance windows", "POST", "/v1/admin/maintenance-windows/batch"},
		{"admin create announcement", "POST", "/v1/admin/announcements"},
		{"admin update crossserver config", "PUT", "/v1/admin/crossserver/config"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, ts, tc.method, tc.path, `{"nope`)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("%s %s with malformed body: expected 400, got %d", tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}
