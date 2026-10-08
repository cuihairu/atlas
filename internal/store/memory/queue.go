package memory

import (
	"context"
	"errors"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// 指令化写路径 (docs/performance.md §性能设计 — 指令化调度).
//
// Every hot write enters the store as an instruction envelope
// {kind, target, params, base_version, idempotency_key} instead of taking
// the write lock directly. Instructions are routed to per-entity lanes
// (hash(target): the same entity always lands in the same lane — FIFO,
// single writer, no races), and each lane's applier commits a whole batch
// inside ONE critical section where the data mutation and its index
// maintenance happen together (拍板① 同锁同临界区; ② 锁外构造、批内只做
// 指针/映射操作; 索引增量维护 O(1)/条, 禁全量重建).
//
// Merge rules are declared per kind (拍板②), not hidden in lock bodies:
// heartbeat / server-status / server-tags are last-writer-wins, so a batch
// coalesces same-kind same-target envelopes down to the newest. Deletes
// and character patches are order-sensitive and never merge. Priority
// (配置变更 > 心跳) is two lane classes — a config instruction never queues
// behind heartbeats (separate lanes, separate capacity), and hot-lane
// backlog cannot delay a config commit.
//
// Visibility & versions (拍板③): every applied instruction advances a
// monotonic watermark. base_version is stamped at enqueue; at apply, a
// mismatch means later instructions already landed — the current kind set
// is overwrite-commutative per entity, so it resolves by re-applying over
// current state (重算/幂等覆盖). A future compare-and-set kind would
// reject with errVersionConflict instead. An idempotency key deduplicates
// retries through a bounded LRU.
//
// Replay & dry-run (拍板④): applied instructions land in a bounded capture
// ring (payloads deep-copied); ReplayInto rebuilds a fresh store from it
// and DryRun applies it to a sandbox copy without touching the live store.
//
// Read path unchanged (拍板⑤): reads stay RLock + watermark checks; the
// instruction machinery is write-side only.

// Lane sizing. Deliberately constants — the design doc pins the shape;
// environment knobs would be a second axis nobody asked for.
const (
	controlLanes = 4
	hotLanes     = 2
	laneCap      = 4096 // pending instructions per lane before backpressure
	batchCap     = 256  // max instructions per committed batch

	idemCap    = 1024 // idempotency-key LRU
	captureCap = 1024 // replay capture ring
	flushRing  = 16   // recent flush records exposed via QueueStats
)

// errVersionConflict is reserved for future compare-and-set kinds: a
// base_version mismatch on a non-commutative operation must reject instead
// of re-apply. Every current kind is overwrite-commutative, so the mismatch
// path is 重算 (re-apply over current state) and this error is never
// returned today.
var errVersionConflict = errors.New("memory: base_version mismatch (instruction stream moved on)")

// opKind enumerates every instruction the queue understands. Each kind
// declares its lane class (hot()) and whether batches may coalesce it
// (mergeable()).
type opKind uint8

const (
	opRegisterServer opKind = iota
	opUpdateServerStatus
	opUpdateServerTags
	opDeleteServer
	opHeartbeat
	opDeleteRuntime
	opUpsertCharacter
	opUpdateCharacter
	opDeleteCharacter
	opKindCount
)

func (k opKind) String() string {
	switch k {
	case opRegisterServer:
		return "register_server"
	case opUpdateServerStatus:
		return "update_server_status"
	case opUpdateServerTags:
		return "update_server_tags"
	case opDeleteServer:
		return "delete_server"
	case opHeartbeat:
		return "heartbeat"
	case opDeleteRuntime:
		return "delete_runtime"
	case opUpsertCharacter:
		return "upsert_character"
	case opUpdateCharacter:
		return "update_character"
	case opDeleteCharacter:
		return "delete_character"
	}
	return "unknown"
}

// hot reports the lane class: heartbeats are the high-frequency writes and
// get their own lanes (and capacity) so a burst can never delay a config
// change stuck behind heartbeat backlog.
func (k opKind) hot() bool { return k == opHeartbeat }

// mergeable declares same-kind same-target coalescing inside a batch.
// Only last-writer-wins kinds qualify; anything whose result depends on
// applying every step (character patches) or on ordering against deletes
// must not. Kinds with a caller copyback (register, character upsert) are
// excluded too: a coalesced instruction would report the successor's state
// to its own caller.
func (k opKind) mergeable() bool {
	switch k {
	case opHeartbeat, opUpdateServerStatus, opUpdateServerTags:
		return true
	}
	return false
}

// instruction is the write envelope. Payload fields are prepared by the
// caller BEFORE enqueue (锁外构造): the applier's critical section only
// does map/index assignments.
type instruction struct {
	kind      opKind
	target    string // server ID, charKey, or runtime ID — the lane key
	hb        model.Heartbeat
	runtime   *model.Runtime
	server    *model.Server
	status    model.ServerStatus
	statusAt  time.Time
	tags      []model.ServerTag
	tagsAt    time.Time
	char      *model.Character
	charPatch *store.CharacterPatch
	charUpAt  time.Time

	baseVersion uint64 // watermark snapshot taken at enqueue
	idemKey     string // empty = always apply

	// out receives the per-instruction result; buffered so the applier
	// never blocks on a caller that stopped waiting (canceled context).
	// Capture/replay instructions carry nil.
	out chan instructionResult

	srvOut  *model.Server    // register copyback (caller sees the stored row)
	charOut *model.Character // upsert copyback
}

type instructionResult struct {
	err       error
	appliedAt uint64 // watermark after the batch committed
}

// captureEntry is the deep-copied, replayable form of an applied
// instruction. Payloads are cloned at capture time so a caller mutating
// its own objects after the call can never rewrite captured history.
type captureEntry struct {
	kind   opKind
	target string

	server    *model.Server
	status    model.ServerStatus
	statusAt  time.Time
	tags      []model.ServerTag
	tagsAt    time.Time
	hb        model.Heartbeat
	runtime   *model.Runtime
	char      *model.Character
	charPatch store.CharacterPatch
	hasPatch  bool
	charUpAt  time.Time
}

func (e captureEntry) instruction() *instruction {
	ins := &instruction{
		kind:     e.kind,
		target:   e.target,
		hb:       e.hb,
		runtime:  e.runtime,
		server:   e.server,
		status:   e.status,
		statusAt: e.statusAt,
		tags:     e.tags,
		tagsAt:   e.tagsAt,
		char:     e.char,
		charUpAt: e.charUpAt,
	}
	if e.hasPatch {
		ins.charPatch = &e.charPatch
	}
	return ins
}

// ReplayInto rebuilds the effect of src's capture ring on dst, in order —
// 排障回放: dump the ring, replay, and the sandbox reproduces the live
// write history. Idempotency keys are preserved, so replaying a stream
// that already ran against src is a no-op only on dst (fresh LRU), which
// is exactly the reproduction you want.
func ReplayInto(dst *Store, src *Store) error {
	src.captureMu.Lock()
	entries := make([]captureEntry, len(src.capture))
	copy(entries, src.capture)
	src.captureMu.Unlock()

	for i := range entries {
		dst.applyBatch(entries[i].instruction())
	}
	return nil
}

// DryRun replays the live capture ring onto a sandbox copy and reports
// what the write stream produced, without touching the live store (预检):
// applied-count per kind plus the sandbox's final watermark.
func DryRun(src *Store) (map[string]uint64, uint64, error) {
	sandbox := New()
	if err := ReplayInto(sandbox, src); err != nil {
		return nil, 0, err
	}
	stats := sandbox.QueueStats()
	return stats.AppliedByKind, stats.Watermark, nil
}

// startQueue spawns the lane appliers. Close shuts them down.
func (s *Store) startQueue() {
	s.ctrlCh = make([]chan *instruction, controlLanes)
	s.hotCh = make([]chan *instruction, hotLanes)
	for i := range s.ctrlCh {
		s.ctrlCh[i] = make(chan *instruction, laneCap)
	}
	for i := range s.hotCh {
		s.hotCh[i] = make(chan *instruction, laneCap)
	}
	for _, ch := range s.ctrlCh {
		s.wg.Add(1)
		go s.runLane(ch)
	}
	for _, ch := range s.hotCh {
		s.wg.Add(1)
		go s.runLane(ch)
	}
}

// submit routes an instruction to its lane and waits for it to commit —
// read-after-write stays exact. Queue-full degrades to an inline
// synchronous apply (backpressure: the caller becomes the single writer
// for its own instruction).
func (s *Store) submit(ctx context.Context, ins *instruction) instructionResult {
	s.enqueued.Add(1)
	lane := s.laneFor(ins)
	select {
	case lane <- ins:
	default:
		s.backpressureSync.Add(1)
		s.applyBatch(ins)
		return <-ins.out
	}
	if ctx == nil {
		// Legacy callers (and some tests) pass a nil context; the previous
		// implementation ignored ctx entirely, so wait unconditionally.
		return <-ins.out
	}
	select {
	case r := <-ins.out:
		return r
	case <-ctx.Done():
		// The instruction still applies (best effort); the caller just
		// stops waiting. Unblocked ≠ not applied — the watermark, not the
		// return, is the visibility contract.
		return instructionResult{err: ctx.Err()}
	}
}

func (s *Store) laneFor(ins *instruction) chan *instruction {
	if ins.kind.hot() {
		return s.hotCh[fnv32a(ins.target)%hotLanes]
	}
	return s.ctrlCh[fnv32a(ins.target)%controlLanes]
}

func fnv32a(s string) uint64 {
	h := uint64(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 16777619
	}
	return h
}

// runLane is the single writer for one lane. The merge window is
// availability-driven: keep draining whatever is already queued until the
// lane runs dry or the batch cap is reached, then commit. Under load the
// buffer stays non-empty and batches fill to the cap (coalesce + one
// critical section per 256 instructions); a lone instruction on an idle
// lane commits immediately. There is no timed window: throughput under
// load is bounded by the critical section, never by a flush interval, and
// an idle lane has no latency floor.
func (s *Store) runLane(ch <-chan *instruction) {
	defer s.wg.Done()
	for {
		first, ok := <-ch
		if !ok {
			return
		}
		s.applyBatchN(drainAvailable(ch, first))
	}
}

// drainAvailable keeps picking up whatever is queued right now. Commits
// happen on queue-empty (the common exit) or batchCap — never on a timer.
func drainAvailable(ch <-chan *instruction, first *instruction) []*instruction {
	batch := make([]*instruction, 1, batchCap)
	batch[0] = first
	for len(batch) < batchCap {
		select {
		case ins, ok := <-ch:
			if !ok {
				return batch
			}
			batch = append(batch, ins)
		default:
			return batch
		}
	}
	return batch
}

// applyBatch commits a single instruction (backpressure path).
func (s *Store) applyBatch(ins *instruction) { s.applyBatchN([]*instruction{ins}) }

// applyBatchN coalesces mergeable kinds, then commits the survivors in one
// critical section (index + data together), advances the watermark,
// records the flush, and signals every waiter.
func (s *Store) applyBatchN(batch []*instruction) {
	start := time.Now()

	// Coalesce: same kind + same target + mergeable → keep the newest
	// payload at the older slot (overwrite-commutative, position
	// irrelevant), drop the rest, count them. Superseded envelopes are
	// remembered: their callers are still waiting, and get the batch's
	// commit receipt — for last-writer-wins kinds a superseded write IS
	// durably visible once its successor commits.
	kept := make([]*instruction, 0, len(batch))
	var superseded []*instruction
	merged := 0
	for _, ins := range batch {
		if ins.kind.mergeable() {
			dup := false
			for i := len(kept) - 1; i >= 0; i-- {
				if kept[i].kind == ins.kind && kept[i].target == ins.target {
					superseded = append(superseded, kept[i])
					kept[i] = ins
					merged++
					dup = true
					break
				}
			}
			if dup {
				continue
			}
		}
		kept = append(kept, ins)
	}
	if merged > 0 {
		s.merged.Add(uint64(merged))
	}

	// One critical section for data + index (拍板①).
	s.mu.Lock()
	s.charMu.Lock()
	byKind := make(map[opKind]int, 1)
	lastApplied := uint64(0)
	for _, ins := range kept {
		if ins.idemKey != "" {
			if at, seen := s.idempotentSeen(ins.idemKey); seen {
				s.idempotentHits.Add(1)
				s.signal(ins, instructionResult{appliedAt: at})
				continue
			}
		}
		// base_version semantics (拍板③): current kinds are
		// overwrite-commutative under single-writer lanes, so a watermark
		// mismatch resolves by re-applying over current state. A future
		// CAS kind would compare ins.baseVersion here and reject with
		// errVersionConflict instead.
		res := s.applyOne(ins)
		if ins.idemKey != "" && res.err == nil {
			s.rememberIdempotent(ins.idemKey, res.appliedAt)
		}
		byKind[ins.kind]++
		s.captureAppend(ins)
		lastApplied = res.appliedAt
		s.signal(ins, res)
	}
	s.charMu.Unlock()
	s.mu.Unlock()

	// Superseded callers wake after the commit: any read they make sees
	// the batch's state (≥ their own write) — the watermark is the
	// visibility contract, not the per-instruction payload.
	for _, sup := range superseded {
		s.signal(sup, instructionResult{appliedAt: lastApplied})
	}

	took := time.Since(start)
	s.recordFlush(len(kept), merged, took)
	for k, n := range byKind {
		s.appliedByKind[k].Add(uint64(n))
	}
	s.applied.Add(uint64(len(kept)))
}

// signal wakes the waiter (never blocks: out is buffered size 1).
func (s *Store) signal(ins *instruction, res instructionResult) {
	if ins.out != nil {
		ins.out <- res
	}
}

// applyOne executes one instruction primitive against the locked store.
func (s *Store) applyOne(ins *instruction) instructionResult {
	at := s.watermark.Add(1)
	var err error
	switch ins.kind {
	case opRegisterServer:
		err = s.applyRegisterServer(ins)
	case opUpdateServerStatus:
		err = s.applyUpdateServerStatus(ins)
	case opUpdateServerTags:
		err = s.applyUpdateServerTags(ins)
	case opDeleteServer:
		err = s.applyDeleteServer(ins.target)
	case opHeartbeat:
		s.applyHeartbeat(ins.target, *ins.runtime)
	case opDeleteRuntime:
		err = s.applyDeleteRuntime(ins.target)
	case opUpsertCharacter:
		err = s.applyUpsertCharacter(ins)
	case opUpdateCharacter:
		err = s.applyUpdateCharacter(ins)
	case opDeleteCharacter:
		err = s.applyDeleteCharacter(ins.target)
	}
	if err != nil {
		return instructionResult{err: err, appliedAt: at}
	}
	return instructionResult{appliedAt: at}
}

// recordFlush appends a flush record for the status surface.
func (s *Store) recordFlush(size, merged int, took time.Duration) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	now := time.Now()
	s.lastFlushAt = now
	s.lastFlushBatch = size
	s.lastFlushDur = took
	s.recent = append(s.recent, store.FlushRecord{At: now, Size: size, Merged: merged, Duration: took})
	if len(s.recent) > flushRing {
		s.recent = s.recent[len(s.recent)-flushRing:]
	}
}

