package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/audit"
	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
	"github.com/cuihairu/atlas/internal/telemetry"
)

// setupServerWithEvents mirrors setupTestServer but lets a test swap the
// event adapter (async stubs, counting wrapper, failing publishes), attach
// optional panels (telemetry sampler, bus counters), or build the handler
// without a cross-server config service (the 503 CONFIG_CENTER_DISABLED
// contract).
func setupServerWithEvents(t *testing.T, events event.EventAdapter, attach func(h *Handler)) (*httptest.Server, *directory.Service) {
	t.Helper()
	mem := memory.New()
	logger := quietLogger()
	regSvc := registry.New(mem, mem, logger)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem)
	admSvc := admin.New(mem)
	if events == nil {
		base := httpadapter.New()
		_ = base.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
			_, err := dirSvc.ApplyEvent(context.Background(), e)
			return err
		})
		events = base
	}

	crossSvc := crossserver.New(mem, mem, events, logger)
	handler := New(regSvc, discSvc, dirSvc, admSvc, routing.New(mem, mem, mem, mem), crossSvc, mem, events, logger)
	if attach != nil {
		attach(handler)
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return httptest.NewServer(mux), dirSvc
}

// stubEventAdapter overrides publish behavior without a real bus: sync or
// async, success or failure. Everything else is inert (nil embed).
type stubEventAdapter struct {
	event.EventAdapter
	sync   bool
	pubErr error
}

func (s *stubEventAdapter) Synchronous() bool { return s.sync }
func (s *stubEventAdapter) Publish(_ context.Context, _ *event.Event) error {
	return s.pubErr
}
func (s *stubEventAdapter) Subscribe(_ context.Context, _ string, _ event.Handler) error {
	return nil
}

// heartbeatServer promotes a freshly registered (starting) server to
// online so status-scoped listings and recommendations can see it.
func heartbeatServer(t *testing.T, ts *httptest.Server, id string) {
	t.Helper()
	resp := postJSON(t, ts, "/v1/registry/servers/"+id+"/heartbeat", map[string]any{
		"players": 1, "load": 0.1,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat %s: expected 200, got %d", id, resp.StatusCode)
	}
}

// TestDiscoveryListQueryValidation pins the reject side of the public list's
// query contract: bad status and bad limit are 400s (the happy combos are
// covered by TestDiscoveryQueryParams).
func TestDiscoveryListQueryValidation(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-a")

	for _, q := range []string{"?status=bogus", "?limit=0", "?limit=abc"} {
		resp := getJSON(t, ts, "/v1/discovery/servers"+q)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("list %q: expected 400, got %d", q, resp.StatusCode)
		}
	}
}

// TestRoutingRecommendedQueryValidation pins the account_id parse branch.
func TestRoutingRecommendedQueryValidation(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-a")
	heartbeatServer(t, ts, "srv-a")

	resp := getJSON(t, ts, "/v1/routing/recommended?account_id=notanumber")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("recommended with bad account_id: expected 400, got %d", resp.StatusCode)
	}
	resp = getJSON(t, ts, "/v1/routing/recommended?account_id=42")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("recommended with account_id: expected 200, got %d", resp.StatusCode)
	}
}

