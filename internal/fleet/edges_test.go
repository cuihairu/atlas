// Error-path and edge coverage for the fleet index: the tracked-store
// delegate and CAS arms (error, not-applied, applied), QueueStats
// forwarding both arms (provider and fallback), reconcile failures
// (listing, runtimes) with the reconciler serving the stale snapshot, the
// upsert/no-op guards, shard/realm count maintenance, metadata keys, and
// the ListAll sort.
package fleet

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// casFaultStore fails the configured verbs; everything else delegates to
// the backing memory store. It embeds the full store interface so it works
// both as the ServerStore of TrackServers/Reconcile and as the composite
// store TrackStore requires.
type casFaultStore struct {
	store.Store
	registerErr error
	casErr      error
	listErr     error
}

func (f *casFaultStore) RegisterServer(ctx context.Context, srv *model.Server) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	return f.Store.RegisterServer(ctx, srv)
}

func (f *casFaultStore) UpdateServerStatusIf(ctx context.Context, id string, from, to model.ServerStatus) (bool, error) {
	if f.casErr != nil {
		return false, f.casErr
	}
	return f.Store.UpdateServerStatusIf(ctx, id, from, to)
}

func (f *casFaultStore) ListServers(ctx context.Context, sf store.ServerFilter) ([]*model.Server, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.Store.ListServers(ctx, sf)
}

// listFaultRuntime fails the fleet-wide runtime listing.
type listFaultRuntime struct {
	store.RuntimeStore
	listErr error
}

func (f *listFaultRuntime) ListRuntimes(ctx context.Context) (map[string]model.Runtime, error) {
	return nil, f.listErr
}

func newMemServer(id string, status model.ServerStatus) *model.Server {
	srv := mkServer(id, "cn-east", string(status), "1.0.0", 100)
	return srv
}

// TestTrackStoreCASArms pins the compare-and-set contract through the
// composite TrackStore: an error propagates untouched, a not-applied CAS
// leaves the index alone (the winning writer owns the projection), and an
// applied CAS is mirrored.
func TestTrackStoreCASArms(t *testing.T) {
	ctx := context.Background()

	mem := memory.New()
	if err := mem.RegisterServer(ctx, newMemServer("cas-srv", model.StatusOnline)); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	fs := &casFaultStore{Store: mem}

	idx := New()
	idx.Upsert(newMemServer("cas-srv", model.StatusOnline))
	ts := TrackStore(fs, idx)

	// Error arm: the store failure propagates, index untouched.
	fs.casErr = errFault
	if _, err := ts.UpdateServerStatusIf(ctx, "cas-srv", model.StatusOnline, model.StatusMaintenance); !errors.Is(err, errFault) {
		t.Fatalf("cas error = %v, want errFault", err)
	}
	fs.casErr = nil

	// Not-applied arm: wrong from-status means another writer won.
	applied, err := ts.UpdateServerStatusIf(ctx, "cas-srv", model.StatusOffline, model.StatusMaintenance)
	if err != nil || applied {
		t.Fatalf("cas from wrong status = %v/%v, want false/nil", applied, err)
	}
	if got, _ := idx.Get("cas-srv"); got.Status != model.StatusOnline {
		t.Fatalf("index projected a not-applied CAS: %s", got.Status)
	}

	// Applied arm: the landed transition is mirrored.
	applied, err = ts.UpdateServerStatusIf(ctx, "cas-srv", model.StatusOnline, model.StatusMaintenance)
	if err != nil || !applied {
		t.Fatalf("cas valid transition = %v/%v, want true/nil", applied, err)
	}
	if got, _ := idx.Get("cas-srv"); got.Status != model.StatusMaintenance {
		t.Fatalf("index did not mirror applied CAS: %s", got.Status)
	}
}

// TestTrackedRegisterError pins the register wrap: a failed register
// returns the store error and never upserts into the index.
func TestTrackedRegisterError(t *testing.T) {
	fs := &casFaultStore{Store: memory.New(), registerErr: errFault}
	idx := New()
	ts := TrackServers(fs, idx)

	if err := ts.RegisterServer(context.Background(), newMemServer("reg-srv", model.StatusOnline)); !errors.Is(err, errFault) {
		t.Fatalf("register error = %v, want errFault", err)
	}
	if _, ok := idx.Get("reg-srv"); ok {
		t.Fatal("index upserted a server whose register failed")
	}
}

