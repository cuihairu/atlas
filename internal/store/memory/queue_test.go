package memory

// Instruction-queue semantics (docs/performance.md §性能设计 — 指令化调度):
// merge rules, per-entity ordering, idempotency, replay/dry-run, backpressure
// and the watermark. Batch-level tests drive applyBatchN directly for
// determinism; the public API path is exercised by the contract suite.

import (
	"context"
	"sync"
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

	// Swap in a fresh, unconsumed hot lane with capacity 1 — the live lane
	// goroutines keep serving the old channels, so nothing consumes this
	// one (and Close is not used: post-Close submits take the inline path
	// by design, see TestSubmitAfterCloseInline).
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

// TestCloseConcurrentWithSubmit pins the shutdown race: a submit that loses
// the race against Close must degrade to the inline apply path, never send
// on a closed lane (send-on-closed-channel panic). Run with -race.
func TestCloseConcurrentWithSubmit(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		s := New()
		ctx := context.Background()
		if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		stop := make(chan struct{})
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for j := 0; ; j++ {
					select {
					case <-stop:
						return
					default:
					}
					ins := hbInstr("srv-1", j%7)
					_ = s.submit(ctx, ins)
				}
			}(w)
		}

		s.Close()
		// Submits racing the close (or arriving after it) must still
		// complete via the inline path — and must not panic.
		close(stop)
		wg.Wait()
	}
}

