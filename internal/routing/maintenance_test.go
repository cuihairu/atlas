package routing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// frozen pins the window arithmetic so tests are deterministic: the lead
// horizon and Active(now) all evaluate against this instant.
var frozen = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func frozenNow() time.Time { return frozen }

// window seeds a maintenance window on the store.
func window(t *testing.T, mem *memory.Store, id, serverID string, start, end time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := mem.CreateMaintenanceWindow(ctx, &model.MaintenanceWindow{
		ID: id, ServerID: serverID, StartAt: start, EndAt: end, CreatedAt: frozen,
	}); err != nil {
		t.Fatalf("create window %s: %v", id, err)
	}
}

func TestRecommendAvoidsActiveMaintenanceWindow(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100) // lowest load…
	seed(t, mem, "eu-2", "eu", model.StatusOnline, 50, 0.5, 100)
	// …but eu-1 is inside its maintenance window: the monitor has not swept
	// yet, status still says online — recommendation must not trust status.
	window(t, mem, "w-eu-1", "eu-1", frozen.Add(-time.Hour), frozen.Add(time.Hour))

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "eu-2" {
		t.Errorf("expected eu-2 (eu-1 window-blocked), got %s", srv.ID)
	}
}

func TestRecommendAvoidsUpcomingWindowWithinLead(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
	seed(t, mem, "eu-2", "eu", model.StatusOnline, 50, 0.5, 100)
	// 维护前引导: window opens in 2 minutes, inside the default 5m lead —
	// eu-1 is a landing spot that dies mid-session.
	window(t, mem, "w-eu-1", "eu-1", frozen.Add(2*time.Minute), frozen.Add(time.Hour))

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "eu-2" {
		t.Errorf("expected eu-2 (eu-1 window starts within lead), got %s", srv.ID)
	}
}

func TestRecommendKeepsWindowBeyondLead(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
	seed(t, mem, "eu-2", "eu", model.StatusOnline, 50, 0.5, 100)
	// Window opens in 30 minutes — outside the default 5m lead: eu-1 keeps
	// serving until the steering horizon actually begins.
	window(t, mem, "w-eu-1", "eu-1", frozen.Add(30*time.Minute), frozen.Add(2*time.Hour))

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "eu-1" {
		t.Errorf("expected eu-1 (window beyond lead), got %s", srv.ID)
	}
}

func TestRecommendIgnoresEndedWindow(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
	// Window already ended; the monitor deletes it at sweep time, but until
	// then it must not keep blocking — the server is restored.
	window(t, mem, "w-eu-1", "eu-1", frozen.Add(-2*time.Hour), frozen.Add(-time.Hour))

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "eu-1" {
		t.Errorf("expected eu-1 (ended window must not block), got %s", srv.ID)
	}
}

func TestRecommendAllWindowedFailsClosed(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
	window(t, mem, "w-eu-1", "eu-1", frozen.Add(-time.Hour), frozen.Add(time.Hour))

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	_, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when every candidate is window-blocked, got %v", err)
	}
}

func TestRecommendFallbackAvoidsWindows(t *testing.T) {
	mem := memory.New()
	// Strict request (region=eu) matches nothing safe: eu-1 is windowed.
	// The fallback pass relaxes the profile filters but never the window
	// exclusion — na-1 is the honest winner.
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
	seed(t, mem, "na-1", "na", model.StatusOnline, 90, 0.9, 100)
	window(t, mem, "w-eu-1", "eu-1", frozen.Add(-time.Hour), frozen.Add(time.Hour))

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	srv, reason, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "na-1" || reason != ReasonFallback {
		t.Errorf("expected na-1/%s (fallback must also skip windowed), got %s/%s",
			ReasonFallback, srv.ID, reason)
	}
}

