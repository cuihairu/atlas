// Error-path coverage for the admin service: every store verb's failure
// wrap is driven with a fault-wrapped store (per-verb hooks over a real
// memory store), and the natural validation failures (unknown server,
// template-level batch rejection, duplicate shard, invalid announcement,
// empty batch entry) run against the real store.
package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// faultStore wraps a real memory.Store and lets individual verbs fail on
// demand; unset hooks delegate to the backing store.
type faultStore struct {
	store.Store
	base *memory.Store

	updateServerStatusFn     func(ctx context.Context, id string, status model.ServerStatus) error
	deleteServerFn           func(ctx context.Context, id string) error
	getStatsFn               func(ctx context.Context) (*model.Stats, error)
	searchCharactersFn       func(ctx context.Context, f store.CharacterSearchFilter) ([]*model.Character, string, error)
	listRealmsFn             func(ctx context.Context, limit int) ([]*model.Realm, error)
	listShardsFn             func(ctx context.Context, realmID string, limit int) ([]*model.Shard, error)
	listMaintenanceWindowsFn func(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error)
	createAnnouncementFn     func(ctx context.Context, a *model.Announcement) error
	createMaintenanceWinFn   func(ctx context.Context, w *model.MaintenanceWindow) error
	deleteMaintenanceWinFn   func(ctx context.Context, id string) error
	updateServerTagsFn       func(ctx context.Context, id string, tags []model.ServerTag) error
}

func (f *faultStore) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	if f.updateServerStatusFn != nil {
		return f.updateServerStatusFn(ctx, id, status)
	}
	return f.base.UpdateServerStatus(ctx, id, status)
}

func (f *faultStore) DeleteServer(ctx context.Context, id string) error {
	if f.deleteServerFn != nil {
		return f.deleteServerFn(ctx, id)
	}
	return f.base.DeleteServer(ctx, id)
}

func (f *faultStore) GetStats(ctx context.Context) (*model.Stats, error) {
	if f.getStatsFn != nil {
		return f.getStatsFn(ctx)
	}
	return f.base.GetStats(ctx)
}

func (f *faultStore) SearchCharacters(ctx context.Context, f2 store.CharacterSearchFilter) ([]*model.Character, string, error) {
	if f.searchCharactersFn != nil {
		return f.searchCharactersFn(ctx, f2)
	}
	return f.base.SearchCharacters(ctx, f2)
}

func (f *faultStore) ListRealms(ctx context.Context, limit int) ([]*model.Realm, error) {
	if f.listRealmsFn != nil {
		return f.listRealmsFn(ctx, limit)
	}
	return f.base.ListRealms(ctx, limit)
}

func (f *faultStore) ListShards(ctx context.Context, realmID string, limit int) ([]*model.Shard, error) {
	if f.listShardsFn != nil {
		return f.listShardsFn(ctx, realmID, limit)
	}
	return f.base.ListShards(ctx, realmID, limit)
}

func (f *faultStore) ListMaintenanceWindows(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error) {
	if f.listMaintenanceWindowsFn != nil {
		return f.listMaintenanceWindowsFn(ctx, serverID, limit)
	}
	return f.base.ListMaintenanceWindows(ctx, serverID, limit)
}

func (f *faultStore) CreateAnnouncement(ctx context.Context, a *model.Announcement) error {
	if f.createAnnouncementFn != nil {
		return f.createAnnouncementFn(ctx, a)
	}
	return f.base.CreateAnnouncement(ctx, a)
}

func (f *faultStore) CreateMaintenanceWindow(ctx context.Context, w *model.MaintenanceWindow) error {
	if f.createMaintenanceWinFn != nil {
		return f.createMaintenanceWinFn(ctx, w)
	}
	return f.base.CreateMaintenanceWindow(ctx, w)
}

func (f *faultStore) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	if f.deleteMaintenanceWinFn != nil {
		return f.deleteMaintenanceWinFn(ctx, id)
	}
	return f.base.DeleteMaintenanceWindow(ctx, id)
}

func (f *faultStore) UpdateServerTags(ctx context.Context, id string, tags []model.ServerTag) error {
	if f.updateServerTagsFn != nil {
		return f.updateServerTagsFn(ctx, id, tags)
	}
	return f.base.UpdateServerTags(ctx, id, tags)
}

func newFaultAdmin() (*Service, *faultStore) {
	base := memory.New()
	fs := &faultStore{Store: base, base: base}
	return New(fs), fs
}

// TestSetDrainAndEnableNotFound pin the lookup wraps: an unknown server is
// an error, never a silent no-op transition.
func TestSetDrainAndEnableNotFound(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	if err := svc.SetDrain(ctx, "ghost"); err == nil {
		t.Fatal("set drain on unknown server: expected error")
	}
	if err := svc.Enable(ctx, "ghost"); err == nil {
		t.Fatal("enable on unknown server: expected error")
	}
}

