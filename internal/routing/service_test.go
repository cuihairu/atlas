package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// seed registers a server with a live runtime snapshot.
func seed(t *testing.T, mem *memory.Store, id, region string, status model.ServerStatus, players int, load float64, capacity int) {
	t.Helper()
	ctx := context.Background()
	if err := mem.RegisterServer(ctx, &model.Server{
		ID: id, Name: id, Region: region, Version: "1.0.0", Platform: "pc",
		Status: status, Capacity: capacity,
	}); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
	if err := mem.RecordHeartbeat(ctx, id, model.Heartbeat{Players: players, Load: load}); err != nil {
		t.Fatalf("heartbeat %s: %v", id, err)
	}
}

func TestRecommendFiltersByRegion(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 10, 0.1, 100)
	seed(t, mem, "eu-2", "eu", model.StatusOnline, 90, 0.9, 100)
	seed(t, mem, "na-1", "na", model.StatusOnline, 1, 0.01, 100)
	seed(t, mem, "eu-old", "eu", model.StatusOffline, 0, 0, 100)

	svc := New(mem, mem, mem)
	srv, reason, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "eu-1" {
		t.Errorf("expected eu-1 (lowest load in eu), got %s", srv.ID)
	}
	if reason != ReasonLowestLoad {
		t.Errorf("expected %q, got %q", ReasonLowestLoad, reason)
	}
}

func TestRecommendHighestCapacity(t *testing.T) {
	mem := memory.New()
	// Same load; b has more headroom.
	seed(t, mem, "a", "eu", model.StatusOnline, 50, 0.5, 100)
	seed(t, mem, "b", "eu", model.StatusOnline, 10, 0.5, 100)

	svc := New(mem, mem, mem)
	srv, reason, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "b" {
		t.Errorf("expected b (more capacity), got %s", srv.ID)
	}
	if reason != ReasonHighestCapacity {
		t.Errorf("expected %q, got %q", ReasonHighestCapacity, reason)
	}
}

func TestRecommendPrefersOwnedServer(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "busy", "eu", model.StatusOnline, 90, 0.9, 100)
	seed(t, mem, "idle", "eu", model.StatusOnline, 1, 0.01, 100)

	// Account 7 owns a character on the busy server.
	if err := mem.UpsertCharacter(context.Background(), &model.Character{
		AccountID: 7, ServerID: "busy", CharacterID: 1001, Name: "main",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	svc := New(mem, mem, mem)
	srv, reason, err := svc.Recommend(context.Background(), Request{Region: "eu", AccountID: 7})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "busy" {
		t.Errorf("expected owned 'busy' server, got %s", srv.ID)
	}
	if reason != ReasonHasCharacter {
		t.Errorf("expected %q, got %q", ReasonHasCharacter, reason)
	}
}

func TestRecommendFallsBackWhenFiltersMatchNothing(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "na-1", "na", model.StatusOnline, 10, 0.2, 100)

	svc := New(mem, mem, mem)
	srv, reason, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "na-1" {
		t.Errorf("expected relaxed pick na-1, got %s", srv.ID)
	}
	if reason != ReasonFallback {
		t.Errorf("expected %q, got %q", ReasonFallback, reason)
	}
}

func TestRecommendNoServerAvailable(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "only-offline", "eu", model.StatusOffline, 0, 0, 100)

	svc := New(mem, mem, mem)
	_, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRecommendSkipsOfflineEvenWithoutFilters(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "down", "eu", model.StatusSuspect, 0, 0, 100)

	svc := New(mem, mem, mem)
	_, _, err := svc.Recommend(context.Background(), Request{})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-online candidates, got %v", err)
	}
}

func TestDiagnoseMirrorsRecommendPipeline(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 10, 0.1, 100)
	seed(t, mem, "eu-2", "eu", model.StatusOnline, 90, 0.9, 100)
	seed(t, mem, "na-1", "na", model.StatusOnline, 1, 0.01, 100)
	seed(t, mem, "eu-full", "eu", model.StatusOnline, 100, 1.0, 100)
	seed(t, mem, "eu-maint", "eu", model.StatusMaintenance, 5, 0.05, 100)

	// The account already plays on eu-2 — the tiebreak must surface there.
	ctx := context.Background()
	if err := mem.UpsertCharacter(ctx, &model.Character{
		AccountID: 42, ServerID: "eu-2", CharacterID: 1, Name: "玩家", Level: 1,
	}); err != nil {
		t.Fatal(err)
	}

	svc := New(mem, mem, mem)
	req := Request{AccountID: 42, Region: "eu"}

	// Recommend is the contract Diagnose must mirror.
	want, wantReason, err := svc.Recommend(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	diag, err := svc.Diagnose(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if diag.Stage != "strict" {
		t.Errorf("stage = %q, want strict", diag.Stage)
	}
	if diag.WinnerID != want.ID || diag.WinnerReason != wantReason {
		t.Errorf("diagnose winner = %s/%s, recommend = %s/%s (must be the same decision)",
			diag.WinnerID, diag.WinnerReason, want.ID, wantReason)
	}

	verdicts := map[string]ServerVerdict{}
	for _, v := range diag.Servers {
		verdicts[v.Server.ID] = v
	}
	if len(diag.Servers) != 5 {
		t.Fatalf("whole fleet expected (rejected stay visible), got %d", len(diag.Servers))
	}
	// Owned server ranks first despite higher load (tiebreak #1).
	if v := verdicts["eu-2"]; v.Rank != 1 || !v.Owned {
		t.Errorf("eu-2 verdict = rank %d owned %v, want rank 1 owned true", v.Rank, v.Owned)
	}
	if v := verdicts["eu-1"]; v.Rank != 2 {
		t.Errorf("eu-1 rank = %d, want 2", v.Rank)
	}
	// Rejected servers keep their reason instead of vanishing.
	if v := verdicts["na-1"]; v.Rank != 0 || v.Reason != "region=na (需要 eu)" {
		t.Errorf("na-1 verdict = %+v, want rejected with region reason", v)
	}
	if v := verdicts["eu-maint"]; v.Rank != 0 || v.Eligible {
		t.Errorf("eu-maint verdict = %+v, want rejected + ineligible", v)
	}
	// Full server is a candidate but flagged ineligible (headroom = 0).
	if v := verdicts["eu-full"]; !v.MatchedStrict || v.Eligible {
		t.Errorf("eu-full verdict = %+v, want strict candidate but ineligible", v)
	}
}

func TestDiagnoseFallbackStageAndAccountEntries(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "na-1", "na", model.StatusOnline, 1, 0.01, 100)

	svc := New(mem, mem, mem)
	diag, err := svc.Diagnose(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatal(err)
	}
	if diag.Stage != "fallback" {
		t.Errorf("stage = %q, want fallback (no eu server exists)", diag.Stage)
	}
	if diag.WinnerID != "na-1" || diag.WinnerReason != ReasonFallback {
		t.Errorf("winner = %s/%s, want na-1/fallback", diag.WinnerID, diag.WinnerReason)
	}
	// In fallback stage the status-only pass legitimately keeps na-1 —
	// it is the winner, flagged as fallback-matched but not strict-matched.
	v := diag.Servers[0]
	if v.Rank != 1 || v.MatchedStrict || !v.MatchedFallback {
		t.Errorf("fallback candidate verdict = %+v, want rank 1 strict=false fallback=true", v)
	}
}
