package health

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestRunWithSweepsAndCancel: Run drives sweeps on its ticker until the
// context is cancelled. A server past the offline threshold must be marked
// offline by the loop alone (no manual sweep call).
func TestRunWithSweepsAndCancel(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	suspectAfter, offlineAfter := 30*time.Millisecond, 60*time.Millisecond

	registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
	recordHeartbeat(t, ctx, mem, "srv-1", 0) // runtime data: sweep watches heartbeat age

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mon := New(mem, suspectAfter, offlineAfter, 10*time.Millisecond, logger)
	mon.WithMetrics(nil) // WithMetrics is nil-safe and optional

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mon.Run(runCtx)
		close(done)
	}()

	// Wait until the monitor loop has advanced the server to offline.
	deadline := time.Now().Add(3 * time.Second)
	for {
		srv, err := mem.GetServer(ctx, "srv-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if srv.Status == model.StatusOffline {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server still %s after 3s, want offline from Run loop", srv.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
		// Run returned promptly after cancel.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