// TestSubmitAfterCloseInline pins post-Close semantics: with the lanes
// closed, submits take the inline synchronous path instead of sending on a
// closed lane — writes keep applying (memory stores serve reads after
// close), and Close stays idempotent.
func TestSubmitAfterCloseInline(t *testing.T) {
	s := New()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if err := s.RecordHeartbeat(ctx, "srv-1", model.Heartbeat{Players: 3}); err != nil {
		t.Fatalf("post-close submit = %v, want inline apply", err)
	}
	rt, err := s.GetRuntime(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Players != 3 {
		t.Errorf("players = %d, want 3 (post-close inline write visible)", rt.Players)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close = %v, want nil (idempotent)", err)
	}
}

// waitPickedUp blocks until exactly one more instruction has been enqueued
// and its lane is empty again — the applier holds it and is blocked on the
// store mutex the test holds, so it is queued-but-uncommittable.
func waitPickedUp(t *testing.T, s *Store, pickedUp func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pickedUp() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("lane applier did not pick up the buffered instruction in time")
}

// waitClosed blocks until a concurrent Close has linearized (s.closed set
// under closeMu) — every submit from here on deterministically observes
// the closed store.
func waitClosed(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.closeMu.Lock()
		closed := s.closed
		s.closeMu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("store did not observe the close in time")
}

// TestCloseDrainPrecedesPostCloseWrite pins the ordering half of the close
// contract: an instruction that landed in a lane before Close must commit
// before a write submitted after the close. The rtMu blocker parks the lane
// applier mid-commit so the queued heartbeat stays in the channel; the
// flushMu hold then parks it between batches (recordFlush runs after the
// mutex release but before the next dequeue) — the exact window where the
// unfixed inline path takes the free mutex and overtakes the queued write.
// The fixed path is deterministic: the post-close submit waits for the whole
// drain before applying.
func TestCloseDrainPrecedesPostCloseWrite(t *testing.T) {
	s := New()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r"}); err != nil {
		t.Fatal(err)
	}

	// Park the hot applier mid-commit: it dequeues the blocker heartbeat
	// and blocks on rtMu inside applyHeartbeat (holding the store mutex).
	s.rtMu.Lock()
	before := s.QueueStats().Enqueued
	blocker := make(chan instructionResult, 1)
	go func() { blocker <- s.submit(ctx, hbInstr("srv-1", 9)) }()
	waitPickedUp(t, s, func() bool {
		st := s.QueueStats()
		return st.Enqueued == before+1 && st.DepthHot == 0
	})

	// The queued heartbeat parks in the lane channel behind the blocker.
	before2 := s.QueueStats().Enqueued
	buffered := make(chan instructionResult, 1)
	go func() { buffered <- s.submit(ctx, hbInstr("srv-1", 1)) }()
	waitPickedUp(t, s, func() bool {
		st := s.QueueStats()
		return st.Enqueued == before2+1 && st.DepthHot == 1
	})

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitClosed(t, s)

	// Every submit from here on observes the closed store: the fixed path
	// waits for the drain (blocker + queued heartbeat) before applying,
	// the unfixed one goes inline and parks on the store mutex.
	post := make(chan instructionResult, 1)
	go func() { post <- s.submit(ctx, hbInstr("srv-1", 2)) }()

	// Release rtMu: the applier finishes the blocker commit and parks in
	// recordFlush. While flushMu is held the queued heartbeat cannot be
	// dequeued, so anything the unfixed inline path commits in this
	// window overtakes it. The fixed path cannot use the window at all
	// (the post-close submit is still waiting for the drain).
	s.flushMu.Lock()
	s.rtMu.Unlock()
	time.Sleep(250 * time.Millisecond)
	s.flushMu.Unlock()

	bres, pres, blres := <-buffered, <-post, <-blocker
	if bres.err != nil || pres.err != nil || blres.err != nil {
		t.Fatalf("submit errors: blocker=%v buffered=%v post=%v", blres.err, bres.err, pres.err)
	}
	if !(blres.appliedAt < bres.appliedAt && bres.appliedAt < pres.appliedAt) {
		t.Errorf("commit order broken: blocker %d, queued %d, post-close %d (want strictly increasing — post-close write overtook a queued one)",
			blres.appliedAt, bres.appliedAt, pres.appliedAt)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close = %v", err)
	}
	rt, err := s.GetRuntime(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Players != 2 {
		t.Errorf("players = %d, want 2 (newest write wins, not the queued older one)", rt.Players)
	}
}

// TestCloseDrainOrderCAS pins the same invariant for the compare-and-set
// kind: the queued CAS must commit while its precondition still holds — a
// post-close status write may not overtake it and make it fail against a
// state it never queued against. The DeleteRuntime blocker parks the
// control-lane applier on rtMu (applyDeleteRuntime) so the CAS stays in the
// channel; the flushMu hold then parks it between batches while the
// unfixed inline path lands the post-close status write first and flips
// the CAS verdict.
func TestCloseDrainOrderCAS(t *testing.T) {
	s := New()
	ctx := context.Background()
	if err := s.RegisterServer(ctx, &model.Server{ID: "srv-1", Name: "one", Region: "r", Status: model.StatusOnline}); err != nil {
		t.Fatal(err)
	}

	// Park the control applier mid-commit inside applyDeleteRuntime's rtMu
	// critical section (the runtime does not exist — the blocker's error
	// result is discarded; parking is the point).
	s.rtMu.Lock()
	before := s.QueueStats().Enqueued
	blocker := make(chan error, 1)
	go func() { blocker <- s.DeleteRuntime(ctx, "rt-none") }()
	waitPickedUp(t, s, func() bool {
		st := s.QueueStats()
		return st.Enqueued == before+1 && st.DepthControl == 0
	})

	// The CAS parks in the control channel behind the blocker.
	before2 := s.QueueStats().Enqueued
	matched := make(chan bool, 1)
	go func() {
		ok, err := s.UpdateServerStatusIf(ctx, "srv-1", model.StatusOnline, model.StatusDraining)
		if err != nil {
			t.Errorf("CAS error: %v", err)
		}
		matched <- ok
	}()
	waitPickedUp(t, s, func() bool {
		st := s.QueueStats()
		return st.Enqueued == before2+1 && st.DepthControl == 1
	})

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitClosed(t, s)

	post := make(chan error, 1)
	go func() { post <- s.UpdateServerStatus(ctx, "srv-1", model.StatusOffline) }()

	// While flushMu is held the queued CAS cannot be dequeued — the same
	// overtake window as the heartbeat test above.
	s.flushMu.Lock()
	s.rtMu.Unlock()
	time.Sleep(250 * time.Millisecond)
	s.flushMu.Unlock()

	if ok := <-matched; !ok {
		t.Errorf("CAS matched = false, want true (post-close write overtook the queued CAS)")
	}
	if err := <-post; err != nil {
		t.Fatalf("post-close status write = %v", err)
	}
	if err := <-blocker; err != nil {
		t.Logf("blocker DeleteRuntime = %v (expected: runtime not found)", err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close = %v", err)
	}
	srv, err := s.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Status != model.StatusOffline {
		t.Errorf("status = %q, want offline (post-close write lands after the CAS)", srv.Status)
	}
}
