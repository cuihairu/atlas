package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

func TestMaintenanceWindowLifecycle(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now()

	w := &model.MaintenanceWindow{
		ID:       "mwin-1",
		ServerID: "srv-1",
		StartAt:  now.Add(-time.Minute),
		EndAt:    now.Add(time.Hour),
	}
	if err := s.CreateMaintenanceWindow(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Duplicate ID conflicts.
	if err := s.CreateMaintenanceWindow(ctx, w); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate create = %v, want ErrConflict", err)
	}

	got, err := s.GetMaintenanceWindow(ctx, "mwin-1")
	if err != nil || got.ServerID != "srv-1" {
		t.Fatalf("get: %v %+v", err, got)
	}

	if err := s.MarkMaintenanceWindowApplied(ctx, "mwin-1", model.StatusOnline); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	got, _ = s.GetMaintenanceWindow(ctx, "mwin-1")
	if got.PreviousStatus != model.StatusOnline {
		t.Errorf("previous status = %q, want online", got.PreviousStatus)
	}
	if err := s.MarkMaintenanceWindowApplied(ctx, "missing", model.StatusOnline); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("mark missing = %v, want ErrNotFound", err)
	}

	// Server-scoped listing.
	other := &model.MaintenanceWindow{ID: "mwin-2", ServerID: "srv-2", StartAt: now, EndAt: now.Add(2 * time.Hour)}
	if err := s.CreateMaintenanceWindow(ctx, other); err != nil {
		t.Fatalf("create other: %v", err)
	}
	only1, _ := s.ListMaintenanceWindows(ctx, "srv-1", 0)
	if len(only1) != 1 || only1[0].ID != "mwin-1" {
		t.Errorf("server filter = %+v", only1)
	}
	all, _ := s.ListMaintenanceWindows(ctx, "", 0)
	if len(all) != 2 {
		t.Errorf("unfiltered list = %d, want 2", len(all))
	}

	if err := s.DeleteMaintenanceWindow(ctx, "mwin-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetMaintenanceWindow(ctx, "mwin-1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
}

func TestAnnouncementFiltering(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now()

	srv := "srv-1"
	anns := []*model.Announcement{
		{ID: "a-global", Title: "global", ServerID: nil, Level: "info", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)},
		{ID: "a-srv1", Title: "srv1", ServerID: &srv, Level: "warning", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)},
		{ID: "a-past", Title: "past", ServerID: nil, Level: "info", StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(-time.Hour)},
		{ID: "a-future", Title: "future", ServerID: &srv, Level: "info", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour)},
	}
	for _, a := range anns {
		if err := s.CreateAnnouncement(ctx, a); err != nil {
			t.Fatalf("create %s: %v", a.ID, err)
		}
		if err := s.CreateAnnouncement(ctx, a); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate %s = %v, want ErrConflict", a.ID, err)
		}
	}

	// Discovery view: global + srv-1, active only.
	got, err := s.ListAnnouncements(ctx, store.AnnouncementFilter{ServerID: srv, ActiveOnly: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a-srv1" || got[1].ID != "a-global" {
		t.Errorf("discovery filter got %+v, want [a-srv1 a-global] (newest first)", ids(got))
	}

	// Admin view: everything.
	all, _ := s.ListAnnouncements(ctx, store.AnnouncementFilter{})
	if len(all) != 4 {
		t.Errorf("admin list = %d, want 4", len(all))
	}

	// Empty ServerID + active = global + every server.
	everythingActive, _ := s.ListAnnouncements(ctx, store.AnnouncementFilter{ActiveOnly: true})
	if len(everythingActive) != 2 {
		t.Errorf("active list = %+v, want the 2 active announcements", ids(everythingActive))
	}

	if err := s.DeleteAnnouncement(ctx, "a-past"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	all, _ = s.ListAnnouncements(ctx, store.AnnouncementFilter{})
	if len(all) != 3 {
		t.Errorf("after delete = %d, want 3", len(all))
	}
}

func TestCreatedAtStamp(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now()

	w := &model.MaintenanceWindow{
		ID:       "mwin-createdat",
		ServerID: "srv-1",
		StartAt:  now.Add(-time.Minute),
		EndAt:    now.Add(time.Hour),
	}
	if err := s.CreateMaintenanceWindow(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.CreatedAt.IsZero() {
		t.Errorf("caller object CreatedAt is zero after CreateMaintenanceWindow")
	}
	got, _ := s.GetMaintenanceWindow(ctx, "mwin-createdat")
	if got.CreatedAt.IsZero() {
		t.Errorf("stored object CreatedAt is zero")
	}

	// Same for announcements.
	a := &model.Announcement{
		ID:        "ann-createdat",
		ServerID:  nil,
		Title:     "test",
		Level:     "info",
		StartsAt:  now.Add(-time.Hour),
		EndsAt:    now.Add(time.Hour),
	}
	if err := s.CreateAnnouncement(ctx, a); err != nil {
		t.Fatalf("create announcement: %v", err)
	}
	if a.CreatedAt.IsZero() {
		t.Errorf("caller object CreatedAt is zero after CreateAnnouncement")
	}
	gotA, _ := s.GetAnnouncement(ctx, "ann-createdat")
	if gotA.CreatedAt.IsZero() {
		t.Errorf("stored announcement CreatedAt is zero")
	}
}

func ids(anns []*model.Announcement) []string {
	out := make([]string, len(anns))
	for i, a := range anns {
		out[i] = a.ID
	}
	return out
}
