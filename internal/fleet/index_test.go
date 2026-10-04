package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func mkServer(id, region, status, version string, capacity int) *model.Server {
	return &model.Server{
		ID: id, Name: id, Type: "game", Region: region,
		Version: version, Platform: "pc", Capacity: capacity,
		Status: model.ServerStatus(status),
	}
}

func TestIndex_UpsertAndCounts(t *testing.T) {
	idx := New()
	idx.Upsert(mkServer("game-1", "cn-east", "online", "1.0.0", 100))
	idx.Upsert(mkServer("game-2", "cn-east", "offline", "1.0.0", 200))
	idx.Upsert(mkServer("game-3", "us", "online", "1.1.0", 300))

	st := idx.Stats()
	if st.TotalServers != 3 {
		t.Fatalf("total = %d, want 3", st.TotalServers)
	}
	if st.OnlineServers != 2 {
		t.Fatalf("online = %d, want 2 (BUGS ①: derived from by-status)", st.OnlineServers)
	}
	if st.ServersByRegion["cn-east"] != 2 || st.ServersByRegion["us"] != 1 {
		t.Fatalf("by region = %v", st.ServersByRegion)
	}
	if st.ServersByVersion["1.0.0"] != 2 || st.ServersByVersion["1.1.0"] != 1 {
		t.Fatalf("by version = %v", st.ServersByVersion)
	}
	if st.TotalCapacity != 600 {
		t.Fatalf("capacity = %d, want 600", st.TotalCapacity)
	}

	// Upsert with changed fields moves the counts, not duplicates them.
	upd := mkServer("game-3", "eu", "maintenance", "1.1.0", 300)
	idx.Upsert(upd)
	st = idx.Stats()
	if st.ServersByRegion["us"] != 0 {
		t.Fatalf("region us should be gone, got %v", st.ServersByRegion)
	}
	if st.ServersByRegion["eu"] != 1 || st.ServersByStatus["maintenance"] != 1 || st.ServersByStatus["online"] != 1 {
		t.Fatalf("post-update counts wrong: %v %v", st.ServersByRegion, st.ServersByStatus)
	}
	if st.TotalServers != 3 {
		t.Fatalf("total after re-upsert = %d, want 3", st.TotalServers)
	}
}

func TestIndex_UpdateStatusAndRuntime(t *testing.T) {
	idx := New()
	idx.Upsert(mkServer("game-1", "cn", "starting", "1.0.0", 100))
	idx.UpdateStatus("game-1", model.StatusOnline)
	idx.UpdateRuntime("game-1", 42, 0.5, time.Now())

	st := idx.Stats()
	if st.ServersByStatus["online"] != 1 || st.OnlineServers != 1 {
		t.Fatalf("status update not reflected: %v", st.ServersByStatus)
	}
	if st.TotalPlayers != 42 {
		t.Fatalf("players = %d, want 42", st.TotalPlayers)
	}

	// Runtime fields survive a base-record upsert (register refresh).
	idx.Upsert(mkServer("game-1", "cn", "online", "1.0.0", 100))
	srv, ok := idx.Get("game-1")
	if !ok || srv.Players != 42 || srv.Load != 0.5 || srv.LastSeenAt == nil {
		t.Fatalf("runtime fields wiped by upsert: %+v", srv)
	}
	if st := idx.Stats(); st.TotalPlayers != 42 {
		t.Fatalf("players sum changed by upsert: %d", st.TotalPlayers)
	}

	// Unregister-style cleanup zeroes runtime data.
	idx.UpdateRuntime("game-1", 0, 0, time.Time{})
	srv, _ = idx.Get("game-1")
	if srv.Players != 0 || srv.LastSeenAt != nil {
		t.Fatalf("runtime not cleared: %+v", srv)
	}
}

func TestIndex_ListFiltersAndCursor(t *testing.T) {
	idx := New()
	realm := "realm-cn"
	for i, r := range []string{"cn", "cn", "us"} {
		s := mkServer("game-100"+string(rune('0'+i)), r, "online", "1.0.0", 100)
		if r == "cn" {
			s.RealmID = &realm
			s.Tags = []model.ServerTag{{Code: "hot", Label: "火热", Tier: "hot", Public: true}}
			s.Metadata = map[string]string{"cluster": "c1"}
		}
		idx.Upsert(s)
	}
	// id substring search (item 9): "game-100" hits all, tighter hits one.
	if got, _ := idx.List(ListFilter{ID: "game-1001"}); len(got) != 1 || got[0].ID != "game-1001" {
		t.Fatalf("id search: %+v", got)
	}
	// tag filter
	if got, _ := idx.List(ListFilter{Tag: "hot"}); len(got) != 2 {
		t.Fatalf("tag filter: %d", len(got))
	}
	// metadata key/value filter
	if got, _ := idx.List(ListFilter{MetadataKey: "cluster", MetadataValue: "c1"}); len(got) != 2 {
		t.Fatalf("metadata filter: %d", len(got))
	}
	if got, _ := idx.List(ListFilter{MetadataKey: "cluster", MetadataValue: "c9"}); len(got) != 0 {
		t.Fatalf("metadata mismatch should filter out: %d", len(got))
	}
	// realm + status composability
	if got, _ := idx.List(ListFilter{Realm: realm, Status: model.StatusOnline}); len(got) != 2 {
		t.Fatalf("realm+status: %d", len(got))
	}
	// cursor pagination
	page, cur := idx.List(ListFilter{Limit: 2})
	if len(page) != 2 || cur == "" {
		t.Fatalf("page1: %d cur=%q", len(page), cur)
	}
	page2, cur2 := idx.List(ListFilter{Limit: 2, Cursor: cur})
	if len(page2) != 1 || cur2 != "" {
		t.Fatalf("page2: %d cur=%q", len(page2), cur2)
	}
}

