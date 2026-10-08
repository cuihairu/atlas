// Package memory provides a pure-Go, in-memory implementation of store.Store.
//
// It is safe for concurrent use. This implementation requires no external
// dependencies and is useful for testing and development.
//
// Concurrency shape (docs/performance.md §性能设计): three lock domains —
//
//   - s.mu:      servers map + the serverIndex (region/realm/shard/version/
//     platform/status/public-tags inverted indexes)
//   - s.charMu:  characters map + the charIndex (byAccount / byServer /
//     byAccountServer / byCharID inverted indexes)
//   - s.rtMu:    runtimes map structure
//
// Lock order is mu → charMu → rtMu; no path acquires them in reverse.
// Hot writes do not take these locks directly — they enter the instruction
// queue (queue.go), whose appliers commit data + index in one critical
// section per batch. Admin-cold writes (realms, shards, migrations,
// maintenance windows, announcements, cross-server config) stay on the
// classic direct-lock path: their frequency cannot contend, and wrapping
// them would only add latency.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// compile-time interface check
var _ store.Store = (*Store)(nil)

// compile-time check: the memory store exposes the instruction-queue
// status surface (admin endpoint + Prometheus collector).
var _ store.QueueStatusProvider = (*Store)(nil)

// Store is an in-memory implementation of store.Store.
type Store struct {
	mu     sync.RWMutex // servers + serverIndex
	charMu sync.RWMutex // characters + charIndex
	rtMu   sync.RWMutex // runtimes map structure

	servers    map[string]*model.Server
	characters map[string]*model.Character // key: "accountID:serverID:characterID"
	runtimes   map[string]model.Runtime
	migrations map[string]*model.Migration
	realms     map[string]*model.Realm
	shards     map[string]*model.Shard

	srvIdx *serverIndex
	chIdx  *charIndex

	maintWindows  map[string]*model.MaintenanceWindow
	announcements map[string]*model.Announcement
	crossConfig   *model.CrossServerConfig

	// ── instruction queue state (queue.go) ──────────────────────────
	ctrlCh    []chan *instruction
	hotCh     []chan *instruction
	wg        sync.WaitGroup
	closeOnce sync.Once

	watermark        atomic.Uint64 // last applied instruction sequence
	enqueued         atomic.Uint64
	merged           atomic.Uint64
	applied          atomic.Uint64
	idempotentHits   atomic.Uint64
	backpressureSync atomic.Uint64
	appliedByKind    [opKindCount]atomic.Uint64

	flushMu        sync.Mutex
	lastFlushAt    time.Time
	lastFlushBatch int
	lastFlushDur   time.Duration
	recent         []store.FlushRecord

	idemMu   sync.Mutex
	idem     map[string]uint64
	idemRing []string

	captureMu sync.Mutex
	capture   []captureEntry
}

// New creates a new in-memory store.
func New() *Store {
	s := &Store{
		servers:       make(map[string]*model.Server),
		characters:    make(map[string]*model.Character),
		runtimes:      make(map[string]model.Runtime),
		migrations:    make(map[string]*model.Migration),
		realms:        make(map[string]*model.Realm),
		shards:        make(map[string]*model.Shard),
		maintWindows:  make(map[string]*model.MaintenanceWindow),
		announcements: make(map[string]*model.Announcement),
		srvIdx:        newServerIndex(),
		chIdx:         newCharIndex(),
		idem:          make(map[string]uint64),
	}
	s.startQueue()
	return s
}

// Ping always succeeds for the in-memory store.
func (s *Store) Ping(_ context.Context) error {
	return nil
}

// ---------------------------------------------------------------------------
// ServerStore — writes go through the instruction queue
// ---------------------------------------------------------------------------

