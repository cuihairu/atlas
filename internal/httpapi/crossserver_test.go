package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

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

// setupCrossServer wires the config center against an in-memory store and
// a recording bus so tests can observe both the HTTP surface and the
// change signal.
func setupCrossServer(t *testing.T) (*httptest.Server, *memory.Store, *signalRecorder) {
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

	rec := &signalRecorder{adapter: events}
	crossSvc := crossserver.New(mem, mem, events, logger)
	handler := New(regSvc, discSvc, dirSvc, admSvc, routing.New(mem, mem, mem), crossSvc, mem, events, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return httptest.NewServer(mux), mem, rec
}

// signalRecorder subscribes to the config topic and records the signals
// Atlas publishes, standing in for a game server's bus subscription.
type signalRecorder struct {
	adapter *httpadapter.Adapter

	mu       sync.Mutex
	signals  []*event.Event
	notified chan struct{}
}

func (r *signalRecorder) watch(t *testing.T) {
	t.Helper()
	r.notified = make(chan struct{}, 8)
	err := r.adapter.Subscribe(context.Background(), event.TopicConfig, func(_ context.Context, e *event.Event) error {
		r.mu.Lock()
		r.signals = append(r.signals, e)
		r.mu.Unlock()
		select {
		case r.notified <- struct{}{}:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe config topic: %v", err)
	}
}

func (r *signalRecorder) wait(t *testing.T) *event.Event {
	t.Helper()
	select {
	case <-r.notified:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for config.updated signal")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.signals[len(r.signals)-1]
}

func (r *signalRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.signals)
}

func specBody() map[string]any {
	return map[string]any{
		"topology": map[string]any{
			"clusters": []map[string]any{
				{"id": "cluster-a", "name": "华东", "region": "cn-east", "servers": []string{"game-1001", "game-1002"}},
			},
		},
		"groups":        []map[string]any{{"id": "g-1", "name": "第一期", "servers": []string{"game-1001"}}},
		"features":      map[string]bool{"world-boss": true, "arena": false},
		"match_domains": []map[string]any{{"id": "md-1", "servers": []string{"game-1001", "game-1002"}, "params": map[string]string{"mmr_range": "500"}}},
		"crossplay_types": []map[string]any{
			{"id": "battlefield", "name": "跨服战场/竞技", "summary": "跨服 PVP 匹配对局",
				"lifecycle": "seasonal", "matchmaking": true, "ranking": true, "id_prefix": "xb"},
		},
	}
}

// TestCrossServerConfigPublishAndPull walks the contract a game server
// depends on: pull before publish fails fast (404), admin publish bumps
// the version, the pull returns the document, and a signal carrying only
// version+hash (never the body) is broadcast.
func TestCrossServerConfigPublishAndPull(t *testing.T) {
	ts, _, rec := setupCrossServer(t)
	defer ts.Close()
	rec.watch(t)

	// Nothing published yet: a booting server must fail fast, not receive
	// an empty document it could mistake for "nothing to do".
	resp := getJSON(t, ts, "/v1/crossserver/config")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pull before publish = %d, want 404", resp.StatusCode)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, resp, &errBody)
	if errBody.Error.Code != "CONFIG_NOT_FOUND" {
		t.Errorf("error code = %q, want CONFIG_NOT_FOUND", errBody.Error.Code)
	}

	// Publish (admin).
	resp = putJSON(t, ts, "/v1/admin/crossserver/config", specBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	var published struct {
		Config struct {
			Version int             `json:"version"`
			Hash    string          `json:"hash"`
			Spec    json.RawMessage `json:"spec"`
		} `json:"config"`
		Notify struct {
			Bus        string   `json:"bus"`
			Idempotent bool     `json:"idempotent"`
			Targets    []string `json:"targets"`
			Callbacks  struct {
				Targets int `json:"targets"`
			} `json:"callbacks"`
		} `json:"notify"`
	}
	decode(t, resp, &published)
	if published.Config.Version != 1 {
		t.Errorf("first publish version = %d, want 1", published.Config.Version)
	}
	if published.Config.Hash == "" {
		t.Error("publish did not stamp a hash")
	}
	if published.Notify.Bus != "http" {
		t.Errorf("notify bus = %q, want http", published.Notify.Bus)
	}
	if len(published.Notify.Targets) == 0 {
		t.Error("publish reported no change targets")
	}

	// The signal carries the version and hash only — the receiver pulls.
	sig := rec.wait(t)
	if sig.Type != event.EventConfigUpdated {
		t.Errorf("signal type = %q, want config.updated", sig.Type)
	}
	if sig.ConfigVersion != 1 || sig.ConfigHash != published.Config.Hash {
		t.Errorf("signal = v%d/%q, want v1/%q", sig.ConfigVersion, sig.ConfigHash, published.Config.Hash)
	}
	if sig.Metadata != nil && len(*sig.Metadata) != 0 {
		t.Error("signal must not carry the config body")
	}

	// Pull now succeeds and returns the document.
	resp = getJSON(t, ts, "/v1/crossserver/config")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pull after publish = %d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Error("pull did not set an ETag validator")
	}
	var pulled struct {
		Version int `json:"version"`
		Hash    string
		Spec    struct {
			Topology struct {
				Clusters []struct {
					ID string `json:"id"`
				} `json:"clusters"`
			} `json:"topology"`
			Features map[string]bool `json:"features"`
		} `json:"spec"`
	}
	decode(t, resp, &pulled)
	if pulled.Version != 1 || pulled.Hash != published.Config.Hash {
		t.Errorf("pulled = v%d/%q, want v1/%q", pulled.Version, pulled.Hash, published.Config.Hash)
	}
	if len(pulled.Spec.Topology.Clusters) != 1 || pulled.Spec.Topology.Clusters[0].ID != "cluster-a" {
		t.Errorf("pulled topology = %+v", pulled.Spec.Topology.Clusters)
	}
	if !pulled.Spec.Features["world-boss"] {
		t.Errorf("pulled features = %+v", pulled.Spec.Features)
	}
}

// TestCrossServerConfigConditionalGet pins the poll-mode cheap check from
// both sides: a current validator (If-None-Match, or ?version=N&hash=H)
// answers 304 with no body, while a stale one answers 200 with the fresh
// document. A conditional pull also never 404s — a running server that has
// no config yet keeps polling instead of failing to start.
func TestCrossServerConfigConditionalGet(t *testing.T) {
	ts, _, _ := setupCrossServer(t)
	defer ts.Close()

	conditional := func(t *testing.T, path, etag string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("conditional get: %v", err)
		}
		return resp
	}

	// Before any publish, a conditional pull is not a startup failure: it
	// answers 200 with the version-0 empty snapshot so a polling server
	// learns "nothing configured yet" instead of seeing a 404 it would
	// have to special-case on every tick.
	resp := conditional(t, "/v1/crossserver/config?version=0", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("conditional pull before publish = %d, want 200", resp.StatusCode)
	}
	var empty struct {
		Version int `json:"version"`
	}
	decode(t, resp, &empty)
	if empty.Version != 0 {
		t.Errorf("unpublished config version = %d, want 0", empty.Version)
	}

	resp = putJSON(t, ts, "/v1/admin/crossserver/config", specBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Stale validator → full document (the client is behind).
	resp = conditional(t, "/v1/crossserver/config", `"v0-stale"`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stale validator = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Current validator → 304.
	resp = getJSON(t, ts, "/v1/crossserver/config")
	etag := resp.Header.Get("ETag")
	resp.Body.Close()
	if etag == "" {
		t.Fatal("pull did not set an ETag validator")
	}
	resp = conditional(t, "/v1/crossserver/config", etag)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("matching validator = %d, want 304", resp.StatusCode)
	}
	resp.Body.Close()

	// A re-encoding proxy weakens the validator (Cloudflare turns "x" into
	// W/"x"). Weak comparison still recognises it — otherwise every polling
	// server behind such a hop re-downloads the whole document each tick.
	resp = conditional(t, "/v1/crossserver/config", "W/"+etag)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("weak matching validator = %d, want 304", resp.StatusCode)
	}
	resp.Body.Close()

	// A list of validators (some unrelated) still matches on the hit.
	resp = conditional(t, "/v1/crossserver/config", `"other", W/`+etag)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("validator list = %d, want 304", resp.StatusCode)
	}
	resp.Body.Close()

	// "*" matches any current representation.
	resp = conditional(t, "/v1/crossserver/config", "*")
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf(`If-None-Match: * = %d, want 304`, resp.StatusCode)
	}
	resp.Body.Close()

	// The query form is equivalent: current version → 304, old → 200.
	resp = conditional(t, "/v1/crossserver/config?version=1", "")
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("version=1 (current) = %d, want 304", resp.StatusCode)
	}
	resp.Body.Close()
	resp = conditional(t, "/v1/crossserver/config?version=0", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("version=0 (stale) = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// A malformed version is rejected rather than silently ignored.
	resp = conditional(t, "/v1/crossserver/config?version=abc", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("version=abc = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestCrossServerConfigIdempotentPublish proves same-content publishes do
// not move the version and do not signal: receivers must not be woken for
// a change that is not one.
func TestCrossServerConfigIdempotentPublish(t *testing.T) {
	ts, _, rec := setupCrossServer(t)
	defer ts.Close()
	rec.watch(t)

	if resp := putJSON(t, ts, "/v1/admin/crossserver/config", specBody()); resp.StatusCode != http.StatusOK {
		t.Fatalf("first publish = %d", resp.StatusCode)
	}
	rec.wait(t)

	resp := putJSON(t, ts, "/v1/admin/crossserver/config", specBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second publish = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	var out struct {
		Config struct {
			Version int `json:"version"`
		} `json:"config"`
		Notify struct {
			Idempotent bool `json:"idempotent"`
		} `json:"notify"`
	}
	decode(t, resp, &out)
	if !out.Notify.Idempotent {
		t.Error("identical publish was not reported idempotent")
	}
	if out.Config.Version != 1 {
		t.Errorf("version moved on identical publish: %d, want 1", out.Config.Version)
	}
	if n := rec.count(); n != 1 {
		t.Errorf("signals = %d, want 1 (no wakeup for an unchanged config)", n)
	}
}

// TestCrossServerConfigInvalidSpecRejected checks the publish boundary:
// malformed documents never reach storage, so a bad admin edit cannot
// break every running game server's next pull.
func TestCrossServerConfigInvalidSpecRejected(t *testing.T) {
	ts, _, _ := setupCrossServer(t)
	defer ts.Close()

	cases := []struct {
		name string
		body map[string]any
	}{
		{"duplicate cluster id", map[string]any{"topology": map[string]any{"clusters": []map[string]any{
			{"id": "c1", "servers": []string{}}, {"id": "c1", "servers": []string{}},
		}}}},
		{"cluster without id", map[string]any{"topology": map[string]any{"clusters": []map[string]any{
			{"name": "no id", "servers": []string{}},
		}}}},
		{"bad cluster status", map[string]any{"topology": map[string]any{"clusters": []map[string]any{
			{"id": "c1", "status": "paused", "servers": []string{}},
		}}}},
		{"duplicate server in cluster", map[string]any{"topology": map[string]any{"clusters": []map[string]any{
			{"id": "c1", "servers": []string{"a", "a"}},
		}}}},
		{"empty server id", map[string]any{"topology": map[string]any{"clusters": []map[string]any{
			{"id": "c1", "servers": []string{""}},
		}}}},
		{"illegal feature key", map[string]any{"features": map[string]bool{"World Boss": true}}},
		{"duplicate group id", map[string]any{"groups": []map[string]any{
			{"id": "g", "servers": []string{}}, {"id": "g", "servers": []string{}},
		}}},
		{"duplicate match domain id", map[string]any{"match_domains": []map[string]any{
			{"id": "m", "servers": []string{}}, {"id": "m", "servers": []string{}},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := putJSON(t, ts, "/v1/admin/crossserver/config", tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("publish = %d, want 400: %s", resp.StatusCode, readBody(t, resp))
			}
		})
	}

	// Nothing was stored, so a pull still reports the unpublished state.
	if resp := getJSON(t, ts, "/v1/crossserver/config"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("pull after rejected publishes = %d, want 404", resp.StatusCode)
	}
}

// TestRegisterDeclaresNotifyModeAndReportsConfigVersion ties the two ends
// of the config-center contract together: a server declares how it wants
// to be notified at registration, and learns the config version in force
// from the same response so it can skip a pointless pull.
func TestRegisterDeclaresNotifyModeAndReportsConfigVersion(t *testing.T) {
	ts, mem, _ := setupCrossServer(t)
	defer ts.Close()

	// Before anything is published: registration succeeds and reports
	// version 0 (nothing configured yet).
	resp := postJSON(t, ts, "/v1/registry/servers/register", map[string]any{
		"server_id": "game-1", "name": "G1", "type": "game", "region": "cn-east",
		"version": "1.0.0", "platform": "any",
		"endpoint":    map[string]any{"host": "10.0.0.1", "port": 30001},
		"capacity":    1000,
		"notify_mode": "callback", "notify_callback_url": "http://127.0.0.1:9999/notify",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	var reg struct {
		CrossServerConfig *struct {
			Version int `json:"version"`
		} `json:"crossserver_config"`
	}
	decode(t, resp, &reg)
	if reg.CrossServerConfig == nil || reg.CrossServerConfig.Version != 0 {
		t.Errorf("register config version = %+v, want version 0", reg.CrossServerConfig)
	}

	srv, err := mem.GetServer(context.Background(), "game-1")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if srv.NotifyMode != "callback" || srv.NotifyCallbackURL != "http://127.0.0.1:9999/notify" {
		t.Errorf("notify declaration not persisted: %+v", srv)
	}

	// After a publish, a re-registering server learns the new version.
	if resp := putJSON(t, ts, "/v1/admin/crossserver/config", specBody()); resp.StatusCode != http.StatusOK {
		t.Fatal("publish failed")
	}
	resp = postJSON(t, ts, "/v1/registry/servers/register", map[string]any{
		"server_id": "game-2", "name": "G2", "type": "game", "region": "cn-east",
		"version": "1.0.0", "platform": "any",
		"endpoint":    map[string]any{"host": "10.0.0.2", "port": 30002},
		"capacity":    1000,
		"notify_mode": "subscribe",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-register = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	decode(t, resp, &reg)
	if reg.CrossServerConfig == nil || reg.CrossServerConfig.Version != 1 {
		t.Errorf("register config version = %+v, want version 1", reg.CrossServerConfig)
	}
}

// TestRegisterRejectsBadNotifyDeclaration keeps a broken notify
// declaration from reaching storage: callback mode without a URL, a
// relative URL, an unknown mode, or a URL without a mode.
func TestRegisterRejectsBadNotifyDeclaration(t *testing.T) {
	ts, _, _ := setupCrossServer(t)
	defer ts.Close()

	cases := []struct {
		name string
		body map[string]any
	}{
		{"callback without url", map[string]any{"notify_mode": "callback"}},
		{"callback with relative url", map[string]any{"notify_mode": "callback", "notify_callback_url": "/notify"}},
		{"callback with non-http scheme", map[string]any{"notify_mode": "callback", "notify_callback_url": "ftp://host/notify"}},
		{"unknown mode", map[string]any{"notify_mode": "telepathy"}},
		{"unknown mode inside a mode list", map[string]any{"notify_mode": "callback,telepathy"}},
		{"callback inside a mode list without url", map[string]any{"notify_mode": "subscribe,callback"}},
		{"url without callback mode", map[string]any{"notify_callback_url": "http://127.0.0.1:9999/notify"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{
				"server_id": "game-" + tc.name, "name": "G", "type": "game", "region": "cn-east",
				"version": "1.0.0", "platform": "any",
				"endpoint": map[string]any{"host": "10.0.0.1", "port": 30001},
				"capacity": 1000,
			}
			for k, v := range tc.body {
				body[k] = v
			}
			resp := postJSON(t, ts, "/v1/registry/servers/register", body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("register = %d, want 400: %s", resp.StatusCode, readBody(t, resp))
			}
		})
	}
}

// TestRegisterCoexistingModesAndSwitch pins 可并存可切换: both push
// channels may be declared at once ("subscribe,callback" + callback URL),
// and a plain re-registration replaces the declaration — switching to
// poll clears the old callback URL instead of leaving it behind.
func TestRegisterCoexistingModesAndSwitch(t *testing.T) {
	ts, mem, _ := setupCrossServer(t)
	defer ts.Close()

	register := func(mode, url string) int {
		t.Helper()
		body := map[string]any{
			"server_id": "game-coexist", "name": "GC", "type": "game", "region": "cn-east",
			"version": "1.0.0", "platform": "any",
			"endpoint": map[string]any{"host": "10.0.0.9", "port": 30009},
			"capacity": 100,
		}
		if mode != "" {
			body["notify_mode"] = mode
		}
		if url != "" {
			body["notify_callback_url"] = url
		}
		resp := postJSON(t, ts, "/v1/registry/servers/register", body)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := register("subscribe,callback", "http://127.0.0.1:9999/notify"); code != http.StatusCreated {
		t.Fatalf("coexisting declaration = %d, want 201", code)
	}
	srv, err := mem.GetServer(context.Background(), "game-coexist")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if srv.NotifyMode != "subscribe,callback" {
		t.Errorf("notify_mode = %q, want subscribe,callback", srv.NotifyMode)
	}
	if !srv.HasNotifyMode("subscribe") || !srv.HasNotifyMode("callback") {
		t.Errorf("HasNotifyMode misses a declared mode: %q", srv.NotifyMode)
	}

	if code := register("poll", ""); code != http.StatusCreated {
		t.Fatalf("re-register as poll = %d, want 201", code)
	}
	srv, err = mem.GetServer(context.Background(), "game-coexist")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if srv.NotifyMode != "poll" || srv.NotifyCallbackURL != "" {
		t.Errorf("switch did not replace declaration: mode=%q url=%q", srv.NotifyMode, srv.NotifyCallbackURL)
	}
}

// TestCrossServerConfigPullOnPublicPort guards the split-mount promise of
// docs/config-center.md §4.2/§5: the strict pull is served on the public
// listener too (deployments without a registry gateway pull it there),
// not only on the registry listener. The all-in-one RegisterRoutes mux
// would hide a missing public mount — this test registers only the
// public/admin route sets the way main.go wires its listeners.
func TestCrossServerConfigPullOnPublicPort(t *testing.T) {
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	events := httpadapter.New()
	crossSvc := crossserver.New(mem, mem, events, logger)
	handler := New(
		registry.New(mem, mem, logger), discovery.New(mem, mem), directory.New(mem),
		admin.New(mem), routing.New(mem, mem, mem), crossSvc, mem, events, logger,
	)

	mux := http.NewServeMux()
	handler.RegisterPublicRoutes(mux)
	handler.RegisterAdminRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Unpublished: JSON CONFIG_NOT_FOUND — a plain mux "404 page not
	// found" here would mean the route is not mounted on the listener.
	resp := getJSON(t, ts, "/v1/crossserver/config")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("public pull before publish = %d, want 404", resp.StatusCode)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, resp, &errBody)
	if errBody.Error.Code != "CONFIG_NOT_FOUND" {
		t.Errorf("error code = %q, want CONFIG_NOT_FOUND", errBody.Error.Code)
	}

	// Publish via admin, then pull on the public listener: the fifth
	// section (crossplay_types) must round-trip through both listeners.
	resp = putJSON(t, ts, "/v1/admin/crossserver/config", specBody())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()

	resp = getJSON(t, ts, "/v1/crossserver/config")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public pull = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	var cfg struct {
		Version int `json:"version"`
		Spec    struct {
			CrossPlayTypes []struct {
				ID       string `json:"id"`
				IDPrefix string `json:"id_prefix"`
			} `json:"crossplay_types"`
		} `json:"spec"`
	}
	decode(t, resp, &cfg)
	if cfg.Version != 1 {
		t.Errorf("version = %d, want 1", cfg.Version)
	}
	if len(cfg.Spec.CrossPlayTypes) != 1 ||
		cfg.Spec.CrossPlayTypes[0].ID != "battlefield" ||
		cfg.Spec.CrossPlayTypes[0].IDPrefix != "xb" {
		t.Errorf("crossplay_types roundtrip = %+v", cfg.Spec.CrossPlayTypes)
	}
}

// putJSON issues the full-document publish (PUT is the admin verb: the
// config is replaced, never patched).
func putJSON(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, ts.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
