// Package fleet maintains the in-memory server aggregation index (BUGS ③).
//
// Design decree: aggregates like region / status / version counts and the
// server-ID index are computed once, on the write path (register /
// heartbeat / unregister / lifecycle / tags), and kept in memory. Every view
// — the admin stats (总览), the /servers filter bar, and the admin server
// list — reads this same aggregation instead of each page re-querying and
// re-counting on its own. Numbers agree by construction.
//
// Feeding happens through the TrackServers / TrackRuntime store decorators
// (every service write passes through them, so no call site changes), plus:
//
//   - an initial seed at startup (full store + runtime merge), and
//   - a periodic reconciler that rebuilds the snapshot from the stores —
//     the safety net for writes this process did not observe (multi-replica
//     HA deployments share the SQL/Redis stores; each replica only sees its
//     own traffic between reconciles).
package fleet

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Index is the in-memory aggregate: server records keyed by ID plus the
// count maps every admin view reads. All methods are safe for concurrent
// use.
type Index struct {
	mu sync.RWMutex

	servers map[string]*model.Server

	byStatus  map[string]int
	byRegion  map[string]int
	byVersion map[string]int
	byType    map[string]int
	byRealm   map[string]int
	byShard   map[string]int
	byTag     map[string]int
	metaKeys  map[string]struct{}

	players  int // summed from heartbeat-fed runtime fields
	capacity int // summed from base records
}

// New creates an empty index.
func New() *Index {
	return &Index{
		servers:   map[string]*model.Server{},
		byStatus:  map[string]int{},
		byRegion:  map[string]int{},
		byVersion: map[string]int{},
		byType:    map[string]int{},
		byRealm:   map[string]int{},
		byShard:   map[string]int{},
		byTag:     map[string]int{},
		metaKeys:  map[string]struct{}{},
	}
}

// Upsert records a base server snapshot (register / reconcile / tag write).
// Runtime-owned fields (Players / Load / LastSeenAt) carried by srv are
// ignored: only UpdateRuntime owns them, so a store read with zeroed
// runtime fields cannot wipe live heartbeat data.
func (x *Index) Upsert(srv *model.Server) {
	if srv == nil || srv.ID == "" {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	cp := *srv
	if old, ok := x.servers[srv.ID]; ok {
		cp.Players, cp.Load, cp.LastSeenAt = old.Players, old.Load, old.LastSeenAt
		x.removeCounts(old)
	} else {
		cp.Players, cp.Load, cp.LastSeenAt = 0, 0, nil
	}
	x.addCounts(&cp)
	x.servers[srv.ID] = &cp
}

// UpdateStatus applies a lifecycle status change (admin actions, health
// monitor sweeps, unregister).
func (x *Index) UpdateStatus(id string, status model.ServerStatus) {
	x.mu.Lock()
	defer x.mu.Unlock()
	srv, ok := x.servers[id]
	if !ok || srv.Status == status {
		return
	}
	x.byStatus[string(srv.Status)]--
	if x.byStatus[string(srv.Status)] <= 0 {
		delete(x.byStatus, string(srv.Status))
	}
	srv.Status = status
	x.byStatus[string(status)]++
}

// UpdateRuntime records heartbeat-fed runtime data (players / load / last
// seen) for a server already known to the index.
func (x *Index) UpdateRuntime(id string, players int, load float64, seen time.Time) {
	x.mu.Lock()
	defer x.mu.Unlock()
	srv, ok := x.servers[id]
	if !ok {
		return
	}
	x.players += players - srv.Players
	srv.Players = players
	srv.Load = load
	if !seen.IsZero() {
		t := seen
		srv.LastSeenAt = &t
	} else {
		srv.LastSeenAt = nil
	}
}

// Remove drops a record entirely (delete / reconcile of vanished rows).
func (x *Index) Remove(id string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if srv, ok := x.servers[id]; ok {
		x.removeCounts(srv)
		delete(x.servers, id)
	}
}

// Get returns a copy of one record by ID (the server-ID index).
func (x *Index) Get(id string) (*model.Server, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	srv, ok := x.servers[id]
	if !ok {
		return nil, false
	}
	cp := *srv
	return &cp, true
}

// Len reports the number of tracked servers.
func (x *Index) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.servers)
}