// TestLifecycleWriteFaults pins the write wraps of disable/delete: a store
// that fails mid-operation surfaces the wrap (a half-disabled fleet state
// must be visible, not swallowed).
func TestLifecycleWriteFaults(t *testing.T) {
	svc, fs := newFaultAdmin()
	ctx := context.Background()
	registerServer(t, fs.base, "dis-srv", model.StatusOnline)
	registerServer(t, fs.base, "del-srv", model.StatusOnline)

	fs.updateServerStatusFn = func(context.Context, string, model.ServerStatus) error { return errFault }
	if err := svc.Disable(ctx, "dis-srv"); err == nil {
		t.Fatal("disable with failing status write: expected error")
	}
	fs.updateServerStatusFn = nil

	fs.deleteServerFn = func(context.Context, string) error { return errFault }
	if err := svc.DeleteServer(ctx, "del-srv"); err == nil {
		t.Fatal("delete server with failing store: expected error")
	}
}

// TestQueryFaults pins every read wrap: stats, character search, realm and
// shard listings, and window listings all surface store failures.
func TestQueryFaults(t *testing.T) {
	svc, fs := newFaultAdmin()
	ctx := context.Background()

	fs.getStatsFn = func(context.Context) (*model.Stats, error) { return nil, errFault }
	if _, err := svc.GetStats(ctx); err == nil {
		t.Fatal("get stats with failing store: expected error")
	}
	fs.searchCharactersFn = func(context.Context, store.CharacterSearchFilter) ([]*model.Character, string, error) {
		return nil, "", errFault
	}
	if _, _, err := svc.SearchCharacters(ctx, store.CharacterSearchFilter{}); err == nil {
		t.Fatal("search characters with failing store: expected error")
	}
	fs.listRealmsFn = func(context.Context, int) ([]*model.Realm, error) { return nil, errFault }
	if _, err := svc.ListRealms(ctx, 0); err == nil {
		t.Fatal("list realms with failing store: expected error")
	}
	fs.listShardsFn = func(context.Context, string, int) ([]*model.Shard, error) { return nil, errFault }
	if _, err := svc.ListShards(ctx, "", 0); err == nil {
		t.Fatal("list shards with failing store: expected error")
	}
	fs.listMaintenanceWindowsFn = func(context.Context, string, int) ([]*model.MaintenanceWindow, error) {
		return nil, errFault
	}
	if _, err := svc.ListMaintenanceWindows(ctx, "", 0); err == nil {
		t.Fatal("list windows with failing store: expected error")
	}
}

// TestCreateWindowsTemplateInvalid pins the template rejection wrap: a
// template-level mistake (missing end_at) fails the whole CreateWindows
// call before any per-server work.
func TestCreateWindowsTemplateInvalid(t *testing.T) {
	svc, _ := setupService(t)
	if err := svc.CreateWindows(context.Background(), []string{"game-1"}, time.Now(), time.Time{}); err == nil {
		t.Fatal("create windows with zero end_at: expected error")
	}
}

// TestCreateShardDuplicate pins the shard insert wrap: re-creating an
// existing shard surfaces the store's conflict.
func TestCreateShardDuplicate(t *testing.T) {
	svc, mem := setupService(t)
	ctx := context.Background()
	if err := mem.CreateRealm(ctx, &model.Realm{ID: "r-1", Name: "Realm", Region: "cn-east"}); err != nil {
		t.Fatalf("seed realm: %v", err)
	}
	req := CreateShardRequest{ID: "sh-1", RealmID: "r-1", Name: "Shard"}
	if _, err := svc.CreateShard(ctx, req); err != nil {
		t.Fatalf("create shard: %v", err)
	}
	if _, err := svc.CreateShard(ctx, req); err == nil {
		t.Fatal("duplicate shard create: expected error")
	}
}

// TestWindowWriteFaults pins the single-window wraps: an announcement that
// cannot be created and a window that cannot be created both fail the
// CreateMaintenanceWindow call.
func TestWindowWriteFaults(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	win := CreateMaintenanceWindowRequest{StartAt: now, EndAt: now.Add(time.Hour)}

	svc, fs := newFaultAdmin()
	registerServer(t, fs.base, "mw-srv", model.StatusOnline)
	fs.createAnnouncementFn = func(context.Context, *model.Announcement) error { return errFault }
	if _, err := svc.CreateMaintenanceWindow(ctx, "mw-srv", win); err == nil {
		t.Fatal("window with failing announcement write: expected error")
	}

	svc2, fs2 := newFaultAdmin()
	registerServer(t, fs2.base, "mw-srv2", model.StatusOnline)
	no := false
	fs2.createMaintenanceWinFn = func(context.Context, *model.MaintenanceWindow) error { return errFault }
	if _, err := svc2.CreateMaintenanceWindow(ctx, "mw-srv2", CreateMaintenanceWindowRequest{
		StartAt: now, EndAt: now.Add(time.Hour), Announce: &no,
	}); err == nil {
		t.Fatal("window with failing window write: expected error")
	}
}