func TestRecommendOwnedWindowedServerStillExcluded(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "busy", "eu", model.StatusOnline, 90, 0.9, 100)
	seed(t, mem, "idle", "eu", model.StatusOnline, 1, 0.01, 100)
	window(t, mem, "w-busy", "busy", frozen.Add(-time.Hour), frozen.Add(time.Hour))

	ctx := context.Background()
	if err := mem.UpsertCharacter(ctx, &model.Character{
		AccountID: 7, ServerID: "busy", CharacterID: 1001, Name: "main",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	srv, reason, err := svc.Recommend(ctx, Request{Region: "eu", AccountID: 7})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	// Character stickiness must not outweigh the window constraint: steering
	// the player to their own server mid-window lands them in maintenance.
	if srv.ID != "idle" || reason == ReasonHasCharacter {
		t.Errorf("expected idle (owned windowed server excluded), got %s/%s", srv.ID, reason)
	}
}

func TestDiagnoseSurfacesMaintenanceWindow(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-clean", "eu", model.StatusOnline, 1, 0.01, 100)
	seed(t, mem, "eu-active", "eu", model.StatusOnline, 2, 0.02, 100)
	seed(t, mem, "eu-upcoming", "eu", model.StatusOnline, 3, 0.03, 100)
	seed(t, mem, "eu-ended", "eu", model.StatusOnline, 4, 0.04, 100)
	window(t, mem, "w-active", "eu-active", frozen.Add(-time.Hour), frozen.Add(time.Hour))
	window(t, mem, "w-upcoming", "eu-upcoming", frozen.Add(2*time.Minute), frozen.Add(time.Hour))
	window(t, mem, "w-ended", "eu-ended", frozen.Add(-2*time.Hour), frozen.Add(-time.Hour))

	ctx := context.Background()
	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	req := Request{Region: "eu"}

	// The diagnosis must mirror the decision the player would actually get.
	want, _, err := svc.Recommend(ctx, req)
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	diag, err := svc.Diagnose(ctx, req)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if diag.Stage != "strict" {
		t.Errorf("stage = %q, want strict", diag.Stage)
	}
	if diag.WinnerID != want.ID {
		t.Errorf("winner = %s, recommend = %s (must be the same decision)", diag.WinnerID, want.ID)
	}

	verdicts := map[string]ServerVerdict{}
	for _, v := range diag.Servers {
		verdicts[v.Server.ID] = v
	}
	// Both windowed servers are non-candidates with the window named and
	// the disqualifying record attached; the ended window stays clean.
	if v := verdicts["eu-active"]; v.Rank != 0 || v.Eligible ||
		v.Reason != "maintenance_window=active" || v.MaintenanceWindow == nil ||
		v.MaintenanceWindow.ID != "w-active" {
		t.Errorf("eu-active verdict = %+v, want rejected/active window attached", v)
	}
	if v := verdicts["eu-upcoming"]; v.Rank != 0 || v.Eligible ||
		v.Reason != "maintenance_window=upcoming" || v.MaintenanceWindow == nil ||
		v.MaintenanceWindow.ID != "w-upcoming" {
		t.Errorf("eu-upcoming verdict = %+v, want rejected/upcoming window attached", v)
	}
	if v := verdicts["eu-ended"]; v.Rank == 0 || !v.Eligible || v.MaintenanceWindow != nil {
		t.Errorf("eu-ended verdict = %+v, want candidate/eligible/no window", v)
	}
	if v := verdicts["eu-clean"]; v.Rank != 1 || !v.Eligible {
		t.Errorf("eu-clean verdict = %+v, want rank 1 eligible", v)
	}
}

func TestDiagnoseWindowStoreFailurePropagates(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)

	svc := New(mem, mem, mem, windowErrStore{}).withClock(frozenNow)
	ctx := context.Background()
	if _, _, err := svc.Recommend(ctx, Request{Region: "eu"}); err == nil {
		t.Error("Recommend with failing window store: expected error (fail closed), got nil")
	}
	if _, err := svc.Diagnose(ctx, Request{Region: "eu"}); err == nil {
		t.Error("Diagnose with failing window store: expected error (fail closed), got nil")
	}
}

func TestRecommendWithoutWindowStore(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
	window(t, mem, "w-eu-1", "eu-1", frozen.Add(-time.Hour), frozen.Add(time.Hour))

	// nil window store (constructor contract: tests may pass none) disables
	// window awareness instead of failing — behavior predating this change.
	svc := New(mem, mem, mem, nil).withClock(frozenNow)
	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "eu-1" {
		t.Errorf("expected eu-1 (window awareness off), got %s", srv.ID)
	}
}

func TestWithMaintenanceLead(t *testing.T) {
	newFixture := func(t *testing.T, lead time.Duration) (*Service, *memory.Store) {
		t.Helper()
		mem := memory.New()
		seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
		seed(t, mem, "eu-2", "eu", model.StatusOnline, 50, 0.5, 100)
		svc := New(mem, mem, mem, mem).withClock(frozenNow).WithMaintenanceLead(lead)
		return svc, mem
	}

	t.Run("short lead keeps imminent window", func(t *testing.T) {
		svc, mem := newFixture(t, time.Minute)
		// Window opens in 2 minutes — outside a 1m lead: eu-1 stays eligible.
		window(t, mem, "w-eu-1", "eu-1", frozen.Add(2*time.Minute), frozen.Add(time.Hour))
		srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
		if err != nil {
			t.Fatalf("Recommend: %v", err)
		}
		if srv.ID != "eu-1" {
			t.Errorf("expected eu-1 (window outside 1m lead), got %s", srv.ID)
		}
	})

	t.Run("zero lead still blocks active window", func(t *testing.T) {
		svc, mem := newFixture(t, 0)
		window(t, mem, "w-eu-1", "eu-1", frozen.Add(-time.Hour), frozen.Add(time.Hour))
		srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
		if err != nil {
			t.Fatalf("Recommend: %v", err)
		}
		if srv.ID != "eu-2" {
			t.Errorf("expected eu-2 (active window blocks even with lead 0), got %s", srv.ID)
		}
	})

	t.Run("zero lead keeps upcoming window", func(t *testing.T) {
		svc, mem := newFixture(t, 0)
		window(t, mem, "w-eu-1", "eu-1", frozen.Add(2*time.Minute), frozen.Add(time.Hour))
		srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
		if err != nil {
			t.Fatalf("Recommend: %v", err)
		}
		if srv.ID != "eu-1" {
			t.Errorf("expected eu-1 (lead 0 disables upcoming exclusion), got %s", srv.ID)
		}
	})
}

func TestMaintenanceBlocklistPicksMostImmediateWindow(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 1, 0.01, 100)
	// Two blockers for one server: the active window outranks the upcoming.
	window(t, mem, "w-upcoming", "eu-1", frozen.Add(2*time.Minute), frozen.Add(time.Hour))
	window(t, mem, "w-active", "eu-1", frozen.Add(-time.Hour), frozen.Add(time.Hour))

	svc := New(mem, mem, mem, mem).withClock(frozenNow)
	diag, err := svc.Diagnose(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	for _, v := range diag.Servers {
		if v.Server.ID == "eu-1" {
			if v.MaintenanceWindow == nil || v.MaintenanceWindow.ID != "w-active" {
				t.Errorf("expected the active window as the blocker, got %+v", v.MaintenanceWindow)
			}
		}
	}
}

// windowErrStore fails every list call — pins the fail-closed contract:
// unknown window state must not serve recommendations.
type windowErrStore struct{ store.MaintenanceWindowStore }

func (windowErrStore) ListMaintenanceWindows(context.Context, string, int) ([]*model.MaintenanceWindow, error) {
	return nil, errors.New("window store down")
}