// TestAdminListServersFiltersAndCursor pins the fallback (no fleet index)
// admin list: filters, limit and cursor pagination.
func TestAdminListServersFiltersAndCursor(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-a")
	registerTestServer(t, ts, "srv-b")
	heartbeatServer(t, ts, "srv-a")
	heartbeatServer(t, ts, "srv-b")

	get := func(q string) (servers []map[string]any, next string, code int) {
		resp := getJSON(t, ts, "/v1/admin/servers"+q)
		defer resp.Body.Close()
		code = resp.StatusCode
		if code != http.StatusOK {
			return
		}
		var body struct {
			Servers    []map[string]any `json:"servers"`
			NextCursor string           `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode %q: %v", q, err)
		}
		return body.Servers, body.NextCursor, code
	}

	if _, _, code := get("?limit=abc"); code != http.StatusBadRequest {
		t.Errorf("bad limit: expected 400, got %d", code)
	}

	page1, next, code := get("?limit=1")
	if code != http.StatusOK || len(page1) != 1 {
		t.Fatalf("limit=1: expected 1 server, got %d (code %d)", len(page1), code)
	}
	if next == "" {
		t.Fatal("limit=1 did not return a next_cursor")
	}
	page2, _, _ := get("?limit=1&cursor=" + next)
	if len(page2) != 1 || page2[0]["id"] == page1[0]["id"] {
		t.Fatalf("cursor pagination returned the same page: %+v then %+v", page1, page2)
	}

	byID, _, _ := get("?id=srv-b")
	if len(byID) != 1 || byID[0]["id"] != "srv-b" {
		t.Fatalf("id filter: expected only srv-b, got %+v", byID)
	}
	byStatus, _, _ := get("?status=online")
	if len(byStatus) != 2 {
		t.Fatalf("status filter: expected 2 online servers, got %d", len(byStatus))
	}
}

// TestAdminSearchCharactersParams pins the search filter parsing (account_id,
// min_level, limit) on the normal store.
func TestAdminSearchCharactersParams(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-a")
	resp := postJSON(t, ts, "/v1/directory/characters", map[string]any{
		"account_id": 10001, "server_id": "srv-a", "character_id": 1,
		"name": "Hero", "level": 10, "class_id": 1,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create char: %d", resp.StatusCode)
	}
	resp.Body.Close()

	for _, q := range []string{"?account_id=abc", "?min_level=x", "?limit=0"} {
		resp := getJSON(t, ts, "/v1/admin/characters/search"+q)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("search %q: expected 400, got %d", q, resp.StatusCode)
		}
	}
	resp = getJSON(t, ts, "/v1/admin/characters/search?account_id=10001&min_level=1&limit=5")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("search with filters: expected 200, got %d", resp.StatusCode)
	}
}

// TestDiagnoseRoutingParams pins diagnose's status validation and the happy
// walk (recommendation plus the account's characters).
func TestDiagnoseRoutingParams(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-a")

	resp := getJSON(t, ts, "/v1/admin/diagnose/routing?status=bogus")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("diagnose bad status: expected 400, got %d", resp.StatusCode)
	}
	resp = getJSON(t, ts, "/v1/admin/diagnose/routing?account_id=10001")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("diagnose happy path: expected 200, got %d", resp.StatusCode)
	}
}

// TestSeriesWindowParams pins the shared window tiers on both series
// endpoints plus the heatmap window validation.
func TestSeriesWindowParams(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	for _, w := range []string{"", "5m", "10m", "30m", "1h", "10h"} {
		for _, path := range []string{"/v1/admin/load-series", "/v1/admin/bus-series"} {
			resp := getJSON(t, ts, path+"?window="+w)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s?window=%s: expected 200, got %d", path, w, resp.StatusCode)
			}
		}
	}
	for _, path := range []string{"/v1/admin/load-series", "/v1/admin/bus-series"} {
		resp := getJSON(t, ts, path+"?window=bogus")
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s?window=bogus: expected 400, got %d", path, resp.StatusCode)
		}
	}
	// Heatmap only allows 1h/24h.
	for _, w := range []string{"", "1h", "24h"} {
		resp := getJSON(t, ts, "/v1/admin/load-heatmap?window="+w)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("heatmap?window=%q: expected 200, got %d", w, resp.StatusCode)
		}
	}
	resp := getJSON(t, ts, "/v1/admin/load-heatmap?window=bogus")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("heatmap?window=bogus: expected 400, got %d", resp.StatusCode)
	}
}

// TestLoadSeriesScopes pins the scope resolution (fleet / server / region)
// and the players-load merge over real sampler rings.
func TestLoadSeriesScopes(t *testing.T) {
	sampler := telemetry.NewSampler(time.Millisecond, time.Minute)
	sampler.Probe(func() map[string]float64 {
		return map[string]float64{
			"load.fleet.players":        10,
			"load.fleet.load":           0.2,
			"load.server.srv-a.players": 5,
			"load.server.srv-a.load":    0.1,
			"load.region.cn.players":    7,
			"load.region.cn.load":       0.3,
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	go sampler.Run(ctx)
	defer cancel()
	time.Sleep(20 * time.Millisecond)

	ts, _ := setupServerWithEvents(t, nil, func(h *Handler) {
		h.WithTelemetry(sampler)
	})
	defer ts.Close()

	get := func(path string) (scope string, n int, code int) {
		resp := getJSON(t, ts, path)
		defer resp.Body.Close()
		code = resp.StatusCode
		if code != http.StatusOK {
			return
		}
		var body struct {
			Scope  string `json:"scope"`
			Points []struct {
				Players float64 `json:"players"`
				Load    float64 `json:"load"`
			} `json:"points"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return body.Scope, len(body.Points), code
	}

	for _, tc := range []struct{ q, scope string }{
		{"", "fleet"},
		{"?server_id=srv-a", "server"},
		{"?region=cn", "region"},
	} {
		scope, n, code := get("/v1/admin/load-series" + tc.q)
		if code != http.StatusOK {
			t.Errorf("load-series %q: expected 200, got %d", tc.q, code)
			continue
		}
		if scope != tc.scope {
			t.Errorf("load-series %q: expected scope %q, got %q", tc.q, tc.scope, scope)
		}
		if n == 0 {
			t.Errorf("load-series %q: expected sampled points, got none", tc.q)
		}
	}
}

// TestBusSeriesWithCountingAdapter pins the 消息总线 panel wiring: depth
// rings from the sampler plus live counters from the counting adapter.
func TestBusSeriesWithCountingAdapter(t *testing.T) {
	base := httpadapter.New()
	counting := event.WrapCounting(base)
	if err := counting.Subscribe(context.Background(), event.TopicCharacters, func(context.Context, *event.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := counting.Publish(context.Background(), &event.Event{Type: event.EventCharacterUpdated}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	sampler := telemetry.NewSampler(time.Millisecond, time.Minute)
	sampler.Probe(func() map[string]float64 {
		return map[string]float64{
			"bus.atlas.characters.depth":     2,
			"bus.atlas.characters.published": 4,
			"bus.atlas.characters.consumed":  4,
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	go sampler.Run(ctx)
	defer cancel()
	time.Sleep(20 * time.Millisecond)

	ts, _ := setupServerWithEvents(t, counting, func(h *Handler) {
		h.WithTelemetry(sampler)
		h.WithBusStats(counting)
	})
	defer ts.Close()

	resp := getJSON(t, ts, "/v1/admin/bus-series")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bus-series: expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Adapter string `json:"adapter"`
		Topics  []struct {
			Topic string `json:"topic"`
			Depth []any  `json:"depth"`
		} `json:"topics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode bus-series: %v", err)
	}
	if body.Adapter == "" {
		t.Error("bus-series did not report the counting adapter name")
	}
	found := false
	for _, tp := range body.Topics {
		if tp.Topic == "atlas.characters" && len(tp.Depth) > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("bus-series missing atlas.characters topic with depth: %+v", body.Topics)
	}
}

// TestCharacterEventAdapterBranches pins the event-adapter contract of the
// directory verbs: async adapters answer 202 queued, publish failures map
// through writeEventError, and parse errors stay 400.
func TestCharacterEventAdapterBranches(t *testing.T) {
	charBody := `{"account_id":10001,"server_id":"srv-a","character_id":1,"name":"Hero","level":1,"class_id":1}`

	t.Run("async adapter queues", func(t *testing.T) {
		ts, dirSvc := setupServerWithEvents(t, &stubEventAdapter{sync: false}, nil)
		defer ts.Close()
		registerTestServer(t, ts, "srv-a")

		resp := doRequest(t, ts, "POST", "/v1/directory/characters", charBody)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("create via async adapter: expected 202, got %d", resp.StatusCode)
		}
		resp = doRequest(t, ts, "PATCH", "/v1/directory/characters/1", `{"level":2}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("patch via async adapter: expected 202, got %d", resp.StatusCode)
		}

		// Delete enqueues only for characters the directory already has:
		// apply one created event directly, then delete through the API.
		if _, err := dirSvc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterCreated, AccountID: 10001,
			ServerID: "srv-a", CharacterID: 2, Name: "Seeded",
		}); err != nil {
			t.Fatalf("seed character: %v", err)
		}
		resp = doRequest(t, ts, "DELETE", "/v1/directory/characters/2", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("delete via async adapter: expected 202, got %d", resp.StatusCode)
		}
	})

	t.Run("publish failure surfaces", func(t *testing.T) {
		ts, dirSvc := setupServerWithEvents(t, &stubEventAdapter{sync: true, pubErr: errors.New("bus down")}, nil)
		defer ts.Close()
		registerTestServer(t, ts, "srv-a")

		resp := doRequest(t, ts, "POST", "/v1/directory/characters", charBody)
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("create with failing publish: expected 500, got %d", resp.StatusCode)
		}
		resp = doRequest(t, ts, "PATCH", "/v1/directory/characters/1", `{"level":2}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("patch with failing publish: expected 500, got %d", resp.StatusCode)
		}

		if _, err := dirSvc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterCreated, AccountID: 10001,
			ServerID: "srv-a", CharacterID: 3, Name: "Seeded",
		}); err != nil {
			t.Fatalf("seed character: %v", err)
		}
		resp = doRequest(t, ts, "DELETE", "/v1/directory/characters/3", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("delete with failing publish: expected 500, got %d", resp.StatusCode)
		}
	})

	t.Run("bad ids are 400", func(t *testing.T) {
		ts, _ := setupServerWithEvents(t, nil, nil)
		defer ts.Close()
		resp := doRequest(t, ts, "DELETE", "/v1/directory/characters/notanumber", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("delete with bad id: expected 400, got %d", resp.StatusCode)
		}
	})
}

// TestCrossServerConfigNilService pins the 503 CONFIG_CENTER_DISABLED
// contract when the config center is not wired.
func TestCrossServerConfigNilService(t *testing.T) {
	mem := memory.New()
	logger := quietLogger()
	regSvc := registry.New(mem, mem, logger)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem)
	admSvc := admin.New(mem)
	events := httpadapter.New()
	handler := New(regSvc, discSvc, dirSvc, admSvc, routing.New(mem, mem, mem, mem), nil, mem, events, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp := doRequest(t, ts, "GET", "/v1/crossserver/config", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("pull without config center: expected 503, got %d", resp.StatusCode)
	}
	resp = doRequest(t, ts, "PUT", "/v1/admin/crossserver/config", `{"cross_server":true}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("put without config center: expected 503, got %d", resp.StatusCode)
	}
}

// TestCrossServerConfigSinceStoreError pins the conditional-pull 500: a
// store that fails during a versioned poll must not be reported as 304.
func TestCrossServerConfigSinceStoreError(t *testing.T) {
	ts := setupFailingServer(t)
	defer ts.Close()

	resp := doRequest(t, ts, "GET", "/v1/crossserver/config?version=1&hash=x", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("conditional pull with failing store: expected 500, got %d", resp.StatusCode)
	}
	resp = doRequest(t, ts, "GET", "/v1/crossserver/config?version=abc", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad version: expected 400, got %d", resp.StatusCode)
	}
}

// TestNotFoundSweep pins the not-found mapping of the read verbs against
// the real store: unknown ids are 404 with the documented error codes.
func TestNotFoundSweep(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"server get", "GET", "/v1/discovery/servers/nope"},
		{"character get", "GET", "/v1/directory/characters/999"},
		{"character delete", "DELETE", "/v1/directory/characters/999"},
		{"migration get", "GET", "/v1/admin/migrations/m-nope"},
		{"migration rollback", "POST", "/v1/admin/migrations/m-nope/rollback"},
		{"tags get", "GET", "/v1/admin/servers/nope/tags"},
		{"tag remove", "DELETE", "/v1/admin/servers/nope/tags/beta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, ts, tc.method, tc.path, "")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s: expected 404, got %d", tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}

// TestServerTagLifecycle pins the happy path of the admin tag verbs on a
// registered server.
func TestServerTagLifecycle(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-a")

	resp := doRequest(t, ts, "POST", "/v1/admin/servers/srv-a/tags", `{"code":"beta","label":"Beta","tier":"new","public":true}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add tag: expected 200, got %d", resp.StatusCode)
	}
	resp = getJSON(t, ts, "/v1/admin/servers/srv-a/tags")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get tags: expected 200, got %d", resp.StatusCode)
	}
	resp = doRequest(t, ts, "DELETE", "/v1/admin/servers/srv-a/tags/beta", "")
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Errorf("remove tag: expected 2xx, got %d", resp.StatusCode)
	}
}

// TestListEndpointsLimitParam pins the ?limit parsing on the remaining list
// verbs (realms, shards, maintenance windows, announcements) against a
// populated store.
func TestListEndpointsLimitParam(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-a")

	// Populate one entity of each kind.
	resp := doRequest(t, ts, "POST", "/v1/admin/realms", `{"id":"realm-1","name":"Realm","region":"cn","status":"active"}`)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("create realm: %d", resp.StatusCode)
	}
	resp = doRequest(t, ts, "POST", "/v1/admin/shards", `{"id":"shard-1","realm_id":"realm-1","name":"Shard","status":"active"}`)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("create shard: %d", resp.StatusCode)
	}
	resp = doRequest(t, ts, "POST", "/v1/admin/servers/srv-a/maintenance-window", `{"start_at":"2027-01-01T00:00:00Z","end_at":"2027-01-01T01:00:00Z"}`)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("create window: %d", resp.StatusCode)
	}
	resp = doRequest(t, ts, "POST", "/v1/admin/announcements", `{"title":"T","body":"B","level":"info","starts_at":"2027-01-01T00:00:00Z","ends_at":"2027-01-01T01:00:00Z"}`)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("create announcement: %d", resp.StatusCode)
	}

	for _, path := range []string{
		"/v1/admin/realms?limit=1",
		"/v1/admin/shards?limit=1&realm_id=realm-1",
		"/v1/admin/maintenance-windows?limit=1&server_id=srv-a",
		"/v1/admin/announcements?limit=1",
		"/v1/discovery/announcements?limit=1",
		"/v1/directory/servers/srv-a/characters?limit=1",
		"/v1/directory/accounts/10001/characters?limit=1",
	} {
		resp := getJSON(t, ts, path)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", path, resp.StatusCode)
		}
	}
}

// TestRegistryAuthTokenAndIPWhitelist extends TestRegistryAuthMiddleware
// with the X-Atlas-Token header variant and the IP-whitelist gate.
func TestRegistryAuthTokenAndIPWhitelist(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_, allowLoopback, _ := net.ParseCIDR("127.0.0.0/8")
	_, allowInternal, _ := net.ParseCIDR("10.0.0.0/8")
	tokens := map[string]struct{}{"secret": {}}

	cases := []struct {
		name   string
		cfg    RegistryAuthConfig
		header func(*http.Request)
		want   int
	}{
		{
			"x-atlas-token accepted",
			RegistryAuthConfig{Tokens: tokens, Logger: quietLogger()},
			func(r *http.Request) { r.Header.Set("X-Atlas-Token", "secret") },
			http.StatusOK,
		},
		{
			"ip outside whitelist blocked",
			RegistryAuthConfig{IPWhitelist: []*net.IPNet{allowInternal}, Logger: quietLogger()},
			nil,
			http.StatusForbidden,
		},
		{
			"ip inside whitelist and token ok",
			RegistryAuthConfig{Tokens: tokens, IPWhitelist: []*net.IPNet{allowLoopback}, Logger: quietLogger()},
			func(r *http.Request) { r.Header.Set("Authorization", "Bearer secret") },
			http.StatusOK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(RegistryAuth(tc.cfg)(ok))
			defer ts.Close()
			req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/registry/servers/register", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.header != nil {
				tc.header(req)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}

// TestActorContextRoundTrip pins actor propagation: what AdminAuth stores
// is exactly what ActorFrom returns to the audit layer.
func TestActorContextRoundTrip(t *testing.T) {
	if got := ActorFrom(context.Background()); got != (Actor{}) {
		t.Errorf("empty context should yield the zero actor, got %+v", got)
	}
	a := Actor{KeyFingerprint: "fp", Role: "admin"}
	got := ActorFrom(WithActor(context.Background(), a))
	if got != a {
		t.Errorf("round trip: expected %+v, got %+v", a, got)
	}
}

// TestParseIPWhitelistVariants pins the whitelist parser: empty input,
// CIDRs, bare IPs (v4/v6), and garbage parts being skipped.
func TestParseIPWhitelistVariants(t *testing.T) {
	nets, err := ParseIPWhitelist("")
	if err != nil || len(nets) != 0 {
		t.Errorf("empty input: expected no nets, got %v (%v)", nets, err)
	}
	nets, err = ParseIPWhitelist("10.0.0.0/8, 192.168.1.5")
	if err != nil {
		t.Fatalf("mixed input: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("expected 2 nets, got %d", len(nets))
	}
	if nets[0].String() != "10.0.0.0/8" || nets[1].String() != "192.168.1.5/32" {
		t.Errorf("unexpected nets: %v %v", nets[0], nets[1])
	}
	nets, err = ParseIPWhitelist("::1, not-an-ip,,")
	if err != nil {
		t.Fatalf("v6+garbage input: %v", err)
	}
	if len(nets) != 1 || nets[0].String() != "::1/128" {
		t.Errorf("expected only ::1/128 to survive, got %v", nets)
	}
}

// TestParseRateDefaultVariants pins the default-rule parser: rps:burst,
// rps alone, and rejection of garbage.
func TestParseRateDefaultVariants(t *testing.T) {
	r, err := ParseRateDefault("100:200")
	if err != nil || r.RPS != 100 || r.Burst != 200 {
		t.Errorf(`"100:200": expected 100/200, got %+v (%v)`, r, err)
	}
	r, err = ParseRateDefault("50")
	if err != nil || r.RPS != 50 {
		t.Errorf(`"50": expected rps 50, got %+v (%v)`, r, err)
	}
	if _, err := ParseRateDefault("fast"); err == nil {
		t.Error(`"fast" should not parse`)
	}
}

// TestAuditLogDrainWithoutSink pins the shutdown contract: Drain on a log
// without a sink is a no-op returning zero dropped entries.
func TestAuditLogDrainWithoutSink(t *testing.T) {
	a := NewAuditLog(4, 128, quietLogger())
	if dropped := a.Drain(time.Millisecond); dropped != 0 {
		t.Errorf("expected 0 dropped entries, got %d", dropped)
	}
}

// TestAuditLogDrainFlushesSink pins the wired path: with a dispatcher
// attached, Drain waits for the queue to empty and drops nothing.
func TestAuditLogDrainFlushesSink(t *testing.T) {
	d := audit.NewDispatcher(noopSink{}, 8, time.Minute, nil, quietLogger())
	a := NewAuditLog(4, 128, quietLogger()).WithSink(d)
	if dropped := a.Drain(2 * time.Second); dropped != 0 {
		t.Errorf("expected 0 dropped entries, got %d", dropped)
	}
}

type noopSink struct{}

func (noopSink) Write(context.Context, audit.Entry) error { return nil }
func (noopSink) Close() error                             { return nil }

// TestRequestIDContextRoundTrip pins request-id propagation between the
// REST tracing middleware and the gRPC interceptor that shares the key.
func TestRequestIDContextRoundTrip(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("empty context should carry no request id, got %q", got)
	}
	const id = "req-1"
	if got := RequestIDFromContext(WithRequestID(context.Background(), id)); got != id {
		t.Errorf("round trip: expected %q, got %q", id, got)
	}
}
