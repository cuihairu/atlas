package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/fleet"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
	"github.com/cuihairu/atlas/internal/telemetry"
)

// fleetFixture bundles everything the observability endpoints need: the
// fleet index, the series sampler, the counting bus adapter, and the rate
// limiter — wired exactly like cmd/atlas/main.go does.
type fleetFixture struct {
	ts      *httptest.Server
	idx     *fleet.Index
	sampler *telemetry.Sampler
	bus     *event.CountingAdapter
	limiter *RateLimiter
	events  event.EventAdapter
	dirSvc  *directory.Service
}

// setupFleetServer wires the full BUGS ③ path: the store is wrapped by the
// fleet decorators, and the handler reads the same index for stats, the
// admin server list, and the observability series. The event adapter is the
// counting wrapper around the synchronous http adapter, with the directory
// projection subscribed — the same wiring as main.go.
func setupFleetServer(t *testing.T) (*httptest.Server, *memory.Store, *fleet.Index) {
	t.Helper()
	mem := memory.New()
	fx := setupFleetFixtureOn(t, mem)
	return fx.ts, mem, fx.idx
}

func setupFleetFixture(t *testing.T) *fleetFixture {
	t.Helper()
	return setupFleetFixtureOn(t, memory.New())
}

func setupFleetFixtureOn(t *testing.T, mem *memory.Store) *fleetFixture {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	idx := fleet.New()
	tracked := fleet.TrackStore(mem, idx)
	if err := idx.Reconcile(context.Background(), tracked, tracked); err != nil {
		t.Fatal(err)
	}

	regSvc := registry.New(tracked, tracked, logger)
	discSvc := discovery.New(tracked, tracked)
	dirSvc := directory.New(tracked)
	admSvc := admin.New(tracked)

	inner := httpadapter.New()
	events := event.WrapCounting(inner)

	// Directory projection via the same event pipeline main.go wires —
	// character creates through the API land in the index AND the bus
	// counters in one step.
	evtCtx, evtCancel := context.WithCancel(context.Background())
	t.Cleanup(evtCancel)
	if err := events.Subscribe(evtCtx, event.TopicCharacters, func(ctx context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(ctx, e)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	rtSvc := routing.New(tracked, tracked, tracked, tracked)
	crossSvc := crossserver.New(tracked, tracked, events, logger)

	// Series sampler: probe reads the fleet index (same aggregate the admin
	// views read) and the bus counters — mirrors main.go's probe, but on a
	// test-friendly tick so a value observed after a write lands within the
	// wait budget.
	sampler := telemetry.NewSampler(50*time.Millisecond, 2*time.Second)
	sampler.Probe(func() map[string]float64 {
		out := map[string]float64{}
		players := 0
		for _, srv := range idx.ListAll() {
			players += srv.Players
		}
		out["load.fleet.players"] = float64(players)
		out["load.fleet.load"] = 0
		for _, st := range events.Stats() {
			out["bus."+st.Topic+".depth"] = float64(st.InFlight)
			out["bus."+st.Topic+".produced"] = float64(st.Published)
			out["bus."+st.Topic+".consumed"] = float64(st.Consumed)
		}
		return out
	})
	sampCtx, sampCancel := context.WithCancel(context.Background())
	t.Cleanup(sampCancel)
	go sampler.Run(sampCtx)

	limiter := NewRateLimiter(
		map[string]RateRule{"/v1/discovery": {RPS: 50, Burst: 100}},
		RateRule{RPS: 1000, Burst: 2000},
	)

	handler := New(regSvc, discSvc, dirSvc, admSvc, rtSvc, crossSvc, tracked, events, logger).
		WithFleetIndex(idx).
		WithTelemetry(sampler).
		WithBusStats(events).
		WithRateLimiter(limiter)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return &fleetFixture{
		ts:      httptest.NewServer(mux),
		idx:     idx,
		sampler: sampler,
		bus:     events,
		limiter: limiter,
		events:  events,
		dirSvc:  dirSvc,
	}
}

// registerFleetServer registers through the HTTP API with a type and
// metadata so the type / metadata filters exercise the index path.
func registerFleetServer(t *testing.T, ts *httptest.Server, id, srvType string, metadata map[string]string) {
	t.Helper()
	body := map[string]any{
		"server_id": id,
		"name":      "Test Server",
		"type":      srvType,
		"region":    "cn-east",
		"version":   "1.0.0",
		"platform":  "android",
		"endpoint":  map[string]any{"host": "10.0.0.1", "port": 30001},
		"capacity":  2000,
	}
	if metadata != nil {
		body["metadata"] = metadata
	}
	resp := postJSON(t, ts, "/v1/registry/servers/register", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s: expected 201, got %d", id, resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminStats_ReadsFleetIndex(t *testing.T) {
	ts, _, _ := setupFleetServer(t)
	defer ts.Close()

	registerFleetServer(t, ts, "game-1001", "game", nil)
	registerFleetServer(t, ts, "game-1002", "game", nil)
	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{
		"players": 10, "load": 0.2, "status": "online",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = getJSON(t, ts, "/v1/admin/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: expected 200, got %d", resp.StatusCode)
	}
	var stats struct {
		TotalServers   int            `json:"total_servers"`
		OnlineServers  int            `json:"online_servers"`
		TotalPlayers   int            `json:"total_players"`
		ServersByType  map[string]int `json:"servers_by_type"`
		ServersByTag   map[string]int `json:"servers_by_tag"`
		MetadataKeys   []string       `json:"server_metadata_keys"`
		TotalCharacter int            `json:"total_characters"`
	}
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()

	if stats.TotalServers != 2 || stats.OnlineServers != 1 {
		t.Errorf("servers: total=%d online=%d, want 2/1 (BUGS ①: online derived)", stats.TotalServers, stats.OnlineServers)
	}
	if stats.ServersByType["game"] != 2 {
		t.Errorf("by type: %v", stats.ServersByType)
	}
	if stats.TotalPlayers != 10 {
		t.Errorf("players: %d, want heartbeat value", stats.TotalPlayers)
	}
}

func TestAdminListServers_FiltersAndCursor(t *testing.T) {
	ts, _, _ := setupFleetServer(t)
	defer ts.Close()

	registerFleetServer(t, ts, "game-1001", "game", map[string]string{"cluster": "a"})
	registerFleetServer(t, ts, "game-1002", "game", nil)
	registerFleetServer(t, ts, "web-2001", "web", nil)

	type listResp struct {
		Servers    []map[string]any `json:"servers"`
		NextCursor string           `json:"next_cursor"`
	}

	get := func(query string) listResp {
		t.Helper()
		resp := getJSON(t, ts, "/v1/admin/servers"+query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list %s: expected 200, got %d", query, resp.StatusCode)
		}
		var out listResp
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out
	}

	// ID substring search (item 9).
	if r := get("?id=web"); len(r.Servers) != 1 || r.Servers[0]["id"] != "web-2001" {
		t.Errorf("id search: %+v", r.Servers)
	}
	// Type filter.
	if r := get("?type=game"); len(r.Servers) != 2 {
		t.Errorf("type filter: %d", len(r.Servers))
	}
	// Region + version composability.
	if r := get("?region=cn-east&version=1.0.0"); len(r.Servers) != 3 {
		t.Errorf("region+version: %d", len(r.Servers))
	}
	// Metadata pair filter (register body carries metadata).
	if r := get("?metadata_key=cluster&metadata_value=a"); len(r.Servers) != 1 {
		t.Errorf("metadata filter: %d", len(r.Servers))
	}
	// Cursor pagination walks every record exactly once.
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		q := "?limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		r := get(q)
		for _, srv := range r.Servers {
			id, _ := srv["id"].(string)
			if seen[id] {
				t.Fatalf("server %s repeated across pages", id)
			}
			seen[id] = true
		}
		pages++
		if r.NextCursor == "" {
			break
		}
		cursor = r.NextCursor
	}
	if len(seen) != 3 || pages != 2 {
		t.Errorf("pagination: seen=%d pages=%d, want 3/2", len(seen), pages)
	}
	// Invalid status is rejected, not silently ignored.
	if resp := getJSON(t, ts, "/v1/admin/servers?status=bogus"); resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Errorf("invalid status: expected 400, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

func TestAdminListServers_FallbackWithoutIndex(t *testing.T) {
	// Embedders that never wire the index still get the same response
	// shape through the store-backed fallback.
	ts, _ := setupTestServer(t)
	defer ts.Close()

	registerTestServer(t, ts, "game-1001")

	resp := getJSON(t, ts, "/v1/admin/servers?type=game")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fallback list: expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Servers []map[string]any `json:"servers"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if len(out.Servers) != 1 {
		t.Errorf("fallback: %d servers", len(out.Servers))
	}
}

// TestAdminDiagnoseRouting walks the 排查 endpoint end to end: the winner is
// the owned server (tiebreak #1) and the account's characters ride along.
func TestAdminDiagnoseRouting(t *testing.T) {
	fx := setupFleetFixture(t)
	defer fx.ts.Close()

	registerFleetServer(t, fx.ts, "game-1001", "game", nil)
	registerFleetServer(t, fx.ts, "game-1002", "game", nil)
	for _, id := range []string{"game-1001", "game-1002"} {
		resp := postJSON(t, fx.ts, "/v1/registry/servers/"+id+"/heartbeat", map[string]any{
			"players": 10, "load": 0.2, "status": "online",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("heartbeat %s: %d", id, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Account 42 owns a character on game-1001 (through the same event
	// pipeline main.go wires — also feeds the bus counters).
	resp := postJSON(t, fx.ts, "/v1/directory/characters", map[string]any{
		"account_id": 42, "server_id": "game-1001", "character_id": 9001,
		"name": "hero", "level": 5,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create character: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = getJSON(t, fx.ts, "/v1/admin/diagnose/routing?account_id=42&region=cn-east")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diagnose: expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Diagnosis struct {
			Stage    string `json:"stage"`
			WinnerID string `json:"winner_id"`
			Servers  []struct {
				Server  map[string]any `json:"server"`
				Rank    int            `json:"rank"`
				Owned   bool           `json:"owned"`
				Eligibl bool           `json:"eligible"`
			} `json:"servers"`
		} `json:"diagnosis"`
		Characters []map[string]any `json:"characters"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	if out.Diagnosis.Stage != "strict" {
		t.Errorf("stage: %q, want strict", out.Diagnosis.Stage)
	}
	if out.Diagnosis.WinnerID != "game-1001" {
		t.Errorf("winner: %q, want game-1001 (owned tiebreak)", out.Diagnosis.WinnerID)
	}
	if len(out.Diagnosis.Servers) != 2 {
		t.Errorf("verdicts: %d servers, want the whole fleet", len(out.Diagnosis.Servers))
	}
	if len(out.Characters) != 1 || out.Characters[0]["character_id"].(float64) != 9001 {
		t.Errorf("characters: %+v, want the account's directory entry", out.Characters)
	}

	// Invalid account_id is rejected rather than silently ignored.
	if resp := getJSON(t, fx.ts, "/v1/admin/diagnose/routing?account_id=abc"); resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Errorf("invalid account_id: expected 400, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

// TestAdminRateLimits covers the read-only gateway view: parsed rules,
// default rule, and the enabled=false shape when no limiter is wired.
func TestAdminRateLimits(t *testing.T) {
	fx := setupFleetFixture(t)
	defer fx.ts.Close()

	resp := getJSON(t, fx.ts, "/v1/admin/rate-limits")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rate-limits: expected 200, got %d", resp.StatusCode)
	}
	var out RateLimitStats
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	if !out.Enabled {
		t.Error("enabled: want true with limiter wired")
	}
	if len(out.Rules) != 1 || out.Rules[0].Prefix != "/v1/discovery" || out.Rules[0].RPS != 50 {
		t.Errorf("rules: %+v", out.Rules)
	}
	if out.Default.RPS != 1000 || out.Default.Burst != 2000 {
		t.Errorf("default: %+v", out.Default)
	}

	// Without a limiter the endpoint still answers the disabled shape.
	ts2, _ := setupTestServer(t)
	defer ts2.Close()
	resp2 := getJSON(t, ts2, "/v1/admin/rate-limits")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("rate-limits (no limiter): expected 200, got %d", resp2.StatusCode)
	}
	var out2 RateLimitStats
	json.NewDecoder(resp2.Body).Decode(&out2)
	resp2.Body.Close()
	if out2.Enabled {
		t.Error("enabled: want false without limiter")
	}
}

// TestAdminLoadSeries covers window parsing, scope fallback, and the merged
// players/load points from the sampler rings.
func TestAdminLoadSeries(t *testing.T) {
	fx := setupFleetFixture(t)
	defer fx.ts.Close()

	registerFleetServer(t, fx.ts, "game-1001", "game", nil)
	resp := postJSON(t, fx.ts, "/v1/registry/servers/game-1001/heartbeat", map[string]any{
		"players": 7, "load": 0.1, "status": "online",
	})
	resp.Body.Close()

	// Wait for a tick AFTER the heartbeat: poll until the sampled value
	// matches (the immediate first tick may predate the heartbeat).
	deadline := time.Now().Add(2 * time.Second)
	for {
		pts := fx.sampler.Series("load.fleet.players", time.Hour)
		if len(pts) > 0 && pts[len(pts)-1].V == 7 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sampler never observed players=7: %+v", pts)
		}
		time.Sleep(5 * time.Millisecond)
	}

	resp2 := getJSON(t, fx.ts, "/v1/admin/load-series?window=10h")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("load-series: expected 200, got %d", resp2.StatusCode)
	}
	var out struct {
		Scope  string `json:"scope"`
		Window string `json:"window"`
		Points []struct {
			T       time.Time `json:"t"`
			Players float64   `json:"players"`
			Load    float64   `json:"load"`
		} `json:"points"`
	}
	json.NewDecoder(resp2.Body).Decode(&out)
	resp2.Body.Close()

	if out.Scope != "fleet" || out.Window != "10h" {
		t.Errorf("scope/window: %q/%q", out.Scope, out.Window)
	}
	if len(out.Points) == 0 || out.Points[len(out.Points)-1].Players != 7 {
		t.Errorf("points: %+v, want the heartbeat value in the last sample", out.Points)
	}

	// Server drill-down narrows the scope prefix.
	resp3 := getJSON(t, fx.ts, "/v1/admin/load-series?server_id=game-1001")
	var scoped struct {
		Scope string `json:"scope"`
	}
	json.NewDecoder(resp3.Body).Decode(&scoped)
	resp3.Body.Close()
	if scoped.Scope != "server" {
		t.Errorf("server scope: %q", scoped.Scope)
	}

	// Unknown window is a 400, not a silent default.
	if resp4 := getJSON(t, fx.ts, "/v1/admin/load-series?window=2h"); resp4.StatusCode != http.StatusBadRequest {
		resp4.Body.Close()
		t.Errorf("invalid window: expected 400, got %d", resp4.StatusCode)
	} else {
		resp4.Body.Close()
	}
}

// TestAdminBusSeries verifies the per-topic depth curves and current
// counters after real traffic flows through the counting adapter.
func TestAdminBusSeries(t *testing.T) {
	fx := setupFleetFixture(t)
	defer fx.ts.Close()

	registerFleetServer(t, fx.ts, "game-1001", "game", nil)
	resp := postJSON(t, fx.ts, "/v1/directory/characters", map[string]any{
		"account_id": 7, "server_id": "game-1001", "character_id": 1,
		"name": "hero", "level": 1,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create character: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wait for the sampler to observe the post-create counters (topic
	// names carry the atlas. prefix: bus.atlas.characters.produced).
	deadline := time.Now().Add(2 * time.Second)
	for {
		pts := fx.sampler.Series("bus.atlas.characters.produced", time.Hour)
		if len(pts) > 0 && pts[len(pts)-1].V >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sampler never observed produced≥1: %+v", pts)
		}
		time.Sleep(5 * time.Millisecond)
	}

	resp2 := getJSON(t, fx.ts, "/v1/admin/bus-series?window=1h")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("bus-series: expected 200, got %d", resp2.StatusCode)
	}
	var out struct {
		Adapter string `json:"adapter"`
		Topics  []struct {
			Topic     string `json:"topic"`
			Published int64  `json:"published"`
			Consumed  int64  `json:"consumed"`
			InFlight  int64  `json:"in_flight"`
			Depth     []struct {
				V float64 `json:"v"`
			} `json:"depth"`
		} `json:"topics"`
	}
	json.NewDecoder(resp2.Body).Decode(&out)
	resp2.Body.Close()

	if len(out.Topics) == 0 || out.Topics[0].Topic != "atlas.characters" {
		t.Fatalf("topics: %+v, want the characters topic", out.Topics)
	}
	tp := out.Topics[0]
	if tp.Published < 1 || tp.Consumed < 1 {
		t.Errorf("counters: published=%d consumed=%d, want ≥1 each (sync adapter)", tp.Published, tp.Consumed)
	}
	if tp.InFlight != tp.Published-tp.Consumed {
		t.Errorf("in_flight=%d, want published-consumed=%d", tp.InFlight, tp.Published-tp.Consumed)
	}
	if len(tp.Depth) == 0 {
		t.Error("depth curve: no points sampled")
	}
	if out.Adapter == "" {
		t.Error("adapter name: empty")
	}
}