func (s *Store) RegisterServer(ctx context.Context, srv *model.Server) error {
	ins := &instruction{
		kind:        opRegisterServer,
		target:      srv.ID,
		server:      srv,
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) GetServer(_ context.Context, id string) (*model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	srv, ok := s.servers[id]
	if !ok {
		return nil, fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	cp := *srv
	return &cp, nil
}

func (s *Store) ListServers(_ context.Context, f store.ServerFilter) ([]*model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > store.ListServersMaxLimit {
		limit = store.ListServersMaxLimit
	}

	// Index-driven candidate set: the smallest inverted-index bucket among
	// the filter's dimensions. With no indexed filter at all, scan the
	// table directly — materializing the full key set first would be pure
	// overhead. IDs sort for deterministic pagination; semantics identical
	// to the pre-index scan, minus the O(N) row visits on filtered queries.
	ids := s.srvIdx.candidates(f)
	if ids == nil {
		for id, srv := range s.servers {
			if matchServer(srv, f) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)

	start := 0
	if f.Cursor != "" {
		// First index with ID >= cursor; skip the cursor itself (entries
		// with ID <= cursor are excluded).
		start = sort.SearchStrings(ids, f.Cursor)
		if start < len(ids) && ids[start] == f.Cursor {
			start++
		}
	}

	var result []*model.Server
	for _, id := range ids[start:] {
		if len(result) >= limit {
			break
		}
		srv := s.servers[id]
		// Belt and braces: verify the full filter on each candidate so an
		// index-selection bug can never surface as a wrong row.
		if !matchServer(srv, f) {
			continue
		}
		cp := *srv
		result = append(result, &cp)
	}
	return result, nil
}

func (s *Store) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	ins := &instruction{
		kind:        opUpdateServerStatus,
		target:      id,
		status:      status,
		statusAt:    time.Now(),
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) UpdateServerTags(ctx context.Context, id string, tags []model.ServerTag) error {
	// Replace, never mutate in place: readers hold shallow copies of the
	// struct and would otherwise race on the shared backing array. The
	// copy is made outside the queue's critical section (锁外构造).
	var cp []model.ServerTag
	if len(tags) > 0 {
		cp = make([]model.ServerTag, len(tags))
		copy(cp, tags)
	}
	ins := &instruction{
		kind:        opUpdateServerTags,
		target:      id,
		tags:        cp,
		tagsAt:      time.Now(),
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) DeleteServer(ctx context.Context, id string) error {
	ins := &instruction{
		kind:        opDeleteServer,
		target:      id,
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

// ── queue-side primitives (locks held by the applier) ──────────────────

func (s *Store) applyRegisterServer(ins *instruction) error {
	srv := ins.server
	now := time.Now()
	if existing, ok := s.servers[srv.ID]; ok {
		// Idempotent update: overwrite mutable fields. Index maintenance is
		// remove(old projection) + add(new projection) — O(1), same
		// critical section as the data (拍板①/②).
		old := projectServer(existing)
		existing.Name = srv.Name
		existing.Type = srv.Type
		existing.Region = srv.Region
		existing.RealmID = srv.RealmID
		existing.ShardID = srv.ShardID
		existing.Version = srv.Version
		existing.Platform = srv.Platform
		existing.Endpoint = srv.Endpoint
		existing.Capacity = srv.Capacity
		existing.Source = srv.Source
		existing.NotifyMode = srv.NotifyMode
		existing.NotifyCallbackURL = srv.NotifyCallbackURL
		existing.Metadata = srv.Metadata
		existing.StartedAt = srv.StartedAt
		// A fresh registration proves a (re)boot: reset only a dead-ish
		// lifecycle (suspect / offline) so the next heartbeat can promote it
		// again. Keep active and operator-set statuses untouched.
		if existing.Status == model.StatusSuspect || existing.Status == model.StatusOffline {
			existing.Status = srv.Status
		}
		existing.UpdatedAt = now
		// Copy timestamps back so caller sees the stored record.
		*srv = *existing
		s.srvIdx.remove(srv.ID, old)
		s.srvIdx.add(srv.ID, projectServer(existing))
		return nil
	}

	if srv.Status == "" {
		srv.Status = model.StatusStarting
	}
	srv.CreatedAt = now
	srv.UpdatedAt = now

	// Store a copy to prevent mutation.
	cp := *srv
	s.servers[srv.ID] = &cp
	s.srvIdx.add(srv.ID, projectServer(&cp))
	return nil
}

func (s *Store) applyUpdateServerStatus(ins *instruction) error {
	srv, ok := s.servers[ins.target]
	if !ok {
		return fmt.Errorf("server %s: %w", ins.target, store.ErrNotFound)
	}
	old := projectServer(srv)
	srv.Status = ins.status
	srv.UpdatedAt = ins.statusAt
	s.srvIdx.remove(ins.target, old)
	s.srvIdx.add(ins.target, projectServer(srv))
	return nil
}

func (s *Store) applyUpdateServerTags(ins *instruction) error {
	srv, ok := s.servers[ins.target]
	if !ok {
		return fmt.Errorf("server %s: %w", ins.target, store.ErrNotFound)
	}
	old := projectServer(srv)
	srv.Tags = ins.tags
	srv.UpdatedAt = ins.tagsAt
	s.srvIdx.remove(ins.target, old)
	s.srvIdx.add(ins.target, projectServer(srv))
	return nil
}

func (s *Store) applyDeleteServer(id string) error {
	srv, ok := s.servers[id]
	if !ok {
		return fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	s.srvIdx.remove(id, projectServer(srv))
	delete(s.servers, id)
	return nil
}

// ---------------------------------------------------------------------------
// CharacterStore — writes go through the instruction queue
// ---------------------------------------------------------------------------

func charKey(accountID int64, serverID string, characterID int64) string {
	return fmt.Sprintf("%d:%s:%d", accountID, serverID, characterID)
}

func (s *Store) UpsertCharacter(ctx context.Context, ch *model.Character) error {
	ins := &instruction{
		kind:        opUpsertCharacter,
		target:      charKey(ch.AccountID, ch.ServerID, ch.CharacterID),
		char:        ch,
		charUpAt:    time.Now(),
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) GetCharacter(_ context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	key := charKey(accountID, serverID, characterID)
	ch, ok := s.characters[key]
	if !ok {
		return nil, fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	cp := *ch
	return &cp, nil
}

func (s *Store) GetCharacterByCharacterID(_ context.Context, characterID int64) (*model.Character, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	// O(1) through the byCharID inverted index (was a full-table scan).
	key, ok := s.chIdx.byCharID[characterID]
	if !ok {
		return nil, fmt.Errorf("character_id %d: %w", characterID, store.ErrNotFound)
	}
	ch := s.characters[key]
	cp := *ch
	return &cp, nil
}

func (s *Store) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	ins := &instruction{
		kind:        opUpdateCharacter,
		target:      charKey(accountID, serverID, characterID),
		charPatch:   &patch,
		charUpAt:    time.Now(),
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	ins := &instruction{
		kind:        opDeleteCharacter,
		target:      charKey(accountID, serverID, characterID),
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) ListCharactersByAccount(_ context.Context, accountID int64) ([]*model.Character, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	// byAccount inverted index: O(k) in the account's own rows (was a full
	// prefix-scan over the whole table). Ordering contract unchanged:
	// (server_id, character_id).
	keys := s.chIdx.byAccount[accountID]
	var result []*model.Character
	for key := range keys {
		ch := s.characters[key]
		cp := *ch
		result = append(result, &cp)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].ServerID != result[j].ServerID {
			return result[i].ServerID < result[j].ServerID
		}
		return result[i].CharacterID < result[j].CharacterID
	})

	return result, nil
}

func (s *Store) ListCharactersByServer(_ context.Context, serverID string, limit int, cursor string) ([]*model.Character, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// byServer inverted index (was a full-table scan).
	keys := s.chIdx.byServer[serverID]
	var result []*model.Character
	for key := range keys {
		ch := s.characters[key]
		cp := *ch
		result = append(result, &cp)
	}

	// Sort by character ID for stable pagination.
	sort.Slice(result, func(i, j int) bool {
		return result[i].CharacterID < result[j].CharacterID
	})

	// Apply cursor.
	if cursor != "" {
		cursorID, err := strconv.ParseInt(cursor, 10, 64)
		if err == nil {
			start := 0
			for i, ch := range result {
				if ch.CharacterID > cursorID {
					start = i
					break
				}
				start = i + 1
			}
			result = result[start:]
		}
	}

	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// ── queue-side primitives (locks held by the applier) ──────────────────

func (s *Store) applyUpsertCharacter(ins *instruction) error {
	ch := ins.char
	key := ins.target
	now := ins.charUpAt

	if existing, ok := s.characters[key]; ok {
		// The composite key is immutable per row, so no index maintenance
		// is needed on the update path — that is what keeps the upsert hot
		// path O(1) per instruction.
		existing.Name = ch.Name
		existing.Level = ch.Level
		existing.ClassID = ch.ClassID
		existing.Avatar = ch.Avatar
		existing.Metadata = ch.Metadata
		existing.LastLoginAt = ch.LastLoginAt
		existing.UpdatedAt = now
		*ch = *existing
		return nil
	}

	ch.CreatedAt = now
	ch.UpdatedAt = now
	cp := *ch
	s.characters[key] = &cp
	s.chIdx.add(key, &cp)
	return nil
}

func (s *Store) applyUpdateCharacter(ins *instruction) error {
	patch := ins.charPatch
	ch, ok := s.characters[ins.target]
	if !ok {
		return fmt.Errorf("character %s: %w", ins.target, store.ErrNotFound)
	}

	if patch.Name != nil {
		ch.Name = *patch.Name
	}
	if patch.Level != nil {
		ch.Level = *patch.Level
	}
	if patch.ClassID != nil {
		ch.ClassID = *patch.ClassID
	}
	if patch.Avatar != nil {
		ch.Avatar = *patch.Avatar
	}
	if patch.Metadata != nil {
		ch.Metadata = *patch.Metadata
	}
	if patch.LastLoginAt != nil {
		ch.LastLoginAt = patch.LastLoginAt
	}
	ch.UpdatedAt = ins.charUpAt
	return nil
}

func (s *Store) applyDeleteCharacter(key string) error {
	ch, ok := s.characters[key]
	if !ok {
		return fmt.Errorf("character %s: %w", key, store.ErrNotFound)
	}
	s.chIdx.remove(key, ch)
	delete(s.characters, key)
	return nil
}

// ---------------------------------------------------------------------------
// RuntimeStore — heartbeats ride the hot lane
// ---------------------------------------------------------------------------

func (s *Store) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	// Build the runtime snapshot outside any lock (锁外构造); the applier
	// only assigns it into the map.
	rt := model.Runtime{
		Status:     hb.Status,
		Players:    hb.Players,
		Load:       hb.Load,
		LastSeenAt: time.Now(),
	}
	ins := &instruction{
		kind:        opHeartbeat,
		target:      id,
		runtime:     &rt,
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) GetRuntime(_ context.Context, id string) (*model.Runtime, error) {
	s.rtMu.RLock()
	defer s.rtMu.RUnlock()

	rt, ok := s.runtimes[id]
	if !ok {
		return nil, fmt.Errorf("runtime %s: %w", id, store.ErrNotFound)
	}
	return &rt, nil
}

// GetRuntimes batches the lookup in one pass under a single read lock.
// Missing snapshots are simply absent from the result.
func (s *Store) GetRuntimes(_ context.Context, ids []string) (map[string]model.Runtime, error) {
	s.rtMu.RLock()
	defer s.rtMu.RUnlock()

	out := make(map[string]model.Runtime, len(ids))
	for _, id := range ids {
		if rt, ok := s.runtimes[id]; ok {
			out[id] = rt
		}
	}
	return out, nil
}

func (s *Store) ListRuntimes(_ context.Context) (map[string]model.Runtime, error) {
	s.rtMu.RLock()
	defer s.rtMu.RUnlock()

	out := make(map[string]model.Runtime, len(s.runtimes))
	for id, rt := range s.runtimes {
		out[id] = rt
	}
	return out, nil
}

func (s *Store) DeleteRuntime(ctx context.Context, id string) error {
	ins := &instruction{
		kind:        opDeleteRuntime,
		target:      id,
		baseVersion: s.watermark.Load(),
		out:         make(chan instructionResult, 1),
	}
	return s.submit(ctx, ins).err
}

func (s *Store) applyHeartbeat(id string, rt model.Runtime) {
	s.rtMu.Lock()
	s.runtimes[id] = rt
	s.rtMu.Unlock()
}

func (s *Store) applyDeleteRuntime(id string) error {
	s.rtMu.Lock()
	defer s.rtMu.Unlock()
	if _, ok := s.runtimes[id]; !ok {
		return fmt.Errorf("runtime %s: %w", id, store.ErrNotFound)
	}
	delete(s.runtimes, id)
	return nil
}

// ---------------------------------------------------------------------------
// SearchCharacters
// ---------------------------------------------------------------------------

func (s *Store) SearchCharacters(_ context.Context, filter store.CharacterSearchFilter) ([]*model.Character, string, error) {
	s.charMu.RLock()
	defer s.charMu.RUnlock()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Index prefilter: account and/or server narrow through the charIndex;
	// only the non-indexable predicates (name contains, metadata pair,
	// level range) still walk the candidate set. No filter at all scans
	// the table — inherent for substring name search.
	var keys map[string]struct{}
	switch {
	case filter.AccountID != 0 && filter.ServerID != "":
		keys = s.chIdx.byAccountServer[filter.AccountID][filter.ServerID]
	case filter.AccountID != 0:
		keys = s.chIdx.byAccount[filter.AccountID]
	case filter.ServerID != "":
		keys = s.chIdx.byServer[filter.ServerID]
	}

	var result []*model.Character
	if keys != nil {
		for key := range keys {
			ch := s.characters[key]
			if !matchCharacter(ch, filter) {
				continue
			}
			cp := *ch
			result = append(result, &cp)
		}
	} else {
		for _, ch := range s.characters {
			if !matchCharacter(ch, filter) {
				continue
			}
			cp := *ch
			result = append(result, &cp)
		}
	}

	// Sort by (server_id, character_id) for stable pagination.
	sort.Slice(result, func(i, j int) bool {
		if result[i].ServerID != result[j].ServerID {
			return result[i].ServerID < result[j].ServerID
		}
		return result[i].CharacterID < result[j].CharacterID
	})

	// Apply cursor: skip entries at or before the cursor. The comparison must
	// mirror the sort order above — server ID lexicographically, character ID
	// numerically — matching the typed tuple comparison in postgres.
	if filter.Cursor != "" {
		curServer, curCharID := splitSearchCursor(filter.Cursor)
		start := 0
		for i, ch := range result {
			if ch.ServerID > curServer || (ch.ServerID == curServer && ch.CharacterID > curCharID) {
				start = i
				break
			}
			start = i + 1
		}
		result = result[start:]
	}

	nextCursor := ""
	if len(result) > limit {
		result = result[:limit]
	}
	if len(result) > 0 {
		last := result[len(result)-1]
		nextCursor = last.ServerID + ":" + strconv.FormatInt(last.CharacterID, 10)
	}

	return result, nextCursor, nil
}

// ---------------------------------------------------------------------------
// MigrationStore — admin-cold, direct lock
// ---------------------------------------------------------------------------

func (s *Store) CreateMigration(_ context.Context, m *model.Migration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy.
	cp := *m
	cp.SourceServers = make([]string, len(m.SourceServers))
	copy(cp.SourceServers, m.SourceServers)
	s.migrations[m.ID] = &cp
	return nil
}

func (s *Store) GetMigration(_ context.Context, id string) (*model.Migration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, ok := s.migrations[id]
	if !ok {
		return nil, fmt.Errorf("migration %s: %w", id, store.ErrNotFound)
	}
	cp := *m
	cp.SourceServers = make([]string, len(m.SourceServers))
	copy(cp.SourceServers, m.SourceServers)
	return &cp, nil
}

func (s *Store) UpdateMigrationStatus(_ context.Context, id string, status model.MigrationStatus, completedAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.migrations[id]
	if !ok {
		return fmt.Errorf("migration %s: %w", id, store.ErrNotFound)
	}
	m.Status = status
	m.CompletedAt = completedAt
	return nil
}

func (s *Store) ListMigrations(_ context.Context, limit int) ([]*model.Migration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var result []*model.Migration
	for _, m := range s.migrations {
		cp := *m
		cp.SourceServers = make([]string, len(m.SourceServers))
		copy(cp.SourceServers, m.SourceServers)
		result = append(result, &cp)
	}

	// Sort by StartedAt descending.
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.After(result[j].StartedAt)
	})

	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// StatsStore
// ---------------------------------------------------------------------------

func (s *Store) GetStats(_ context.Context) (*model.Stats, error) {
	s.mu.RLock()
	s.charMu.RLock()
	s.rtMu.RLock()
	defer s.rtMu.RUnlock()
	defer s.charMu.RUnlock()
	defer s.mu.RUnlock()

	stats := &model.Stats{
		ServersByStatus:  make(map[string]int),
		ServersByRegion:  make(map[string]int),
		ServersByVersion: make(map[string]int),
	}

	for _, srv := range s.servers {
		stats.TotalServers++
		stats.ServersByStatus[string(srv.Status)]++
		stats.ServersByRegion[srv.Region]++
		stats.ServersByVersion[srv.Version]++
		stats.TotalCapacity += srv.Capacity
	}

	for _, rt := range s.runtimes {
		stats.TotalPlayers += rt.Players
	}

	stats.TotalCharacters = len(s.characters)
	stats.Finalize()

	return stats, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func matchCharacter(ch *model.Character, f store.CharacterSearchFilter) bool {
	if f.Name != "" && !strings.Contains(strings.ToLower(ch.Name), strings.ToLower(f.Name)) {
		return false
	}
	if f.ServerID != "" && ch.ServerID != f.ServerID {
		return false
	}
	if f.AccountID != 0 && ch.AccountID != f.AccountID {
		return false
	}
	if f.MetadataKey != "" && ch.Metadata[f.MetadataKey] != f.MetadataValue {
		return false
	}
	if f.MinLevel != nil && ch.Level < *f.MinLevel {
		return false
	}
	if f.MaxLevel != nil && ch.Level > *f.MaxLevel {
		return false
	}
	return true
}

func matchServer(srv *model.Server, f store.ServerFilter) bool {
	if f.Region != "" && srv.Region != f.Region {
		return false
	}
	if f.Realm != "" {
		if srv.RealmID == nil || *srv.RealmID != f.Realm {
			return false
		}
	}
	if f.Shard != "" {
		if srv.ShardID == nil || *srv.ShardID != f.Shard {
			return false
		}
	}
	if f.Version != "" && srv.Version != f.Version {
		return false
	}
	if f.Platform != "" && srv.Platform != f.Platform {
		return false
	}
	if f.Status != "" && srv.Status != f.Status {
		return false
	}
	// 玩法直查 tag filter: AND semantics over PUBLIC tag codes only —
	// internal tags never match (mirror of model.PublicTags and of the
	// srvIdx.tags bucket, which indexes public codes exclusively).
	if len(f.Tags) > 0 {
		for _, want := range f.Tags {
			found := false
			for _, t := range srv.Tags {
				if t.Public && t.Code == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// splitSearchCursor parses the "serverID:characterID" search cursor the same
// way postgres does (typed: character ID as int64).
func splitSearchCursor(cursor string) (string, int64) {
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) == 2 {
		if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			return parts[0], id
		}
	}
	return "", 0
}

// ── Realms & Shards (TODO v0.1.14) — admin-cold, direct lock ─────

func (s *Store) CreateRealm(_ context.Context, r *model.Realm) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.realms[r.ID]; ok {
		return fmt.Errorf("realm %s: %w", r.ID, store.ErrConflict)
	}
	// SQL stores default the column to 'active'; mirror it so the stores
	// cannot drift on freshly created realms.
	if r.Status == "" {
		r.Status = "active"
	}
	// Stamp on the caller's object too (parity with the SQL stores) so the
	// HTTP response carries the real created_at instead of a zero time.
	r.CreatedAt = time.Now()
	cp := *r
	s.realms[r.ID] = &cp
	return nil
}

func (s *Store) GetRealm(_ context.Context, id string) (*model.Realm, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.realms[id]
	if !ok {
		return nil, fmt.Errorf("realm %s: %w", id, store.ErrNotFound)
	}
	cp := *r
	return &cp, nil
}

func (s *Store) ListRealms(_ context.Context, limit int) ([]*model.Realm, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*model.Realm, 0, len(s.realms))
	for _, r := range s.realms {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) CreateShard(_ context.Context, sh *model.Shard) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.shards[sh.ID]; ok {
		return fmt.Errorf("shard %s: %w", sh.ID, store.ErrConflict)
	}
	// PostgreSQL enforces the realm foreign key; mirror it (the admin layer
	// validates first, this is the store-level guarantee).
	if _, ok := s.realms[sh.RealmID]; !ok {
		return fmt.Errorf("shard %s: realm %s: %w", sh.ID, sh.RealmID, store.ErrNotFound)
	}
	// SQL stores default the column to 'active'; mirror it.
	if sh.Status == "" {
		sh.Status = "active"
	}
	// Stamp on the caller's object too (parity with the SQL stores) so the
	// HTTP response carries the real created_at instead of a zero time.
	sh.CreatedAt = time.Now()
	cp := *sh
	s.shards[sh.ID] = &cp
	return nil
}

func (s *Store) GetShard(_ context.Context, id string) (*model.Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sh, ok := s.shards[id]
	if !ok {
		return nil, fmt.Errorf("shard %s: %w", id, store.ErrNotFound)
	}
	cp := *sh
	return &cp, nil
}

func (s *Store) ListShards(_ context.Context, realmID string, limit int) ([]*model.Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*model.Shard, 0, len(s.shards))
	for _, sh := range s.shards {
		if realmID != "" && sh.RealmID != realmID {
			continue
		}
		cp := *sh
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ── Maintenance windows & announcements (TODO v0.1.20) ──────────

func (s *Store) CreateMaintenanceWindow(_ context.Context, w *model.MaintenanceWindow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.maintWindows[w.ID]; ok {
		return fmt.Errorf("maintenance window %s: %w", w.ID, store.ErrConflict)
	}
	// Stamp on the caller's object too (parity with the SQL stores) so the
	// HTTP response carries the real created_at instead of a zero time.
	w.CreatedAt = time.Now()
	cp := *w
	s.maintWindows[w.ID] = &cp
	return nil
}

func (s *Store) GetMaintenanceWindow(_ context.Context, id string) (*model.MaintenanceWindow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w, ok := s.maintWindows[id]
	if !ok {
		return nil, fmt.Errorf("maintenance window %s: %w", id, store.ErrNotFound)
	}
	cp := *w
	return &cp, nil
}

func (s *Store) DeleteMaintenanceWindow(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.maintWindows, id)
	return nil
}

func (s *Store) ListMaintenanceWindows(_ context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*model.MaintenanceWindow, 0, len(s.maintWindows))
	for _, w := range s.maintWindows {
		if serverID != "" && w.ServerID != serverID {
			continue
		}
		cp := *w
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartAt.After(out[j].StartAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) MarkMaintenanceWindowApplied(_ context.Context, id string, previous model.ServerStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, ok := s.maintWindows[id]
	if !ok {
		return fmt.Errorf("maintenance window %s: %w", id, store.ErrNotFound)
	}
	w.PreviousStatus = previous
	return nil
}

func (s *Store) CreateAnnouncement(_ context.Context, a *model.Announcement) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.announcements[a.ID]; ok {
		return fmt.Errorf("announcement %s: %w", a.ID, store.ErrConflict)
	}
	// Stamp on the caller's object too (parity with the SQL stores).
	a.CreatedAt = time.Now()
	cp := *a
	s.announcements[a.ID] = &cp
	return nil
}

func (s *Store) GetAnnouncement(_ context.Context, id string) (*model.Announcement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a, ok := s.announcements[id]
	if !ok {
		return nil, fmt.Errorf("announcement %s: %w", id, store.ErrNotFound)
	}
	cp := *a
	return &cp, nil
}

func (s *Store) DeleteAnnouncement(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.announcements, id)
	return nil
}

func (s *Store) ListAnnouncements(_ context.Context, f store.AnnouncementFilter) ([]*model.Announcement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	out := make([]*model.Announcement, 0, len(s.announcements))
	for _, a := range s.announcements {
		// Empty ServerID = global + every server; non-empty = global + that
		// one server (globals always pass the server filter).
		if f.ServerID != "" && (a.ServerID != nil && *a.ServerID != f.ServerID) {
			continue
		}
		if f.ActiveOnly && !a.Active(now) {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// ── Cross-server config (config center) — admin-cold, direct lock ──

// SaveCrossServerConfig stores the spec as the new config, atomically
// incrementing the version under the write lock (monotonic, stale writes
// can never win). The caller's object is updated to the stored snapshot.
func (s *Store) SaveCrossServerConfig(_ context.Context, cfg *model.CrossServerConfig) (*model.CrossServerConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := 1
	if s.crossConfig != nil {
		next = s.crossConfig.Version + 1
	}
	saved := *cfg
	// Deep-copy the spec: the stored document must not share backing
	// arrays with the caller — a later in-place caller mutation would
	// rewrite stored history (the SQL stores round-trip through JSON
	// and never alias).
	saved.Spec = model.CloneCrossServerSpec(cfg.Spec)
	saved.Version = next
	saved.UpdatedAt = time.Now()
	s.crossConfig = &saved
	cp := saved
	return &cp, nil
}

// GetCrossServerConfig returns the current config.
func (s *Store) GetCrossServerConfig(_ context.Context) (*model.CrossServerConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.crossConfig == nil {
		return nil, fmt.Errorf("cross-server config: %w", store.ErrNotFound)
	}
	cp := *s.crossConfig
	cp.Spec = model.CloneCrossServerSpec(s.crossConfig.Spec)
	return &cp, nil
}
