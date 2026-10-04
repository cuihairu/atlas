package fleet

import (
	"context"
	"fmt"
	"testing"

	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestIndex_ReconcileWalksPastTheStoreCap pins the rebuild contract: the
// store clamps every page to ListServersMaxLimit, so the walk must page
// at exactly that size. The old Limit 500 walk saw short pages on every
// iteration and stopped after page one — the index silently held only
// the first 200 servers of a bigger fleet.
func TestIndex_ReconcileWalksPastTheStoreCap(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	const fleet = store.ListServersMaxLimit + 50
	for i := 0; i < fleet; i++ {
		if err := mem.RegisterServer(ctx, mkServer(fmt.Sprintf("srv-%03d", i), "eu", "online", "1.0.0", 100)); err != nil {
			t.Fatalf("register %d: %v", i, err)
		}
	}

	idx := New()
	if err := idx.Reconcile(ctx, mem, mem); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	st := idx.Stats()
	if st.TotalServers != fleet {
		t.Fatalf("reconcile snapshot = %d servers, want %d (past-the-cap truncation)", st.TotalServers, fleet)
	}
	if st.ServersByRegion["eu"] != fleet {
		t.Fatalf("region projection = %d, want %d", st.ServersByRegion["eu"], fleet)
	}
}
