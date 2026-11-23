package atlas

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	atlasgrpc "github.com/cuihairu/atlas/internal/grpc"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// newRESTClient stands up a mock Atlas and returns a client against it.
func newRESTClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{Addr: srv.URL, RegistryToken: "reg-token", AdminAPIKey: "adm-key", BaseBackoff: time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c, srv
}

// TestRESTLifecycle exercises register → heartbeat → list → error mapping
// over HTTP, asserting paths, auth headers and query building.
func TestRESTLifecycle(t *testing.T) {
	var gotAuth, gotPath, gotQuery atomic.Value
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/registry/servers/register", func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"server_id": "game-1", "status": "online"}) //nolint:errcheck
	})
	mux.HandleFunc("GET /v1/discovery/servers", func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		gotQuery.Store(r.URL.RawQuery)
		json.NewEncoder(w).Encode(map[string]any{"servers": []Server{{ID: "game-1", Status: "online"}}}) //nolint:errcheck
	})
	// Synchronous REST creates return the flat character object (201).
	mux.HandleFunc("POST /v1/directory/characters", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Character{AccountID: 7, ServerID: "game-1", CharacterID: 1001, Name: "Hero", Level: 1}) //nolint:errcheck
	})
	mux.HandleFunc("GET /v1/discovery/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
			"code": "SERVER_NOT_FOUND", "message": "server nope does not exist",
		}}) //nolint:errcheck
	})
	c, _ := newRESTClient(t, mux)
	ctx := context.Background()

	res, err := c.Register(ctx, RegisterRequest{
		ServerID: "game-1", Name: "Test", Capacity: 100,
		Metadata:          map[string]string{"cluster": "ea"},
		NotifyMode:        "callback",
		NotifyCallbackURL: "https://cfg.example.com/hook",
		RegKind:           RegTemporary,
		TTLSeconds:        3600,
	})
	if err != nil || res.ServerID != "game-1" {
		t.Fatalf("Register: res=%+v err=%v", res, err)
	}
	if gotAuth.Load() != "Bearer reg-token" {
		t.Errorf("expected registry bearer token, got %q", gotAuth.Load())
	}
	// The register body must carry the register-parity fields (reg_kind /
	// ttl_seconds / metadata / notify declaration), mirroring the REST
	// register contract — a dropped field here is the silent-drop defect
	// family the SDK audits guard against.
	for _, k := range []string{"reg_kind", "ttl_seconds", "metadata", "notify_mode", "notify_callback_url"} {
		if _, ok := gotBody[k]; !ok {
			t.Errorf("register body missing %q (body=%v)", k, gotBody)
		}
	}
	if gotBody["reg_kind"] != "temporary" || gotBody["ttl_seconds"] != float64(3600) {
		t.Errorf("register body reg_kind/ttl_seconds = %v/%v, want temporary/3600", gotBody["reg_kind"], gotBody["ttl_seconds"])
	}

	servers, err := c.ListServers(ctx, ServerFilter{Region: "cn-east", Status: "online"})
	if err != nil || len(servers) != 1 {
		t.Fatalf("ListServers: %v, %d servers", err, len(servers))
	}
	if gotPath.Load() != "/v1/discovery/servers" || gotQuery.Load() != "region=cn-east&status=online" {
		t.Errorf("unexpected request: path=%q query=%q", gotPath.Load(), gotQuery.Load())
	}

	_, err = c.GetServer(ctx, "nope")
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Code != "SERVER_NOT_FOUND" || apiErr.StatusCode != 404 {
		t.Fatalf("expected *Error SERVER_NOT_FOUND/404, got %#v", err)
	}

	// Flat synchronous reply parses into Character + derived status.
	wr, err := c.CreateCharacter(ctx, CreateCharacterRequest{AccountID: 7, ServerID: "game-1", CharacterID: 1001})
	if err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	if wr.Status != "created" || wr.Character == nil || wr.Character.CharacterID != 1001 {
		t.Fatalf("unexpected write result: %+v", wr)
	}
}

