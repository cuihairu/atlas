package memory

// Instruction-queue semantics (docs/performance.md §性能设计 — 指令化调度):
// merge rules, per-entity ordering, idempotency, replay/dry-run, backpressure
// and the watermark. Batch-level tests drive applyBatchN directly for
// determinism; the public API path is exercised by the contract suite.

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
)

func hbInstr(target string, players int) *instruction {
	return &instruction{
		kind:    opHeartbeat,
		target:  target,
		runtime: &model.Runtime{Players: players, LastSeenAt: time.Now()},
		out:     make(chan instructionResult, 1),
	}
}

func TestQueueMergeNoLoss(t *testing.T) {
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}

	// Ten heartbeats for the same server in one batch coalesce to the
	// newest; nothing else in the batch is lost. Counters are read as
	// deltas over the setup writes (register also goes through the queue).
	before := s.QueueStats()
	batch := make([]*instruction, 0, 11)
	batch = append(batch, hbInstr("srv-1", 0))
	for i := 1; i <= 10; i++ {
		batch = append(batch, hbInstr("srv-1", i))
	}
	s.applyBatchN(batch)

	stats := s.QueueStats()
	if d := stats.Merged - before.Merged; d != 10 {
		t.Errorf("merged delta = %d, want 10 (11 same-target heartbeats, newest survives)", d)
	}
	if d := stats.Applied - before.Applied; d != 1 {
		t.Errorf("applied delta = %d, want 1", d)
	}
	rt, err := s.GetRuntime(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Players != 10 {
		t.Errorf("players = %d, want 10 (newest heartbeat wins)", rt.Players)
	}
	if d := stats.Watermark - before.Watermark; d != 1 {
		t.Errorf("watermark delta = %d, want 1 (one applied instruction)", d)
	}
}

func TestQueueDifferentTargetsNotMerged(t *testing.T) {
	s := New()
	defer s.Close()
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := s.RegisterServer(ctx, &model.Server{ID: id, Name: id, Region: "r"}); err != nil {
			t.Fatal(err)
		}
	}

	s.applyBatchN([]*instruction{hbInstr("a", 1), hbInstr("b", 2), hbInstr("a", 3)})
	stats := s.QueueStats()
	if stats.Merged != 1 {
		t.Errorf("merged = %d, want 1 (only the same-server pair)", stats.Merged)
	}
	ra, _ := s.GetRuntime(ctx, "a")
	rb, _ := s.GetRuntime(ctx, "b")
	if ra.Players != 3 || rb.Players != 2 {
		t.Errorf("players = %d/%d, want 3/2 (merge never crosses targets)", ra.Players, rb.Players)
	}
}

func TestQueueOrderSensitiveKindsNotMerged(t *testing.T) {
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}

	// Status is mergeable, tags are mergeable — but across kinds nothing
	// coalesces, and two status flips both apply (FIFO within the batch).
	st := func(st model.ServerStatus) *instruction {
		return &instruction{
			kind: opUpdateServerStatus, target: "srv-1", status: st,
			statusAt: time.Now(), out: make(chan instructionResult, 1),
		}
	}
	s.applyBatchN([]*instruction{
		st(model.StatusOnline), st(model.StatusOffline),
		{kind: opUpdateServerTags, target: "srv-1",
			tags: []model.ServerTag{{Code: "hot", Public: true}}, tagsAt: time.Now(),
			out: make(chan instructionResult, 1)},
	})
	srv, err := s.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Status != model.StatusOffline {
		t.Errorf("status = %s, want offline (last write wins, no cross-kind merge)", srv.Status)
	}
	if !model.HasTag(srv.Tags, "hot") {
		t.Errorf("tags = %+v, want hot applied", srv.Tags)
	}
	if stats := s.QueueStats(); stats.Watermark != 3 {
		t.Errorf("watermark = %d, want 3 (three applied instructions)", stats.Watermark)
	}
}

func TestQueueIdempotentReplay(t *testing.T) {
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}

	before := s.QueueStats()
	first := hbInstr("srv-1", 42)
	first.idemKey = "hb:retry-1"
	s.applyBatchN([]*instruction{first})

	// A retry (same idempotency key) must not apply twice.
	retry := hbInstr("srv-1", 99)
	retry.idemKey = "hb:retry-1"
	s.applyBatchN([]*instruction{retry})

	stats := s.QueueStats()
	if stats.IdempotentHits != 1 {
		t.Errorf("idempotent hits = %d, want 1", stats.IdempotentHits)
	}
	rt, err := s.GetRuntime(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Players != 42 {
		t.Errorf("players = %d, want 42 (retry payload must not overwrite)", rt.Players)
	}
	if d := stats.Watermark - before.Watermark; d != 1 {
		t.Errorf("watermark delta = %d, want 1 (retry consumed no version)", d)
	}

	// A distinct key applies normally.
	fresh := hbInstr("srv-1", 50)
	fresh.idemKey = "hb:retry-2"
	s.applyBatchN([]*instruction{fresh})
	if stats := s.QueueStats(); stats.IdempotentHits != 1 {
		t.Errorf("idempotent hits = %d, want still 1", stats.IdempotentHits)
	}
}

