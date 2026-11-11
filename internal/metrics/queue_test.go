package metrics

// 指令写队列指标测试 (TODO v0.2 ④): the collector reads QueueStats at
// scrape time — these assertions pin the atlas_store_queue_* family against
// real queue writes (register via the queue, then scrape).

import (
	"context"
	"strings"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
)

func TestQueueMetricsScrape(t *testing.T) {
	m, mem := newTestMetrics(t)
	ctx := context.Background()

	for _, id := range []string{"q1", "q2", "q3"} {
		if err := mem.RegisterServer(ctx, &model.Server{ID: id, Name: id, Status: model.StatusOnline}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	// One heartbeat: a single hot-lane instruction never coalesces, so the
	// counters below stay exact.
	hb := model.Heartbeat{Players: 1, Load: 0.5, Status: model.StatusOnline}
	if err := mem.RecordHeartbeat(ctx, "q1", hb); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	out := scrape(t, m)

	// Depths: idle after synchronous submits — everything committed.
	if !hasLine(out, `atlas_store_queue_depth_control 0`) {
		t.Errorf("expected idle control depth 0, got:\n%s", out)
	}
	if !hasLine(out, `atlas_store_queue_depth_hot 0`) {
		t.Errorf("expected idle hot depth 0, got:\n%s", out)
	}
	// Watermark advanced by the three registers (+ heartbeat if applied).
	if !strings.Contains(out, `atlas_store_queue_watermark`) {
		t.Errorf("expected watermark series, got:\n%s", out)
	}
	if !hasLine(out, `atlas_store_queue_enqueued_total 4`) {
		t.Errorf("expected enqueued_total = 4 (3 registers + 1 heartbeat), got:\n%s", out)
	}
	if !hasLine(out, `atlas_store_queue_applied_total 4`) {
		t.Errorf("expected applied_total = 4 (no merge at this rate), got:\n%s", out)
	}
	if !hasLine(out, `atlas_store_queue_applied_by_kind_total{kind="register_server"} 3`) {
		t.Errorf("expected register_server kind counter = 3, got:\n%s", out)
	}
	if !strings.Contains(out, `atlas_store_queue_applied_by_kind_total{kind="heartbeat"}`) {
		t.Errorf("expected heartbeat kind counter present, got:\n%s", out)
	}
	// Capture ring holds every applied instruction.
	if !hasLine(out, `atlas_store_queue_capture_entries 4`) {
		t.Errorf("expected capture_entries = 4, got:\n%s", out)
	}
	if !strings.Contains(out, `atlas_store_queue_backpressure_total`) {
		t.Errorf("expected backpressure counter series, got:\n%s", out)
	}
	if !strings.Contains(out, `atlas_store_queue_flush_duration_seconds`) {
		t.Errorf("expected flush duration series (registers count as flushes), got:\n%s", out)
	}
	if !strings.Contains(out, `atlas_store_queue_flush_batch_size`) {
		t.Errorf("expected flush batch size series, got:\n%s", out)
	}
}