// TestCharacterPresenceFields pins the presence read path surfacing: the REST
// character responses carry presence_status / last_logout_at
// (docs/presence.md), and the SDK Character DTO must decode them rather than
// silently drop them.
func TestCharacterPresenceFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/directory/characters/{id}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"account_id": 7, "server_id": "game-1", "character_id": 1001,
			"name": "Hero", "level": 1,
			"presence_status": "online",
			"last_logout_at":  "2026-11-22T08:00:00Z",
			"created_at":      "2026-11-01T00:00:00Z",
			"updated_at":      "2026-11-22T08:01:00Z",
		})
	})
	c, _ := newRESTClient(t, mux)
	ctx := context.Background()

	ch, err := c.GetCharacter(ctx, 1001)
	if err != nil {
		t.Fatalf("GetCharacter: %v", err)
	}
	if ch.PresenceStatus != "online" {
		t.Errorf("PresenceStatus = %q, want online (presence read path must surface)", ch.PresenceStatus)
	}
	if ch.LastLogoutAt == nil || ch.LastLogoutAt.Format(time.RFC3339) != "2026-11-22T08:00:00Z" {
		t.Errorf("LastLogoutAt = %v, want 2026-11-22T08:00:00Z", ch.LastLogoutAt)
	}
}

// TestRetryOnTransient verifies 5xx responses are retried with backoff
// while 4xx are not.
func TestRetryOnTransient(t *testing.T) {
	var attempts atomic.Int32
	var notFoundHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/discovery/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "flaky" {
			if attempts.Add(1) < 3 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(Server{ID: "flaky", Status: "online"}) //nolint:errcheck
			return
		}
		notFoundHits.Add(1)
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "SERVER_NOT_FOUND", "message": "x"}}) //nolint:errcheck
	})
	c, _ := newRESTClient(t, mux)
	ctx := context.Background()

	srv, err := c.GetServer(ctx, "flaky")
	if err != nil || srv.ID != "flaky" {
		t.Fatalf("GetServer flaky: srv=%+v err=%v", srv, err)
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts (2 failures + success), got %d", attempts.Load())
	}

	if _, err := c.GetServer(ctx, "missing"); err == nil {
		t.Fatal("expected error for missing server")
	}
	if notFoundHits.Load() != 1 {
		t.Errorf("404 must not be retried, got %d hits", notFoundHits.Load())
	}
}

// TestAutoHeartbeat checks the loop fires on interval, picks up payload
// changes and stops cleanly.
func TestAutoHeartbeat(t *testing.T) {
	var beats atomic.Int32
	var lastPlayers atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/registry/servers/{id}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var req HeartbeatRequest
		json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
		lastPlayers.Store(int32(req.Players))
		beats.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"server_id": r.PathValue("id"), "status": "online"}) //nolint:errcheck
	})
	c, _ := newRESTClient(t, mux)

	loop := c.StartHeartbeat("game-1", 40*time.Millisecond, HeartbeatRequest{Players: 5})
	loop.Set(42, 0.5) // picked up by the first or second beat
	deadline := time.Now().Add(2 * time.Second)
	for beats.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	loop.Stop()
	loop.Stop() // idempotent

	if got := beats.Load(); got < 2 {
		t.Fatalf("expected ≥2 heartbeats, got %d", got)
	}
	if lastPlayers.Load() != 42 {
		t.Errorf("expected payload update to reach the server, players=%d", lastPlayers.Load())
	}
	if err := loop.LastError(); err != nil {
		t.Errorf("unexpected heartbeat error: %v", err)
	}
}

