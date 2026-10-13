package admin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newTestAdmin(t *testing.T) (*Service, *memory.Store) {
	t.Helper()
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	_ = logger
	return New(mem), mem
}

func registerAdminServer(t *testing.T, ctx context.Context, mem *memory.Store, id string, status model.ServerStatus) {
	t.Helper()
	srv := &model.Server{
		ID: id, Name: id, Type: "game", Region: "cn",
		Version: "1.0", Endpoint: model.Endpoint{Host: "h", Port: 7000 + len(id)},
		Status: status,
	}
	if err := mem.RegisterServer(ctx, srv); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

func TestCreateMaintenanceWindow_AutoAnnounces(t *testing.T) {
	svc, mem := newTestAdmin(t)
	ctx := context.Background()
	registerAdminServer(t, ctx, mem, "srv-1", model.StatusOnline)

	base := time.Now()
	announce := true
	w, err := svc.CreateMaintenanceWindow(ctx, "srv-1", CreateMaintenanceWindowRequest{
		StartAt:  base,
		EndAt:    base.Add(time.Hour),
		Announce: &announce,
	})
	if err != nil {
		t.Fatalf("CreateMaintenanceWindow: %v", err)
	}
	if w.AnnouncementID == nil {
		t.Fatal("expected a linked announcement")
	}
	a, err := mem.GetAnnouncement(ctx, *w.AnnouncementID)
	if err != nil {
		t.Fatalf("linked announcement missing: %v", err)
	}
	if a.Level != model.AnnouncementWarning || a.ServerID == nil || *a.ServerID != "srv-1" {
		t.Errorf("announcement = level %q server %v, want warning/srv-1", a.Level, a.ServerID)
	}
	// The announcement covers the window, so discovery sees it while active.
	active, _ := mem.ListAnnouncements(ctx, store.AnnouncementFilter{ServerID: "srv-1", ActiveOnly: true})
	if len(active) != 1 {
		t.Errorf("active announcements = %d, want 1", len(active))
	}
}

func TestCreateMaintenanceWindow_NoAnnounceAndDefaults(t *testing.T) {
	svc, mem := newTestAdmin(t)
	ctx := context.Background()
	registerAdminServer(t, ctx, mem, "srv-1", model.StatusOnline)

	no := false
	w, err := svc.CreateMaintenanceWindow(ctx, "srv-1", CreateMaintenanceWindowRequest{
		EndAt:    time.Now().Add(time.Hour),
		Announce: &no,
	})
	if err != nil {
		t.Fatalf("CreateMaintenanceWindow: %v", err)
	}
	if w.AnnouncementID != nil {
		t.Error("announce=false must not create an announcement")
	}
	all, _ := mem.ListAnnouncements(ctx, store.AnnouncementFilter{})
	if len(all) != 0 {
		t.Errorf("announcements = %d, want 0", len(all))
	}
	// Omitted start_at means now.
	if w.StartAt.After(time.Now()) {
		t.Errorf("start_at = %v, want ~now", w.StartAt)
	}

	// Unknown server.
	if _, err := svc.CreateMaintenanceWindow(ctx, "ghost", CreateMaintenanceWindowRequest{
		EndAt: time.Now().Add(time.Hour),
	}); !store.IsNotFound(err) {
		t.Errorf("unknown server err = %v, want ErrNotFound", err)
	}

	// end_at <= start_at rejected.
	if _, err := svc.CreateMaintenanceWindow(ctx, "srv-1", CreateMaintenanceWindowRequest{
		StartAt: time.Now().Add(time.Hour),
		EndAt:   time.Now(),
	}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("inverted window err = %v, want ErrInvalid", err)
	}
}

func TestAnnouncementCRUD(t *testing.T) {
	svc, mem := newTestAdmin(t)
	ctx := context.Background()
	registerAdminServer(t, ctx, mem, "srv-1", model.StatusOnline)

	srvID := "srv-1"
	a, err := svc.CreateAnnouncement(ctx, CreateAnnouncementRequest{
		ServerID: &srvID,
		Title:    "Double drops",
		Level:    model.AnnouncementInfo,
		StartsAt: time.Now().Add(-time.Minute),
		EndsAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateAnnouncement: %v", err)
	}
	if a.Level != model.AnnouncementInfo {
		t.Errorf("level = %q, want info", a.Level)
	}

	// Unknown server scoping rejected.
	if _, err := svc.CreateAnnouncement(ctx, CreateAnnouncementRequest{
		ServerID: &[]string{"ghost"}[0],
		Title:    "x",
		StartsAt: time.Now(),
		EndsAt:   time.Now().Add(time.Hour),
	}); !store.IsNotFound(err) {
		t.Errorf("unknown server err = %v, want ErrNotFound", err)
	}

	// Global announcement (nil server).
	if _, err := svc.CreateAnnouncement(ctx, CreateAnnouncementRequest{
		Title:    "Event weekend",
		StartsAt: time.Now().Add(-time.Minute),
		EndsAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("global announcement: %v", err)
	}

	list, _ := svc.ListAnnouncements(ctx, store.AnnouncementFilter{})
	if len(list) != 2 {
		t.Errorf("list = %d, want 2", len(list))
	}

	if err := svc.DeleteAnnouncement(ctx, a.ID); err != nil {
		t.Fatalf("DeleteAnnouncement: %v", err)
	}
	list, _ = svc.ListAnnouncements(ctx, store.AnnouncementFilter{})
	if len(list) != 1 {
		t.Errorf("after delete = %d, want 1", len(list))
	}
}

func TestBatchCreateMaintenanceWindows_StaggeredFleet(t *testing.T) {
	svc, mem := newTestAdmin(t)
	ctx := context.Background()
	ids := []string{"srv-1", "srv-2", "srv-3"}
	for _, id := range ids {
		registerAdminServer(t, ctx, mem, id, model.StatusOnline)
	}

	base := time.Now()
	res, err := svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs:      ids,
		StartAt:        base,
		EndAt:          base.Add(time.Hour),
		StaggerSeconds: 600,
	})
	if err != nil {
		t.Fatalf("BatchCreateMaintenanceWindows: %v", err)
	}
	if len(res.Windows) != 3 || len(res.Failed) != 0 {
		t.Fatalf("created = %d, failed = %v", len(res.Windows), res.Failed)
	}

	seen := make(map[string]bool)
	byServer := make(map[string]*model.MaintenanceWindow)
	for i, w := range res.Windows {
		if w.ID == "" || seen[w.ID] {
			t.Fatalf("index %d: window id %q, want distinct non-empty ids", i, w.ID)
		}
		seen[w.ID] = true
		if w.ServerID != ids[i] {
			t.Errorf("index %d server = %q, want %q", i, w.ServerID, ids[i])
		}
		offset := time.Duration(i) * 10 * time.Minute
		if got, want := w.StartAt, base.Add(offset); !got.Equal(want) {
			t.Errorf("index %d start = %v, want %v", i, got, want)
		}
		// Both ends shift together: the window duration is preserved.
		if got, want := w.EndAt, base.Add(time.Hour).Add(offset); !got.Equal(want) {
			t.Errorf("index %d end = %v, want %v", i, got, want)
		}
		if w.AnnouncementID == nil {
			t.Errorf("index %d: want a linked announcement", i)
			continue
		}
		a, err := mem.GetAnnouncement(ctx, *w.AnnouncementID)
		if err != nil {
			t.Fatalf("index %d linked announcement missing: %v", i, err)
		}
		if a.Level != model.AnnouncementWarning || a.ServerID == nil || *a.ServerID != ids[i] {
			t.Errorf("index %d announcement = level %q server %v, want warning/%s",
				i, a.Level, a.ServerID, ids[i])
		}
		if !a.StartsAt.Equal(w.StartAt) || !a.EndsAt.Equal(w.EndAt) {
			t.Errorf("index %d announcement range = [%v %v], want the window range", i, a.StartsAt, a.EndsAt)
		}
		byServer[ids[i]] = w
	}

	windows, err := mem.ListMaintenanceWindows(ctx, "srv-2", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(windows) != 1 || windows[0].ID != byServer["srv-2"].ID {
		t.Errorf("stored srv-2 windows = %+v, want the staggered one", windows)
	}
	anns, _ := mem.ListAnnouncements(ctx, store.AnnouncementFilter{})
	if len(anns) != 3 {
		t.Errorf("announcements = %d, want 3 (one per server)", len(anns))
	}
}

func TestBatchCreateMaintenanceWindows_PartialFailure(t *testing.T) {
	svc, mem := newTestAdmin(t)
	ctx := context.Background()
	registerAdminServer(t, ctx, mem, "srv-1", model.StatusOnline)
	registerAdminServer(t, ctx, mem, "srv-3", model.StatusOnline)

	no := false
	res, err := svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs: []string{"srv-1", "ghost", "srv-3", "srv-1"},
		EndAt:     time.Now().Add(time.Hour),
		Announce:  &no,
	})
	if err != nil {
		t.Fatalf("BatchCreateMaintenanceWindows: %v", err)
	}
	// A duplicate id must not schedule twice; one bad server must not abort the rest.
	if len(res.Windows) != 2 {
		t.Fatalf("windows = %d, want 2 (srv-1 deduped, ghost skipped)", len(res.Windows))
	}
	if len(res.Failed) != 1 || res.Failed[0].ServerID != "ghost" {
		t.Fatalf("failed = %+v, want only ghost", res.Failed)
	}
	if res.Failed[0].Error == "" {
		t.Error("failure must carry the reason")
	}
	for _, w := range res.Windows {
		if w.AnnouncementID != nil {
			t.Errorf("announce=false must not link an announcement")
		}
	}
	if a, _ := mem.ListAnnouncements(ctx, store.AnnouncementFilter{}); len(a) != 0 {
		t.Errorf("announcements = %d, want 0", len(a))
	}
}

