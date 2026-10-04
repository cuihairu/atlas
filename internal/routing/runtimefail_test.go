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
