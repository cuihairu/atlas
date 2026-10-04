package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// failingPingStore answers Ping with a configured error so the readiness
// probe's degraded path can be driven without a real outage.
type failingPingStore struct {
	store.Store
	pingErr error
}

func (f *failingPingStore) Ping(ctx context.Context) error { return f.pingErr }

// TestReadyzReportsStorageUnavailable pins the probe contract: storage
// failure reads 503 STORAGE_UNAVAILABLE, health stays separate from it.
func TestReadyzReportsStorageUnavailable(t *testing.T) {
	h := &Handler{store: &failingPingStore{pingErr: errors.New("connection refused")}, logger: nil}
	rec := httptest.NewRecorder()
	h.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with failed store = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "STORAGE_UNAVAILABLE") {
		t.Fatalf("readyz body = %s, want STORAGE_UNAVAILABLE", rec.Body.String())
	}
}

// TestAdminGetCrossServerConfigBaseline pins the admin read: the dashboard
// baseline is the version-0 empty snapshot when nothing was published,
// the stored document afterwards, and 503 when the service is not wired.
func TestAdminGetCrossServerConfigBaseline(t *testing.T) {
	ts, mem := setupTestServer(t)

	// Unpublished: version-0 empty snapshot, not a 404 (unlike the strict
	// startup pull).
	resp, err := http.Get(ts.URL + "/v1/admin/crossserver/config")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || cfg.Version != 0 {
		t.Fatalf("unpublished admin GET = %d version %d, want 200 version 0", resp.StatusCode, cfg.Version)
	}

	// After a publish the admin read surfaces the stored document.
	put, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/admin/crossserver/config",
		strings.NewReader(`{"topology":{"clusters":[]},"groups":[],"features":{},"match_domains":[],"cross_play_types":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	put.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d, want 200", resp.StatusCode)
	}
	_ = mem

	resp, err = http.Get(ts.URL + "/v1/admin/crossserver/config")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if cfg.Version != 1 {
		t.Fatalf("published admin GET version = %d, want 1", cfg.Version)
	}

	// Service not wired: 503, discoverable by the dashboard.
	logger := newQuietLogger(t)
	unwired := New(nil, nil, nil, admin.New(mem), nil, nil, mem, nil, logger)
	rec := httptest.NewRecorder()
	unwired.handleAdminGetCrossServerConfig(rec, httptest.NewRequest(http.MethodGet, "/v1/admin/crossserver/config", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired admin GET = %d, want 503", rec.Code)
	}
}

// TestRegisterRegistryRoutesMount pins the registry-port mount: registry
// verbs and the cross-server pull live there, discovery does not.
func TestRegisterRegistryRoutesMount(t *testing.T) {
	mem := memory.New()
	logger := newQuietLogger(t)
	events := httpadapter.New()
	_ = events.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error { return nil })

	regSvc := registry.New(mem, mem, logger)
	handler := New(regSvc, discovery.New(mem, mem), directory.New(mem), admin.New(mem),
		routing.New(mem, mem, mem, mem), crossserver.New(mem, mem, events, logger), mem, events, logger)
	mux := http.NewServeMux()
	handler.RegisterRegistryRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := `{"server_id":"reg-1","name":"reg-1","type":"game","region":"cn-east",
		"version":"1.0.0","platform":"pc","endpoint":{"host":"10.0.0.1","port":30001},"capacity":100}`
	resp, err := http.Post(ts.URL+"/v1/registry/servers/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register on registry port = %d, want 201", resp.StatusCode)
	}

	// The strict pull is mounted here too; unpublished is 404 CONFIG_NOT_FOUND.
	resp, err = http.Get(ts.URL + "/v1/crossserver/config")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(raw), "CONFIG_NOT_FOUND") {
		t.Fatalf("unpublished strict pull = %d %s, want 404 CONFIG_NOT_FOUND", resp.StatusCode, string(raw))
	}
}

func newQuietLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
