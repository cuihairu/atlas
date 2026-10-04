package crossserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestListServersAllWalksPastTheStoreCap pins fleet-wide addressing: the
// store clamps every page to ListServersMaxLimit, so the walk must
// terminate on a short page at exactly that size. Asking for a bigger
// page (the old pageSize 500) made every return look short and stopped
// the walk after page one — fleets past the cap lost signal addressing
// and callback dispatch for every server beyond it.
func TestListServersAllWalksPastTheStoreCap(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	const fleet = store.ListServersMaxLimit + 50
	for i := 0; i < fleet; i++ {
		srv := &model.Server{
			ID: fmt.Sprintf("srv-%03d", i), Name: fmt.Sprintf("srv-%03d", i),
			Type: "game", Region: "cn-east", Version: "1.0.0", Platform: "pc",
			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 100,
		}
		if err := mem.RegisterServer(ctx, srv); err != nil {
			t.Fatalf("register %d: %v", i, err)
		}
	}

	svc := New(nil, mem, httpadapter.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	got, err := svc.listServersAll(ctx)
	if err != nil {
		t.Fatalf("listServersAll: %v", err)
	}
	if len(got) != fleet {
		t.Fatalf("listServersAll = %d servers, want %d (past-the-cap truncation)", len(got), fleet)
	}
	// Cursor walk must stay ID-ascending so the next page is well-defined.
	for i := 1; i < len(got); i++ {
		if got[i-1].ID >= got[i].ID {
			t.Fatalf("not ascending at %d: %s >= %s", i, got[i-1].ID, got[i].ID)
		}
	}
}
