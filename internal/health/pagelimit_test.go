package health

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestSweepCoversFleetsPastTheDefaultPage pins the sweep contract: every
// auto-managed server is checked, cursor-paginated at the store cap. A
// bare list call takes the store's 50-row default page — servers past it
// would never transition to suspect/offline, no matter how long they
// stayed silent.
func TestSweepCoversFleetsPastTheDefaultPage(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	// 60 servers — past the 50-row default page, under the hard cap.
	const n = 60
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("srv-%03d", i)
		registerServer(t, ctx, mem, id, model.StatusOnline)
		recordHeartbeat(t, ctx, mem, id, 10)
	}

	mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
	heartbeatTime := time.Now()
	mon.now = func() time.Time { return heartbeatTime.Add(35 * time.Second) }

	if err := mon.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	suspect, err := mem.ListServers(ctx, store.ServerFilter{Status: model.StatusSuspect, Limit: store.ListServersMaxLimit})
	if err != nil {
		t.Fatalf("list suspect: %v", err)
	}
	if len(suspect) != n {
		t.Fatalf("suspect count = %d, want %d (default-page truncation left servers unmonitored)", len(suspect), n)
	}
}