func TestTrackDecoratorsFeedIndex(t *testing.T) {
	mem := memory.New()
	idx := New()
	servers := TrackServers(mem, idx)
	runtime := TrackRuntime(mem, idx)

	ctx := context.Background()
	srv := mkServer("game-1", "cn", "starting", "1.0.0", 100)
	if err := servers.RegisterServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordHeartbeat(ctx, "game-1", model.Heartbeat{Players: 7, Load: 0.1}); err != nil {
		t.Fatal(err)
	}
	if err := servers.UpdateServerStatus(ctx, "game-1", model.StatusOnline); err != nil {
		t.Fatal(err)
	}

	st := idx.Stats()
	if st.TotalServers != 1 || st.OnlineServers != 1 || st.TotalPlayers != 7 {
		t.Fatalf("decorator feed wrong: %+v", st)
	}

	// Tags flow through too.
	if err := servers.UpdateServerTags(ctx, "game-1", []model.ServerTag{{Code: "new"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := idx.List(ListFilter{Tag: "new"}); len(got) != 1 {
		t.Fatalf("tag feed: %d", len(got))
	}
}

func TestIndex_ReconcileRebuildsFromStores(t *testing.T) {
	mem := memory.New()
	// Write straight into the inner store, bypassing the decorators —
	// the reconciler must still pick it up (cross-replica writes).
	inner := mkServer("game-9", "eu", "online", "2.0", 50)
	if err := mem.RegisterServer(context.Background(), inner); err != nil {
		t.Fatal(err)
	}
	if err := mem.RecordHeartbeat(context.Background(), "game-9", model.Heartbeat{Players: 5}); err != nil {
		t.Fatal(err)
	}

	idx := New()
	if err := idx.Reconcile(context.Background(), mem, mem); err != nil {
		t.Fatal(err)
	}
	st := idx.Stats()
	if st.TotalServers != 1 || st.TotalPlayers != 5 || st.ServersByRegion["eu"] != 1 {
		t.Fatalf("reconcile snapshot: %+v", st)
	}
	// Server gone from the store disappears from the index.
	if err := mem.UpdateServerStatus(context.Background(), "game-9", model.StatusOffline); err != nil {
		t.Fatal(err)
	}
	if err := idx.Reconcile(context.Background(), mem, mem); err != nil {
		t.Fatal(err)
	}
	if st := idx.Stats(); st.ServersByStatus["online"] != 0 || st.ServersByStatus["offline"] != 1 {
		t.Fatalf("post-reconcile statuses: %v", st.ServersByStatus)
	}
}

// TestTrackStoreCompositeAndRemovals drives the composite TrackStore
// wrapper (main.go's single wiring point) through every tracked write —
// register / status / tags / heartbeat / deletes — then exercises the
// index accessors and the background reconciler loop.
func TestTrackStoreCompositeAndRemovals(t *testing.T) {
	mem := memory.New()
	idx := New()
	tracked := TrackStore(mem, idx)

	ctx := context.Background()
	if err := tracked.RegisterServer(ctx, mkServer("game-1", "cn", "starting", "1.0.0", 100)); err != nil {
		t.Fatal(err)
	}
	if err := tracked.UpdateServerStatus(ctx, "game-1", model.StatusOnline); err != nil {
		t.Fatal(err)
	}
	if err := tracked.UpdateServerTags(ctx, "game-1", []model.ServerTag{{Code: "new"}}); err != nil {
		t.Fatal(err)
	}
	if err := tracked.RecordHeartbeat(ctx, "game-1", model.Heartbeat{Players: 3}); err != nil {
		t.Fatal(err)
	}

	if idx.Len() != 1 {
		t.Fatalf("len = %d, want 1", idx.Len())
	}
	if got := idx.ListAll(); len(got) != 1 || got[0].ID != "game-1" || got[0].Players != 3 {
		t.Fatalf("list all: %+v", got)
	}
	if _, ok := idx.Get("game-1"); !ok {
		t.Fatal("get: want hit for tracked id")
	}
	if _, ok := idx.Get("game-404"); ok {
		t.Fatal("get: want miss for unknown id")
	}

	// Runtime deletion zeroes the live gauges; server deletion drops the
	// record and its facet counts entirely.
	if err := tracked.DeleteRuntime(ctx, "game-1"); err != nil {
		t.Fatal(err)
	}
	if st := idx.Stats(); st.TotalPlayers != 0 {
		t.Fatalf("players after runtime delete: %d", st.TotalPlayers)
	}
	if err := tracked.DeleteServer(ctx, "game-1"); err != nil {
		t.Fatal(err)
	}
	if idx.Len() != 0 || idx.Stats().TotalServers != 0 {
		t.Fatalf("after delete: len=%d stats=%+v", idx.Len(), idx.Stats())
	}
	idx.Remove("game-1") // idempotent on unknown ids

	// RunReconciler rebuilds from the inner store until cancelled
	// (cross-replica writes land here, decorators never saw them).
	if err := mem.RegisterServer(ctx, mkServer("game-2", "eu", "online", "2.0", 10)); err != nil {
		t.Fatal(err)
	}
	rc, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		RunReconciler(rc, idx, mem, mem, 5*time.Millisecond)
		close(done)
	}()
	for idx.Len() == 0 {
		select {
		case <-time.After(2 * time.Second):
			t.Fatal("reconciler never picked up the inner write")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reconciler did not stop on cancel")
	}
}