func (s *Store) idempotentSeen(key string) (uint64, bool) {
	s.idemMu.Lock()
	defer s.idemMu.Unlock()
	at, ok := s.idem[key]
	return at, ok
}

func (s *Store) rememberIdempotent(key string, at uint64) {
	s.idemMu.Lock()
	defer s.idemMu.Unlock()
	if _, ok := s.idem[key]; !ok {
		s.idemRing = append(s.idemRing, key)
		if len(s.idemRing) > idemCap {
			delete(s.idem, s.idemRing[0])
			s.idemRing = s.idemRing[1:]
		}
	}
	s.idem[key] = at
}

// captureAppend deep-copies the payload into the replay ring (bounded).
func (s *Store) captureAppend(ins *instruction) {
	e := captureEntry{
		kind:     ins.kind,
		target:   ins.target,
		status:   ins.status,
		statusAt: ins.statusAt,
		tagsAt:   ins.tagsAt,
		hb:       ins.hb,
		charUpAt: ins.charUpAt,
	}
	switch ins.kind {
	case opRegisterServer:
		cp := *ins.server
		e.server = &cp
	case opUpdateServerTags:
		e.tags = cloneTags(ins.tags)
	case opHeartbeat:
		rt := *ins.runtime
		e.runtime = &rt
	case opUpsertCharacter:
		e.char = cloneCharacter(ins.char)
	case opUpdateCharacter:
		e.charPatch = *ins.charPatch
		e.hasPatch = true
	}
	s.captureMu.Lock()
	defer s.captureMu.Unlock()
	if len(s.capture) >= captureCap {
		s.capture = s.capture[1:]
	}
	s.capture = append(s.capture, e)
}