// TestBatchPerServerFaults pins the batch loop's per-server failure slices:
// an empty id, an announcement write failure, and a window write failure
// each land in Failed without aborting the batch.
func TestBatchPerServerFaults(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	svc, fs := newFaultAdmin()
	registerServer(t, fs.base, "batch-srv", model.StatusOnline)

	res, err := svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs: []string{"", "batch-srv"}, StartAt: now, EndAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("batch with empty id: %v", err)
	}
	if len(res.Failed) != 1 || res.Failed[0].ServerID != "" {
		t.Fatalf("failed = %+v, want one empty-id entry", res.Failed)
	}
	if len(res.Windows) != 1 {
		t.Fatalf("windows = %d, want the valid one scheduled", len(res.Windows))
	}

	fs.createAnnouncementFn = func(context.Context, *model.Announcement) error { return errFault }
	res, err = svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs: []string{"batch-srv"}, StartAt: now, EndAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("batch with failing announcement: %v", err)
	}
	if len(res.Failed) != 1 || len(res.Windows) != 0 {
		t.Fatalf("announcement failure: failed=%d windows=%d, want 1/0", len(res.Failed), len(res.Windows))
	}
	fs.createAnnouncementFn = nil

	fs.createMaintenanceWinFn = func(context.Context, *model.MaintenanceWindow) error { return errFault }
	no := false
	res, err = svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs: []string{"batch-srv"}, StartAt: now, EndAt: now.Add(time.Hour), Announce: &no,
	})
	if err != nil {
		t.Fatalf("batch with failing window write: %v", err)
	}
	if len(res.Failed) != 1 || len(res.Windows) != 0 {
		t.Fatalf("window failure: failed=%d windows=%d, want 1/0", len(res.Failed), len(res.Windows))
	}
}

// TestDeleteMaintenanceWindowFault pins the delete passthrough: the service
// adds no handling of its own, so a store failure surfaces unchanged (the
// memory store is idempotent on unknown ids — the fault is the only way a
// delete can fail there).
func TestDeleteMaintenanceWindowFault(t *testing.T) {
	svc, fs := newFaultAdmin()
	fs.deleteMaintenanceWinFn = func(context.Context, string) error { return errFault }
	if err := svc.DeleteMaintenanceWindow(context.Background(), "any"); err == nil {
		t.Fatal("delete window with failing store: expected error")
	}
}

// TestCreateAnnouncementValidation pins the DTO validation wrap (no title)
// and the store write wrap of the same method.
func TestCreateAnnouncementValidation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	svc, _ := setupService(t)
	if _, err := svc.CreateAnnouncement(ctx, CreateAnnouncementRequest{
		Title: "", Body: "body", StartsAt: now, EndsAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("announcement without title: expected error")
	}

	svc2, fs := newFaultAdmin()
	fs.createAnnouncementFn = func(context.Context, *model.Announcement) error { return errFault }
	if _, err := svc2.CreateAnnouncement(ctx, CreateAnnouncementRequest{
		Title: "Title", Body: "body", StartsAt: now, EndsAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("announcement with failing store write: expected error")
	}
}

// TestTagWriteFaults pins the tag write wraps: add and remove both merge
// against a validated tag set first, so a failing store write surfaces.
func TestTagWriteFaults(t *testing.T) {
	svc, fs := newFaultAdmin()
	ctx := context.Background()
	registerServer(t, fs.base, "tag-srv", model.StatusOnline)

	fs.updateServerTagsFn = func(context.Context, string, []model.ServerTag) error { return errFault }
	if _, err := svc.AddServerTag(ctx, "tag-srv", AddServerTagRequest{Code: "hot"}); err == nil {
		t.Fatal("add tag with failing write: expected error")
	}

	// Remove needs the tag present to reach its write; seed it on the base
	// store, keep the fault, and remove.
	if err := fs.base.UpdateServerTags(ctx, "tag-srv", []model.ServerTag{{Code: "hot", Label: "火热"}}); err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	if err := svc.RemoveServerTag(ctx, "tag-srv", "hot"); err == nil {
		t.Fatal("remove tag with failing write: expected error")
	}
}
