package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// errRuntimes fails the batched runtime read so the sweep's fail-closed
// path can be driven without a real outage.
type errRuntimes struct {
	store.Store
	err error
}

func (e *errRuntimes) GetRuntimes(ctx context.Context, ids []string) (map[string]model.Runtime, error) {
	return nil, e.err
}

// TestSweepRuntimeStoreFailureFailsClosed pins the sweep contract: a
// failed runtime read fails the whole sweep and flips nothing. The old
// per-key loop treated every read error as "never heartbeated", which
// would have marked live starting servers offline during a store outage.
func TestSweepRuntimeStoreFailureFailsClosed(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
	recordHeartbeat(t, ctx, mem, "srv-1", 100)

	heartbeatTime := time.Now()
	mon := newTestMonitor(&errRuntimes{Store: mem, err: errors.New("redis down")}, 30*time.Second, 60*time.Second)
	mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }

	if err := mon.sweep(ctx); err == nil {
		t.Fatal("sweep with failed runtime store = nil error, want fail closed")
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Status != model.StatusOnline {
		t.Fatalf("status after failed sweep = %s, want untouched online", srv.Status)
	}
}

// TestSweepStartingWithoutHeartbeatGoesOffline pins the absent-key
// semantics: a starting server with no runtime snapshot at all counts
// as "never heartbeated" and goes offline once it outlives the offline
// threshold (batched reads must keep this branch).
func TestSweepStartingWithoutHeartbeatGoesOffline(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	registerServer(t, ctx, mem, "srv-1", model.StatusStarting)

	created := time.Now()
	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
	mon.now = func() time.Time { return created.Add(65 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	srv, err := mem.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Status != model.StatusOffline {
		t.Fatalf("stale starting server = %s, want offline (never heartbeated)", srv.Status)
	}
}
