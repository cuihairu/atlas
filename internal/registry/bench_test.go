package registry

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
)

var ctxBench = context.Background()

// BenchmarkRegister measures the write path: server upsert + initial
// heartbeat, memory store. QPS ≈ 1e9 / ns_per_op.
func BenchmarkRegister(b *testing.B) {
	svc := newTestService()
	req := testRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Unique IDs make each iteration a true insert (no upsert reuse);
		// the idempotent re-register case is covered by the store benchmark.
		r := req
		r.ID = "game-" + strconv.Itoa(i)
		if _, err := svc.Register(ctxBench, r); err != nil {
			b.Fatalf("register: %v", err)
		}
	}
}

// BenchmarkReRegister measures the idempotent upsert of an existing server —
// the Atlas-restart path every game server walks after a control-plane outage.
func BenchmarkReRegister(b *testing.B) {
	svc := newTestService()
	req := testRequest()
	if _, err := svc.Register(ctxBench, req); err != nil {
		b.Fatalf("seed register: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Register(ctxBench, req); err != nil {
			b.Fatalf("re-register: %v", err)
		}
	}
}

// BenchmarkHeartbeat measures the hot path every game server hits every ~10s.
func BenchmarkHeartbeat(b *testing.B) {
	svc := newTestService()
	if _, err := svc.Register(ctxBench, testRequest()); err != nil {
		b.Fatalf("seed register: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hb := model.Heartbeat{
			Players: i % 2000,
			Load:    0.5,
			Status:  model.StatusOnline,
		}
		if err := svc.Heartbeat(ctxBench, "game-1001", hb); err != nil {
			b.Fatalf("heartbeat: %v", err)
		}
	}
}

// BenchmarkHeartbeatParallel measures the registry under concurrent heartbeat
// fan-in from many game servers (the HAProxy fan-in shape).
func BenchmarkHeartbeatParallel(b *testing.B) {
	svc := newTestService()
	const servers = 64
	for i := 0; i < servers; i++ {
		r := testRequest()
		r.ID = "game-" + fmt.Sprintf("%04d", i)
		if _, err := svc.Register(ctxBench, r); err != nil {
			b.Fatalf("seed register: %v", err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			id := "game-" + fmt.Sprintf("%04d", i%servers)
			hb := model.Heartbeat{Players: i % 2000, Load: 0.5, Status: model.StatusOnline}
			if err := svc.Heartbeat(ctxBench, id, hb); err != nil {
				b.Fatalf("heartbeat: %v", err)
			}
			i++
		}
	})
}