// ListAll returns copies of every record, ID-sorted — full-fleet readers
// like the telemetry sampler.
func (x *Index) ListAll() []*model.Server {
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := make([]*model.Server, 0, len(x.servers))
	for _, srv := range x.servers {
		cp := *srv
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Stats snapshots the aggregate every view reads. Always includes the
// classic facets plus the filter-bar facets (type / realm / shard / tag)
// and the fleet's metadata keys.
func (x *Index) Stats() *model.Stats {
	x.mu.RLock()
	defer x.mu.RUnlock()
	st := &model.Stats{
		TotalServers:       len(x.servers),
		ServersByStatus:    copyCounts(x.byStatus),
		ServersByRegion:    copyCounts(x.byRegion),
		ServersByVersion:   copyCounts(x.byVersion),
		ServersByType:      copyCounts(x.byType),
		ServersByRealm:     copyCounts(x.byRealm),
		ServersByShard:     copyCounts(x.byShard),
		ServersByTag:       copyCounts(x.byTag),
		ServerMetadataKeys: sortedSet(x.metaKeys),
		TotalPlayers:       x.players,
		TotalCapacity:      x.capacity,
	}
	st.OnlineServers = st.ServersByStatus[string(model.StatusOnline)]
	return st
}

// ListFilter selects servers from the index. ID is a substring match; all
// other fields are exact. MetadataKey requires MetadataValue (key/value
// pair filter, same contract as the character metadata filter).
type ListFilter struct {
	ID            string
	Status        model.ServerStatus
	Region        string
	Realm         string
	Shard         string
	Version       string
	Type          string
	Platform      string
	Tag           string
	MetadataKey   string
	MetadataValue string
	Limit         int
	Cursor        string
}

// List returns a filtered, ID-sorted page plus the next cursor (empty when
// the page is the last). Same pagination contract as the stores.
func (x *Index) List(f ListFilter) ([]*model.Server, string) {
	x.mu.RLock()
	matched := make([]*model.Server, 0, len(x.servers))
	for _, srv := range x.servers {
		if Matches(srv, f) {
			cp := *srv
			matched = append(matched, &cp)
		}
	}
	x.mu.RUnlock()

	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

	start := 0
	if f.Cursor != "" {
		for i, srv := range matched {
			if srv.ID > f.Cursor {
				start = i
				break
			}
			start = i + 1
		}
		matched = matched[start:]
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	// Cursor = last returned ID (same contract as the stores): the next page
	// keeps IDs strictly greater than it, so nothing is skipped or repeated.
	if len(matched) > limit {
		page := matched[:limit]
		return page, page[len(page)-1].ID
	}
	return matched, ""
}

// Matches reports whether a server satisfies the (non-pagination) parts of
// f. Shared by the index List and the store-backed fallback so both paths
// apply identical predicates.
func Matches(srv *model.Server, f ListFilter) bool {
	if f.ID != "" && !stringsContains(srv.ID, f.ID) {
		return false
	}
	if f.Status != "" && srv.Status != f.Status {
		return false
	}
	if f.Region != "" && srv.Region != f.Region {
		return false
	}
	if f.Realm != "" && (srv.RealmID == nil || *srv.RealmID != f.Realm) {
		return false
	}
	if f.Shard != "" && (srv.ShardID == nil || *srv.ShardID != f.Shard) {
		return false
	}
	if f.Version != "" && srv.Version != f.Version {
		return false
	}
	if f.Type != "" && srv.Type != f.Type {
		return false
	}
	if f.Platform != "" && srv.Platform != f.Platform {
		return false
	}
	if f.Tag != "" && !model.HasTag(srv.Tags, f.Tag) {
		return false
	}
	if f.MetadataKey != "" && srv.Metadata[f.MetadataKey] != f.MetadataValue {
		return false
	}
	return true
}

// Reconcile rebuilds the whole snapshot from the authoritative stores.
// Called once at startup (the seed) and periodically by the reconciler.
func (x *Index) Reconcile(ctx context.Context, servers store.ServerStore, runtime store.RuntimeStore) error {
	// Full base list, cursor-paginated at the store's per-call cap: the
	// store clamps any larger request down to the cap, so the short-page
	// termination must measure against the cap (asking for 500 against a
	// 200 cap stopped the walk after page one).
	var all []*model.Server
	cursor := ""
	for {
		page, err := servers.ListServers(ctx, store.ServerFilter{Limit: store.ListServersMaxLimit, Cursor: cursor})
		if err != nil {
			return err
		}
		all = append(all, page...)
		if len(page) < store.ListServersMaxLimit {
			break
		}
		cursor = page[len(page)-1].ID
	}

	runtimes := map[string]model.Runtime{}
	if runtime != nil {
		var err error
		runtimes, err = runtime.ListRuntimes(ctx)
		if err != nil {
			return err
		}
	}

	next := map[string]*model.Server{}
	for _, srv := range all {
		cp := *srv
		if rt, ok := runtimes[srv.ID]; ok {
			cp.Players = rt.Players
			cp.Load = rt.Load
			t := rt.LastSeenAt
			cp.LastSeenAt = &t
		}
		next[srv.ID] = &cp
	}

	x.mu.Lock()
	defer x.mu.Unlock()
	x.servers = next
	x.rebuildCounts()
	return nil
}

// RunReconciler keeps the index fresh with a full rebuild every interval.
// Cross-replica writes (shared SQL / Redis stores) land here; local writes
// were already fed synchronously by the decorators.
func RunReconciler(ctx context.Context, x *Index, servers store.ServerStore, runtime store.RuntimeStore, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := x.Reconcile(ctx, servers, runtime); err != nil {
				// Keep serving the previous snapshot; next tick retries.
				continue
			}
		}
	}
}

// ── count maintenance ───────────────────────────────────────────────

func (x *Index) addCounts(srv *model.Server) {
	x.byStatus[string(srv.Status)]++
	x.byRegion[srv.Region]++
	x.byVersion[srv.Version]++
	if srv.Type != "" {
		x.byType[srv.Type]++
	}
	if srv.RealmID != nil && *srv.RealmID != "" {
		x.byRealm[*srv.RealmID]++
	}
	if srv.ShardID != nil && *srv.ShardID != "" {
		x.byShard[*srv.ShardID]++
	}
	for _, tag := range srv.Tags {
		x.byTag[tag.Code]++
	}
	for k := range srv.Metadata {
		x.metaKeys[k] = struct{}{}
	}
	x.capacity += srv.Capacity
	x.players += srv.Players
}

func (x *Index) removeCounts(srv *model.Server) {
	dec := func(m map[string]int, k string) {
		m[k]--
		if m[k] <= 0 {
			delete(m, k)
		}
	}
	dec(x.byStatus, string(srv.Status))
	dec(x.byRegion, srv.Region)
	dec(x.byVersion, srv.Version)
	if srv.Type != "" {
		dec(x.byType, srv.Type)
	}
	if srv.RealmID != nil && *srv.RealmID != "" {
		dec(x.byRealm, *srv.RealmID)
	}
	if srv.ShardID != nil && *srv.ShardID != "" {
		dec(x.byShard, *srv.ShardID)
	}
	for _, tag := range srv.Tags {
		dec(x.byTag, tag.Code)
	}
	x.capacity -= srv.Capacity
	x.players -= srv.Players
}

// rebuildCounts recomputes every aggregate from x.servers (post-reconcile
// swap). Metadata keys are rebuilt conservatively: keys only ever observed
// stay listed until a reconcile rebuild replaces the snapshot.
func (x *Index) rebuildCounts() {
	x.byStatus = map[string]int{}
	x.byRegion = map[string]int{}
	x.byVersion = map[string]int{}
	x.byType = map[string]int{}
	x.byRealm = map[string]int{}
	x.byShard = map[string]int{}
	x.byTag = map[string]int{}
	x.metaKeys = map[string]struct{}{}
	x.players = 0
	x.capacity = 0
	for _, srv := range x.servers {
		x.addCounts(srv)
	}
}

func copyCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func sortedSet(s map[string]struct{}) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stringsContains(s, sub string) bool {
	return strings.Contains(s, sub)
}