// newGRPCClient starts the real Atlas gRPC server on a random TCP port.
func newGRPCClient(t *testing.T) (*Client, *memory.Store) {
	t.Helper()
	mem := memory.New()
	adapter := httpadapter.New()
	dirSvc := directory.New(mem)
	if err := adapter.Subscribe(context.Background(), event.TopicCharacters, func(ctx context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(ctx, e)
		return err
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := atlasgrpc.New(
		registry.New(mem, mem, logger),
		discovery.New(mem, mem),
		dirSvc,
		routing.New(mem, mem, mem, mem),
		admin.New(mem),
		adapter,
	)
	g := grpc.NewServer()
	srv.RegisterServices(g)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go g.Serve(lis) //nolint:errcheck // test server
	t.Cleanup(g.Stop)

	c, err := New(Options{Addr: lis.Addr().String(), Transport: TransportGRPC, BaseBackoff: time.Millisecond})
	if err != nil {
		t.Fatalf("New grpc: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c, mem
}

// TestGRPLLifecycle runs the same core flow over gRPC against the real
// server implementation — proving both transports behave alike.
func TestGRPLLifecycle(t *testing.T) {
	c, _ := newGRPCClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.Register(ctx, RegisterRequest{
		ServerID: "game-9", Name: "G", Region: "cn-east", Capacity: 100,
		Endpoint: Endpoint{Host: "10.0.0.9", Port: 30009},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	hb, err := c.Heartbeat(ctx, "game-9", HeartbeatRequest{Players: 7, Load: 0.3})
	if err != nil || hb.Status != "online" {
		t.Fatalf("Heartbeat: %+v err=%v", hb, err)
	}

	servers, err := c.ListServers(ctx, ServerFilter{})
	if err != nil || len(servers) != 1 || servers[0].ID != "game-9" {
		t.Fatalf("ListServers: %v, %+v", err, servers)
	}

	wr, err := c.CreateCharacter(ctx, CreateCharacterRequest{
		AccountID: 1, ServerID: "game-9", CharacterID: 1001, Name: "Hero", Level: 1,
	})
	if err != nil || wr.Character == nil || wr.Status != "created" {
		t.Fatalf("CreateCharacter: %+v err=%v", wr, err)
	}
	if _, err := c.UpdateCharacter(ctx, 1001, UpdateCharacterRequest{Level: intPtr(10)}); err != nil {
		t.Fatalf("UpdateCharacter: %v", err)
	}
	rec, err := c.Recommend(ctx, 0, "", "", "")
	if err != nil || rec.Server.ID != "game-9" || rec.Reason == "" {
		t.Fatalf("Recommend: %+v err=%v", rec, err)
	}
	stats, err := c.Stats(ctx)
	if err != nil || stats.TotalServers != 1 {
		t.Fatalf("Stats: %+v err=%v", stats, err)
	}

	_, err = c.GetCharacter(ctx, 404404)
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Code != "NotFound" {
		t.Errorf("expected *Error code NotFound, got %#v", err)
	}
}

// TestGRPCRegisterMetaFields pins the register-parity fields over gRPC end
// to end: the proto RegisterRequest carries the notify declaration, metadata
// and the registration lifecycle class (api/proto/atlas.proto
// RegisterRequest), so a gRPC-transport game server can declare them. The
// SDK used to reject the notify declaration on the gRPC transport outright
// ("requires the REST transport") and silently dropped metadata / reg_kind /
// ttl_seconds — both fail loudly or silently instead of registering with the
// declaration the caller asked for.
func TestGRPCRegisterMetaFields(t *testing.T) {
	c, mem := newGRPCClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.Register(ctx, RegisterRequest{
		ServerID: "game-meta", Name: "G", Region: "cn-east", Capacity: 100,
		Endpoint:          Endpoint{Host: "10.0.0.9", Port: 30009},
		Metadata:          map[string]string{"cluster": "ea"},
		NotifyMode:        NotifyModeCallback,
		NotifyCallbackURL: "https://cfg.example.com/hook",
		RegKind:           RegTemporary,
		TTLSeconds:        3600,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv, err := mem.GetServer(ctx, "game-meta")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.NotifyMode != NotifyModeCallback || srv.NotifyCallbackURL != "https://cfg.example.com/hook" {
		t.Errorf("notify = %q/%q, want %q/https://cfg.example.com/hook",
			srv.NotifyMode, srv.NotifyCallbackURL, NotifyModeCallback)
	}
	if srv.RegKind != RegTemporary || srv.RegTTLSeconds != 3600 {
		t.Errorf("lifecycle = %q/%d, want %q/3600", srv.RegKind, srv.RegTTLSeconds, RegTemporary)
	}
	if got := srv.Metadata["cluster"]; got != "ea" {
		t.Errorf("metadata[cluster] = %q, want ea", got)
	}
}

func intPtr(v int) *int { return &v }
