package routing

import (
	"context"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// weightedSeed registers a server with an explicit operator weight.
func weightedSeed(t *testing.T, mem *memory.Store, id, region string, players int, load float64, capacity, weight int) {
	t.Helper()
	ctx := context.Background()
	if err := mem.RegisterServer(ctx, &model.Server{
		ID: id, Name: id, Region: region, Version: "1.0.0", Platform: "pc",
		Status: model.StatusOnline, Capacity: capacity, Weight: weight,
	}); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
	if err := mem.RecordHeartbeat(ctx, id, model.Heartbeat{Players: players, Load: load}); err != nil {
		t.Fatalf("heartbeat %s: %v", id, err)
	}
}

// TestRecommendWeightBoostsCandidate: weight below 100 makes a server look
// less loaded — a 0.6-load server at weight 200 (effective 0.3) beats a
// neutral 0.4-load server.
func TestRecommendWeightBoostsCandidate(t *testing.T) {
	mem := memory.New()
	weightedSeed(t, mem, "boosted", "eu", 60, 0.6, 100, 200)
	weightedSeed(t, mem, "neutral", "eu", 40, 0.4, 100, 0)

	svc := New(mem, mem, mem, mem)
	srv, reason, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "boosted" {
		t.Errorf("expected boosted (effective load 0.30 < 0.40), got %s", srv.ID)
	}
	if reason != ReasonLowestLoad {
		t.Errorf("expected %q, got %q", ReasonLowestLoad, reason)
	}
}

// TestRecommendWeightDampensCandidate: weight above 100 makes a server look
// more loaded — a 0.4-load server at weight 50 (effective 0.8) loses to a
// neutral 0.6-load server.
func TestRecommendWeightDampensCandidate(t *testing.T) {
	mem := memory.New()
	weightedSeed(t, mem, "dampened", "eu", 40, 0.4, 100, 50)
	weightedSeed(t, mem, "neutral", "eu", 60, 0.6, 100, 0)

	svc := New(mem, mem, mem, mem)
	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "neutral" {
		t.Errorf("expected neutral (effective load 0.60 < 0.80), got %s", srv.ID)
	}
}

// TestRecommendWeightHundredIsNeutral: weight=100 must not change the
// decision versus no weight at all.
func TestRecommendWeightHundredIsNeutral(t *testing.T) {
	mem := memory.New()
	weightedSeed(t, mem, "hundred", "eu", 10, 0.1, 100, 100)
	weightedSeed(t, mem, "none", "eu", 90, 0.9, 100, 0)

	svc := New(mem, mem, mem, mem)
	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "hundred" {
		t.Errorf("expected hundred (weight 100 = neutral), got %s", srv.ID)
	}
}

// TestRecommendWeightNeverBeatsOwnedCharacter: the weight bias only modulates
// the load comparison; the account's home server still wins (safety
// backbone unchanged).
func TestRecommendWeightNeverBeatsOwnedCharacter(t *testing.T) {
	mem := memory.New()
	weightedSeed(t, mem, "light", "eu", 1, 0.01, 100, 1000)
	weightedSeed(t, mem, "home", "eu", 99, 0.99, 100, 0)

	ctx := context.Background()
	if err := mem.UpsertCharacter(ctx, &model.Character{
		AccountID: 9, ServerID: "home", CharacterID: 1, Name: "alt",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	svc := New(mem, mem, mem, mem)
	srv, reason, err := svc.Recommend(ctx, Request{Region: "eu", AccountID: 9})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if srv.ID != "home" || reason != ReasonHasCharacter {
		t.Errorf("expected home/has_character, got %s/%s", srv.ID, reason)
	}
}

// TestDiagnoseExposesWeightAndEffectiveLoad: the verdict carries both the
// operator weight and the effective load the ranking actually used, so a
// weight-biased win explains itself on the 排查页.
func TestDiagnoseExposesWeightAndEffectiveLoad(t *testing.T) {
	mem := memory.New()
	weightedSeed(t, mem, "boosted", "eu", 60, 0.6, 100, 200)
	weightedSeed(t, mem, "neutral", "eu", 40, 0.4, 100, 0)

	svc := New(mem, mem, mem, mem)
	diag, err := svc.Diagnose(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if diag.WinnerID != "boosted" {
		t.Errorf("winner = %s, want boosted", diag.WinnerID)
	}
	v := diag.Servers[0]
	if v.Server.ID != "boosted" {
		t.Fatalf("verdict[0] = %s, want boosted", v.Server.ID)
	}
	if v.Weight != 200 {
		t.Errorf("verdict weight = %d, want 200", v.Weight)
	}
	if got, want := v.EffectiveLoad, 0.3; got != want {
		t.Errorf("verdict effective load = %v, want %v", got, want)
	}
}
