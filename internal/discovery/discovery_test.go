package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
	dto "github.com/prometheus/client_model/go"
)

func newTestService() (*Service, *memory.Store) {
	mem := memory.New()
	return New(mem, mem), mem
}

// seed registers a server directly in the store with the given status and,
// optionally, runtime data.
func seed(t *testing.T, mem *memory.Store, id, region string, status model.ServerStatus, players int) {
	t.Helper()
	srv := &model.Server{
		ID:       id,
		Name:     id,
		Type:     "game",
		Region:   region,
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
		Status:   status,
	}
	if err := mem.RegisterServer(context.Background(), srv); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	hb := model.Heartbeat{Players: players, Load: 0.5, Status: status}
	if err := mem.RecordHeartbeat(context.Background(), id, hb); err != nil {
		t.Fatalf("seed runtime %s: %v", id, err)
	}
}

func idsOf(servers []*model.Server) map[string]bool {
	out := make(map[string]bool, len(servers))
	for _, s := range servers {
		out[s.ID] = true
	}
	return out
}

// TestListServersDefaultVisibility covers the client-facing contract: only
// visible statuses (online / maintenance / suspect) leak to the default list.
func TestListServersDefaultVisibility(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	seed(t, mem, "srv-online", "cn-east", model.StatusOnline, 100)
	seed(t, mem, "srv-maint", "cn-east", model.StatusMaintenance, 50)
	seed(t, mem, "srv-suspect", "cn-east", model.StatusSuspect, 10)
	seed(t, mem, "srv-starting", "cn-east", model.StatusStarting, 0)
	seed(t, mem, "srv-draining", "cn-east", model.StatusDraining, 200)
	seed(t, mem, "srv-offline", "cn-east", model.StatusOffline, 0)
	seed(t, mem, "srv-disabled", "cn-east", model.StatusDisabled, 0)

	got, err := svc.ListServers(ctx, store.ServerFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := idsOf(got)
	want := []string{"srv-online", "srv-maint", "srv-suspect"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", ids, want)
	}
	for _, id := range want {
		if !ids[id] {
			t.Errorf("missing %q in %v", id, ids)
		}
	}
}

// TestListServersExplicitStatus: an explicit status filter bypasses the
// visibility default (operators need to list offline servers too).
func TestListServersExplicitStatus(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	seed(t, mem, "srv-online", "cn-east", model.StatusOnline, 100)
	seed(t, mem, "srv-offline-1", "cn-east", model.StatusOffline, 0)
	seed(t, mem, "srv-offline-2", "us-west", model.StatusOffline, 0)

	got, err := svc.ListServers(ctx, store.ServerFilter{Status: model.StatusOffline})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2 offline", len(got))
	}
}

// TestListServersFilterAndRuntimeMerge: region filter applies and runtime
// data (players/load/last_seen) is merged into the returned servers.
func TestListServersFilterAndRuntimeMerge(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	seed(t, mem, "east-1", "cn-east", model.StatusOnline, 111)
	seed(t, mem, "west-1", "us-west", model.StatusOnline, 222)

	got, err := svc.ListServers(ctx, store.ServerFilter{Region: "cn-east"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "east-1" {
		t.Fatalf("region filter = %v, want [east-1]", idsOf(got))
	}

	srv := got[0]
	if srv.Players != 111 {
		t.Errorf("players = %d, want 111 (runtime merged)", srv.Players)
	}
	if srv.Load != 0.5 {
		t.Errorf("load = %f, want 0.5 (runtime merged)", srv.Load)
	}
	if srv.LastSeenAt == nil || srv.LastSeenAt.IsZero() {
		t.Error("last_seen_at not merged from runtime")
	}
}

// TestListServersWithoutRuntime: a server with no runtime data yet still
// appears in the list, just without player counts.
func TestListServersWithoutRuntime(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	srv := &model.Server{
		ID: "bare-1", Name: "bare", Type: "game", Region: "cn-east",
		Version: "1.0.0", Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Status:   model.StatusOnline,
	}
	if err := mem.RegisterServer(ctx, srv); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := svc.ListServers(ctx, store.ServerFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "bare-1" {
		t.Fatalf("list = %v, want [bare-1]", idsOf(got))
	}
	if got[0].Players != 0 || got[0].LastSeenAt != nil {
		t.Errorf("unexpected runtime data on bare server: %+v", got[0])
	}
}

// TestGetServerMergesRuntime covers the detail path.
func TestGetServerMergesRuntime(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	seed(t, mem, "srv-1", "cn-east", model.StatusOnline, 77)

	srv, err := svc.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if srv.ID != "srv-1" || srv.Players != 77 || srv.Load != 0.5 {
		t.Fatalf("get = %+v, want merged runtime", srv)
	}
}

// TestGetServerWithoutRuntime: details work before the first heartbeat.
func TestGetServerWithoutRuntime(t *testing.T) {
	svc, mem := newTestService()
	ctx := context.Background()

	srv0 := &model.Server{
		ID: "bare-1", Name: "bare", Type: "game", Region: "cn-east",
		Version: "1.0.0", Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Status:   model.StatusOnline,
	}
	if err := mem.RegisterServer(ctx, srv0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv, err := svc.GetServer(ctx, "bare-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if srv.Players != 0 || srv.LastSeenAt != nil {
		t.Errorf("unexpected runtime merge: players=%d last=%v", srv.Players, srv.LastSeenAt)
	}
}

// TestGetServerNotFound: unknown IDs surface store.ErrNotFound (wrapped).
func TestGetServerNotFound(t *testing.T) {
	svc, _ := newTestService()

	if _, err := svc.GetServer(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestWithMetrics(t *testing.T) {
	mem := memory.New()
	svc := New(mem, mem).WithMetrics(metrics.New(mem))

	seed(t, mem, "game-1001", "cn-east", model.StatusOnline, 100)

	got, err := svc.ListServers(context.Background(), store.ServerFilter{})
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 server, got %d", len(got))
	}

	// The instrumentation is wired: one default-filter request counted.
	var out dto.Metric
	// Zero filter renders as the "none" label (internal/metrics).
	if err := svc.metrics.DiscoveryRequests.WithLabelValues("none").Write(&out); err != nil {
		t.Fatalf("counter write: %v", err)
	}
	if n := out.GetCounter().GetValue(); n != 1 {
		t.Errorf("expected discovery counter = 1, got %v", n)
	}
}

func TestTagsPublicOnlyInView(t *testing.T) {
	mem := memory.New()
	svc := New(mem, mem)
	ctx := context.Background()

	srv := &model.Server{ID: "game-1", Name: "一区", Region: "cn-east", Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Tags: []model.ServerTag{
			{Code: model.TagHot, Label: "火热", Tier: model.TierHot, Public: true},
			{Code: "ops_note", Label: "内部", Tier: model.TierNeutral},
		}}
	if err := mem.RegisterServer(ctx, srv); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := svc.GetServer(ctx, "game-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Tags) != 1 || got.Tags[0].Code != model.TagHot {
		t.Fatalf("detail tags = %+v, want public only", got.Tags)
	}

	// The stored record keeps the internal tag: filtering must be a view,
	// not a mutation.
	stored, _ := mem.GetServer(ctx, "game-1")
	if len(stored.Tags) != 2 {
		t.Fatalf("stored tags = %+v, want untouched", stored.Tags)
	}

	list, err := svc.ListServers(ctx, store.ServerFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v (%d)", err, len(list))
	}
	if len(list[0].Tags) != 1 || list[0].Tags[0].Code != model.TagHot {
		t.Fatalf("list tags = %+v, want public only", list[0].Tags)
	}
}