func TestQueueReplayAndDryRun(t *testing.T) {
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r", Capacity: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHeartbeat(ctx, "srv-1", model.Heartbeat{Players: 7, Load: 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertCharacter(ctx, &model.Character{
		AccountID: 1, ServerID: "srv-1", CharacterID: 100, Name: "alice", Level: 9,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateServerStatus(ctx, "srv-1", model.StatusOnline); err != nil {
		t.Fatal(err)
	}

	byKind, watermark, err := DryRun(s)
	if err != nil {
		t.Fatal(err)
	}
	if byKind["register_server"] != 1 || byKind["heartbeat"] != 1 ||
		byKind["upsert_character"] != 1 || byKind["update_server_status"] != 1 {
		t.Errorf("applied by kind = %+v, want one of each write", byKind)
	}
	if watermark == 0 {
		t.Error("dry-run watermark = 0, want the replayed stream's final version")
	}

	// Replay reproduces the live state on a fresh store.
	sandbox := New()
	defer sandbox.Close()
	if err := ReplayInto(sandbox, s); err != nil {
		t.Fatal(err)
	}
	live, err := s.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := sandbox.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("replayed server missing: %v", err)
	}
	if replayed.Status != live.Status || replayed.Capacity != live.Capacity {
		t.Errorf("replayed server = %+v, want status/capacity of live %+v", replayed, live)
	}
	liveCh, _ := s.GetCharacterByCharacterID(ctx, 100)
	replayCh, err := sandbox.GetCharacterByCharacterID(ctx, 100)
	if err != nil {
		t.Fatalf("replayed character missing: %v", err)
	}
	if replayCh.Name != liveCh.Name || replayCh.Level != liveCh.Level {
		t.Errorf("replayed character = %+v, want %+v", replayCh, liveCh)
	}
	liveRt, _ := s.GetRuntime(ctx, "srv-1")
	replayRt, err := sandbox.GetRuntime(ctx, "srv-1")
	if err != nil {
		t.Fatalf("replayed runtime missing: %v", err)
	}
	if replayRt.Players != liveRt.Players {
		t.Errorf("replayed players = %d, want %d", replayRt.Players, liveRt.Players)
	}
}

func TestQueueBackpressureInline(t *testing.T) {
	// A store with no lane consumers: the buffer fills, and the next
	// submit degrades to an inline synchronous apply (backpressure path).
	s := New()
	if err := s.RegisterServer(context.Background(), &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}
	s.Close() // lanes drained and stopped

	// Swap in a fresh, unconsumed hot lane with capacity 1.
	s.hotCh = []chan *instruction{make(chan *instruction, 1)}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	// Fills the buffer (never applied — no consumer); the caller stops
	// waiting via the canceled context.
	if err := s.RecordHeartbeat(canceled, "srv-1", model.Heartbeat{Players: 1}); err == nil {
		t.Error("canceled-context submit = nil error, want ctx.Err (instruction still pending)")
	}
	// Buffer full → inline apply.
	if err := s.RecordHeartbeat(canceled, "srv-1", model.Heartbeat{Players: 2}); err != nil {
		t.Fatalf("backpressure submit = %v, want nil (inline apply succeeds)", err)
	}

	stats := s.QueueStats()
	if stats.BackpressureSync != 1 {
		t.Errorf("backpressure count = %d, want 1", stats.BackpressureSync)
	}
	// Submit-path invariant: every enqueued instruction is applied,
	// merged away, or still pending in a lane — none silently dropped.
	pending := stats.DepthControl + stats.DepthHot
	if stats.Enqueued != stats.Applied+stats.Merged+uint64(pending) {
		t.Errorf("enqueued %d != applied %d + merged %d + pending %d",
			stats.Enqueued, stats.Applied, stats.Merged, pending)
	}
	if stats.DepthHot != 1 {
		t.Errorf("hot depth = %d, want 1 (the pending buffered instruction)", stats.DepthHot)
	}
	if stats.Watermark != 2 {
		t.Errorf("watermark = %d, want 2 (register + inline apply)", stats.Watermark)
	}
	rt, err := s.GetRuntime(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Players != 2 {
		t.Errorf("players = %d, want 2 (inline write visible immediately)", rt.Players)
	}
}

func TestQueueMergedCallerStillUnblocked(t *testing.T) {
	// Regression: a coalesced (superseded) heartbeat's caller must still
	// receive its commit receipt — merged envelopes that are dropped
	// without signaling leave the caller blocked on <-out forever.
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}
	const n = 64
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(p int) {
			errs <- s.RecordHeartbeat(ctx, "srv-1", model.Heartbeat{Players: p, Load: 0.5})
		}(i)
	}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("heartbeat caller #%d still blocked — merged envelope not signaled", i)
		}
	}
	if stats := s.QueueStats(); stats.Merged == 0 {
		t.Log("no merge occurred this run (timing); unblocked-ness still verified")
	}
}

func TestQueueUncontendedWriteIsImmediate(t *testing.T) {
	// The merge window must not add a latency floor: a lone instruction on
	// an idle lane commits without waiting for the flush deadline.
	s := New()
	defer s.Close()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.RecordHeartbeat(ctx, "srv-1", model.Heartbeat{Players: 1, Load: 0.5})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("uncontended heartbeat waited longer than a full flush window")
	}
}
