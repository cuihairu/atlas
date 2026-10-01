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
