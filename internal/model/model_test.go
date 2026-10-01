package model

import (
	"errors"
	"testing"
	"time"
)

func TestMaintenanceWindowValidate(t *testing.T) {
	now := time.Now()
	valid := &MaintenanceWindow{ID: "mwin-1", ServerID: "srv-1", StartAt: now, EndAt: now.Add(time.Hour)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid window rejected: %v", err)
	}

	for _, tc := range []struct {
		name string
		w    MaintenanceWindow
	}{
		{"no id", MaintenanceWindow{ServerID: "srv", StartAt: now, EndAt: now.Add(time.Hour)}},
		{"no server", MaintenanceWindow{ID: "m", StartAt: now, EndAt: now.Add(time.Hour)}},
		{"end before start", MaintenanceWindow{ID: "m", ServerID: "srv", StartAt: now, EndAt: now}},
		{"end equals start", MaintenanceWindow{ID: "m", ServerID: "srv", StartAt: now.Add(time.Hour), EndAt: now}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.w.Validate(); !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestMaintenanceWindowActive(t *testing.T) {
	now := time.Now()
	w := &MaintenanceWindow{StartAt: now, EndAt: now.Add(time.Hour)}

	if !w.Active(now.Add(30 * time.Minute)) {
		t.Error("mid-window should be active")
	}
	if w.Active(now.Add(-time.Second)) {
		t.Error("before start should not be active")
	}
	if w.Active(now.Add(time.Hour)) {
		t.Error("at end should not be active (half-open interval)")
	}
}

func TestAnnouncementValidate(t *testing.T) {
	now := time.Now()
	valid := &Announcement{ID: "ann-1", Title: "hi", Level: "", StartsAt: now, EndsAt: now.Add(time.Hour)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid announcement rejected: %v", err)
	}

	if !ValidAnnouncementLevel(AnnouncementInfo) || !ValidAnnouncementLevel(AnnouncementWarning) ||
		!ValidAnnouncementLevel(AnnouncementCritical) || !ValidAnnouncementLevel("") {
		t.Error("known levels rejected")
	}
	if ValidAnnouncementLevel("loud") {
		t.Error("unknown level accepted")
	}

	bad := []Announcement{
		{Title: "no id", StartsAt: now, EndsAt: now.Add(time.Hour)},
		{ID: "a", StartsAt: now, EndsAt: now.Add(time.Hour)},
		{ID: "a", Title: "t", Level: "loud", StartsAt: now, EndsAt: now.Add(time.Hour)},
		{ID: "a", Title: "t", StartsAt: now.Add(time.Hour), EndsAt: now},
	}
	for i := range bad {
		if err := bad[i].Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d: Validate() = %v, want ErrInvalid", i, err)
		}
	}
}

func TestAnnouncementActive(t *testing.T) {
	now := time.Now()
	a := &Announcement{StartsAt: now, EndsAt: now.Add(time.Hour)}
	if !a.Active(now.Add(time.Minute)) || a.Active(now.Add(-time.Minute)) || a.Active(now.Add(time.Hour)) {
		t.Error("Active interval wrong")
	}
}
