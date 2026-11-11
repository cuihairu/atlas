package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// errRuntimes fails every batched runtime read so the fail-closed path
// of the decision surfaces can be driven without a real outage.
type errRuntimes struct {
	store.RuntimeStore
	err error
}

func (e *errRuntimes) GetRuntimes(ctx context.Context, ids []string) (map[string]model.Runtime, error) {
	return nil, e.err
}

// TestRecommendRuntimeFailureFailsClosed: ranking on unknown load could
// steer players into a full or dying server, so a runtime-store failure
// must propagate instead of silently scoring on archive data (the old
// per-key loop swallowed every read error).
func TestRecommendRuntimeFailureFailsClosed(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 10, 0.1, 100)
	seed(t, mem, "eu-2", "eu", model.StatusOnline, 90, 0.9, 100)

	svc := New(mem, &errRuntimes{RuntimeStore: mem, err: errors.New("redis down")}, mem, mem)
	if _, _, err := svc.Recommend(context.Background(), Request{Region: "eu"}); err == nil {
		t.Fatal("Recommend with failed runtime store = nil error, want fail closed")
	}
}

// TestDiagnoseRuntimeFailurePropagates: the diagnosis merges runtime for
// the whole fleet; a failed read must surface, not produce verdicts
// judged on stale archive values.
func TestDiagnoseRuntimeFailurePropagates(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-1", "eu", model.StatusOnline, 10, 0.1, 100)

	svc := New(mem, &errRuntimes{RuntimeStore: mem, err: errors.New("redis down")}, mem, mem)
	if _, err := svc.Diagnose(context.Background(), Request{Region: "eu"}); err == nil {
		t.Fatal("Diagnose with failed runtime store = nil error, want propagation")
	}
}

// TestDiagnoseFlagsMissingRuntimeSnapshot pins the archive-ranked marker:
// a server with no runtime entry (never heartbeated, or the Redis key
// expired) is still a candidate scored on its registration-time archive
// values — Diagnose must say so via LiveRuntime instead of letting an
// archive-zero row look like the least-loaded server in the fleet.
func TestDiagnoseFlagsMissingRuntimeSnapshot(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "eu-live", "eu", model.StatusOnline, 10, 0.4, 100)
	if err := mem.RegisterServer(context.Background(), &model.Server{
		ID: "eu-arch", Name: "eu-arch", Region: "eu", Version: "1.0.0", Platform: "pc",
		Status: model.StatusOnline, Capacity: 100,
	}); err != nil {
		t.Fatalf("register eu-arch: %v", err)
	}

	svc := New(mem, mem, mem, mem)
	diag, err := svc.Diagnose(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	seen := map[string]bool{}
	for _, v := range diag.Servers {
		seen[v.Server.ID] = v.LiveRuntime
	}
	if !seen["eu-live"] {
		t.Error("eu-live has a runtime snapshot, want live_runtime=true")
	}
	if seen["eu-arch"] {
		t.Error("eu-arch never heartbeated, want live_runtime=false")
	}
}