// TestQueueStatsForwardsBothArms pins the snapshot passthrough: a store
// implementing QueueStatusProvider is forwarded; any other store gets the
// zero snapshot (the decorator never hides the endpoint either way).
func TestQueueStatsForwardsBothArms(t *testing.T) {
	idx := New()

	mem := memory.New()
	if err := mem.RegisterServer(context.Background(), newMemServer("q-srv", model.StatusOnline)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	provider := (&trackedServers{ServerStore: mem, idx: idx}).QueueStats()
	if !reflect.DeepEqual(provider, mem.QueueStats()) {
		t.Fatalf("provider stats not forwarded: got %+v", provider)
	}

	fallback := (&trackedServers{ServerStore: &flakyServers{}}).QueueStats()
	if !reflect.DeepEqual(fallback, store.QueueStats{}) {
		t.Fatalf("non-provider fallback = %+v, want zero", fallback)
	}
}

// TestReconcileFailuresKeepSnapshot pins the rebuild contract: a listing
// or runtime failure aborts the reconcile with an error, and the index
// keeps serving the previous snapshot.
func TestReconcileFailuresKeepSnapshot(t *testing.T) {
	ctx := context.Background()

	mem := memory.New()
	if err := mem.RegisterServer(ctx, newMemServer("rc-srv", model.StatusOnline)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	idx := New()
	if err := idx.Reconcile(ctx, mem, mem); err != nil {
		t.Fatalf("seed reconcile: %v", err)
	}
	if idx.Len() != 1 {
		t.Fatalf("seeded index len = %d, want 1", idx.Len())
	}

	fs := &casFaultStore{Store: mem, listErr: errFault}
	if err := idx.Reconcile(ctx, fs, nil); !errors.Is(err, errFault) {
		t.Fatalf("reconcile with failing list = %v, want errFault", err)
	}
	if idx.Len() != 1 {
		t.Fatalf("snapshot lost after failed reconcile: len = %d", idx.Len())
	}

	rt := &listFaultRuntime{RuntimeStore: mem, listErr: errFault}
	if err := idx.Reconcile(ctx, mem, rt); !errors.Is(err, errFault) {
		t.Fatalf("reconcile with failing runtimes = %v, want errFault", err)
	}
}

// TestRunReconcilerServesStaleOnError pins the reconciler loop: with a
// store that always fails, the loop keeps ticking (the error branch
// continues) and never exits before the context dies.
func TestRunReconcilerServesStaleOnError(t *testing.T) {
	fs := &casFaultStore{Store: memory.New(), listErr: errFault}
	idx := New()
	idx.Upsert(newMemServer("rr-srv", model.StatusOnline))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunReconciler(ctx, idx, fs, nil, 2*time.Millisecond)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond) // several failing ticks
	cancel()
	<-done

	if idx.Len() != 1 {
		t.Fatalf("stale snapshot lost: len = %d, want 1", idx.Len())
	}
}

// TestUpsertGuards pins the degenerate upserts: nil and empty-ID records
// are dropped without a panic and without touching counts.
func TestUpsertGuards(t *testing.T) {
	idx := New()
	idx.Upsert(nil)
	idx.Upsert(&model.Server{})
	if idx.Len() != 0 {
		t.Fatalf("guards upserted records: len = %d", idx.Len())
	}
}

// TestUpdateNoOps pins the quiet guards: a status change for an unknown
// server, a same-status "change", and a runtime update for an unknown
// server are all no-ops.
func TestUpdateNoOps(t *testing.T) {
	idx := New()
	idx.Upsert(newMemServer("noop-srv", model.StatusOnline))

	idx.UpdateStatus("ghost", model.StatusOffline)
	idx.UpdateStatus("noop-srv", model.StatusOnline) // same status
	idx.UpdateRuntime("ghost", 5, 0.5, time.Now())

	got, ok := idx.Get("noop-srv")
	if !ok || got.Status != model.StatusOnline || got.Players != 0 {
		t.Fatalf("no-op guards changed state: %+v", got)
	}
	st := idx.Stats()
	if st.ServersByStatus[string(model.StatusOffline)] != 0 {
		t.Fatalf("unknown-server status leaked into counts: %+v", st.ServersByStatus)
	}
}

// TestListAllSortsAndFacetCounts pin the read-side contracts: ListAll is
// ID-sorted regardless of insertion order, and the realm/shard/metadata
// facets are maintained on add and remove (re-upsert).
func TestListAllSortsAndFacetCounts(t *testing.T) {
	idx := New()
	idx.Upsert(newMemServer("zz-srv", model.StatusOnline))
	idx.Upsert(newMemServer("aa-srv", model.StatusOnline))
	idx.Upsert(newMemServer("mm-srv", model.StatusOnline))

	all := idx.ListAll()
	if len(all) != 3 || all[0].ID != "aa-srv" || all[2].ID != "zz-srv" {
		t.Fatalf("ListAll not ID-sorted: %v", ids(all))
	}

	// Realm/shard/metadata facets on add...
	realm, shard := "realm-1", "shard-1"
	full := newMemServer("facet-srv", model.StatusOnline)
	full.RealmID, full.ShardID = &realm, &shard
	full.Metadata = map[string]string{"tier": "gold", "zone": "z1"}
	idx.Upsert(full)
	st := idx.Stats()
	if st.ServersByShard["shard-1"] != 1 || st.ServersByRealm["realm-1"] != 1 {
		t.Fatalf("shard/realm facets = %v/%v, want 1/1", st.ServersByShard, st.ServersByRealm)
	}
	if len(st.ServerMetadataKeys) != 2 || st.ServerMetadataKeys[0] != "tier" {
		t.Fatalf("metadata keys = %v, want sorted [tier zone]", st.ServerMetadataKeys)
	}

	// ...and on remove: re-upserting without realm/shard drops the facets.
	idx.Upsert(newMemServer("facet-srv", model.StatusOnline))
	st = idx.Stats()
	if st.ServersByShard["shard-1"] != 0 || st.ServersByRealm["realm-1"] != 0 {
		t.Fatalf("facets survived removal: shard=%v realm=%v", st.ServersByShard, st.ServersByRealm)
	}
}

func ids(servers []*model.Server) []string {
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		out = append(out, s.ID)
	}
	return out
}