func TestBatchCreateMaintenanceWindows_DryRun(t *testing.T) {
	svc, mem := newTestAdmin(t)
	ctx := context.Background()
	registerAdminServer(t, ctx, mem, "srv-1", model.StatusOnline)

	base := time.Now()
	res, err := svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs:      []string{"srv-1", "ghost"},
		StartAt:        base,
		EndAt:          base.Add(time.Hour),
		StaggerSeconds: 300,
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("BatchCreateMaintenanceWindows: %v", err)
	}
	if len(res.Windows) != 1 || !res.Windows[0].StartAt.Equal(base) {
		t.Fatalf("plan = %+v, want the srv-1 window at base time", res.Windows)
	}
	if len(res.Failed) != 1 || res.Failed[0].ServerID != "ghost" {
		t.Fatalf("failed = %+v, want ghost reported", res.Failed)
	}
	if w, _ := mem.ListMaintenanceWindows(ctx, "", 0); len(w) != 0 {
		t.Errorf("dry run persisted %d windows", len(w))
	}
	if a, _ := mem.ListAnnouncements(ctx, store.AnnouncementFilter{}); len(a) != 0 {
		t.Errorf("dry run created %d announcements", len(a))
	}
}

func TestBatchCreateMaintenanceWindows_Validation(t *testing.T) {
	svc, _ := newTestAdmin(t)
	ctx := context.Background()

	if _, err := svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("empty server_ids err = %v, want ErrInvalid", err)
	}
	if _, err := svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs: []string{"srv-1"},
	}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("missing end_at err = %v, want ErrInvalid", err)
	}
	if _, err := svc.BatchCreateMaintenanceWindows(ctx, BatchMaintenanceWindowRequest{
		ServerIDs: []string{"srv-1"},
		StartAt:   time.Now().Add(time.Hour),
		EndAt:     time.Now(),
	}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("inverted template err = %v, want ErrInvalid", err)
	}
}
