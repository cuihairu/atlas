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