func cloneTags(tags []model.ServerTag) []model.ServerTag {
	if tags == nil {
		return nil
	}
	cp := make([]model.ServerTag, len(tags))
	copy(cp, tags)
	return cp
}

func cloneCharacter(ch *model.Character) *model.Character {
	if ch == nil {
		return nil
	}
	cp := *ch
	if ch.Metadata != nil {
		cp.Metadata = make(map[string]string, len(ch.Metadata))
		for k, v := range ch.Metadata {
			cp.Metadata[k] = v
		}
	}
	if ch.LastLoginAt != nil {
		t := *ch.LastLoginAt
		cp.LastLoginAt = &t
	}
	return &cp
}

// QueueStats implements store.QueueStatusProvider — the single snapshot
// behind both the Prometheus collector and the admin status endpoint.
func (s *Store) QueueStats() store.QueueStats {
	depthControl, depthHot := 0, 0
	for _, ch := range s.ctrlCh {
		depthControl += len(ch)
	}
	for _, ch := range s.hotCh {
		depthHot += len(ch)
	}

	s.flushMu.Lock()
	recent := make([]store.FlushRecord, len(s.recent))
	copy(recent, s.recent)
	lastAt, lastBatch, lastDur := s.lastFlushAt, s.lastFlushBatch, s.lastFlushDur
	s.flushMu.Unlock()

	byKind := make(map[string]uint64, opKindCount)
	for k := opKind(0); k < opKindCount; k++ {
		if n := s.appliedByKind[k].Load(); n > 0 {
			byKind[k.String()] = n
		}
	}

	s.captureMu.Lock()
	captureLen := len(s.capture)
	s.captureMu.Unlock()

	return store.QueueStats{
		Enabled:           true,
		Watermark:         s.watermark.Load(),
		LanesControl:      controlLanes,
		LanesHot:          hotLanes,
		DepthControl:      depthControl,
		DepthHot:          depthHot,
		Enqueued:          s.enqueued.Load(),
		Merged:            s.merged.Load(),
		Applied:           s.applied.Load(),
		IdempotentHits:    s.idempotentHits.Load(),
		BackpressureSync:  s.backpressureSync.Load(),
		LaneCap:           laneCap,
		BatchCap:          batchCap,
		LastFlush:         lastAt,
		LastFlushBatch:    lastBatch,
		LastFlushDuration: lastDur,
		RecentFlushes:     recent,
		AppliedByKind:     byKind,
		CaptureLen:        captureLen,
	}
}

// Close drains and stops the lane appliers.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		for _, ch := range s.ctrlCh {
			close(ch)
		}
		for _, ch := range s.hotCh {
			close(ch)
		}
		s.wg.Wait()
	})
	return nil
}
