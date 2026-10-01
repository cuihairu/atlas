package discovery

import (
	"context"
	"strconv"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var ctxBench = context.Background()

// seedServers registers n online servers with live runtime data — a fleet at
// steady state, which is what discovery serves.
func seedServers(b *testing.B, servers store.ServerStore, runtime store.RuntimeStore, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		id := "game-" + strconv.Itoa(i)
		srv := &model.Server{
			ID:       id,
			Name:     "Server " + id,
			Type:     "game",
			Region:   "cn-east",
			Version:  "1.0.0",
			Platform: "android",
			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30000 + i},
			Capacity: 2000,
			Status:   model.StatusOnline,
		}
		if err := servers.RegisterServer(ctxBench, srv); err != nil {
			b.Fatalf("seed %s: %v", id, err)
		}
		rt := model.Heartbeat{Players: i % 2000, Load: 0.5, Status: model.StatusOnline}
		if err := runtime.RecordHeartbeat(ctxBench, id, rt); err != nil {
			b.Fatalf("seed runtime %s: %v", id, err)
		}
	}
}

// BenchmarkListServers measures the client-facing list path at fleet sizes
// clients actually see: the default (visible-only) filter with runtime merge.
func BenchmarkListServers(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		mem := memory.New()
		seedServers(b, mem, mem, n)
		svc := New(mem, mem)

		b.Run("fleet_"+strconv.Itoa(n), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := svc.ListServers(ctxBench, store.ServerFilter{}); err != nil {
					b.Fatalf("list: %v", err)
				}
			}
		})
	}
}

// BenchmarkListServersRegion measures the filtered path (region predicate +
// cursor pagination) used by region-aware clients.
func BenchmarkListServersRegion(b *testing.B) {
	mem := memory.New()
	seedServers(b, mem, mem, 1000)
	svc := New(mem, mem)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := store.ServerFilter{Region: "cn-east", Limit: 100}
		if _, err := svc.ListServers(ctxBench, f); err != nil {
			b.Fatalf("list: %v", err)
		}
	}
}

// BenchmarkGetServer measures the single-server detail path.
func BenchmarkGetServer(b *testing.B) {
	mem := memory.New()
	seedServers(b, mem, mem, 1000)
	svc := New(mem, mem)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.GetServer(ctxBench, "game-500"); err != nil {
			b.Fatalf("get: %v", err)
		}
	}
}
