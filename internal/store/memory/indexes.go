package memory

import (
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// This file implements the inverted indexes over the in-memory server and
// character tables (docs/performance.md §性能设计 — 索引策略).
//
// Atomicity contract (拍板①): every index mutation happens inside the SAME
// write-lock critical section as the data mutation it mirrors — index and
// data can never diverge, and readers hold the matching read lock so they
// always see a consistent (data, index) pair. Maintenance is incremental:
// O(1) per changed field per instruction, never a rebuild. A full snapshot
// + atomic.Pointer publish was evaluated and rejected: every write would
// copy the whole table (O(N) write amplification at fleet sizes), while the
// read path already runs lock-free-ish under RWMutex RLock.

// indexedServer is the projection of a server's indexable fields. remove()
// needs the OLD projection, so mutations snapshot it before overwriting.
type indexedServer struct {
	region   string
	realm    *string
	shard    *string
	version  string
	platform string
	status   model.ServerStatus
	tagCodes []string // public tags only — internal tags never match ?tags=
}

func projectServer(srv *model.Server) indexedServer {
	ix := indexedServer{
		region:   srv.Region,
		realm:    srv.RealmID,
		shard:    srv.ShardID,
		version:  srv.Version,
		platform: srv.Platform,
		status:   srv.Status,
	}
	for _, t := range srv.Tags {
		if t.Public {
			ix.tagCodes = append(ix.tagCodes, t.Code)
		}
	}
	return ix
}

// serverIndex is the multi-field inverted index: field value → ID set.
type serverIndex struct {
	region   map[string]map[string]struct{}
	realm    map[string]map[string]struct{}
	shard    map[string]map[string]struct{}
	version  map[string]map[string]struct{}
	platform map[string]map[string]struct{}
	status   map[model.ServerStatus]map[string]struct{}
	tags     map[string]map[string]struct{} // public tag codes only
}

func newServerIndex() *serverIndex {
	return &serverIndex{
		region:   make(map[string]map[string]struct{}),
		realm:    make(map[string]map[string]struct{}),
		shard:    make(map[string]map[string]struct{}),
		version:  make(map[string]map[string]struct{}),
		platform: make(map[string]map[string]struct{}),
		status:   make(map[model.ServerStatus]map[string]struct{}),
		tags:     make(map[string]map[string]struct{}),
	}
}

// add indexes id under every non-empty field of view. Callers hold the
// server write lock.
func (ix *serverIndex) add(id string, v indexedServer) {
	addRef(ix.region, v.region, id)
	if v.realm != nil {
		addRef(ix.realm, *v.realm, id)
	}
	if v.shard != nil {
		addRef(ix.shard, *v.shard, id)
	}
	addRef(ix.version, v.version, id)
	addRef(ix.platform, v.platform, id)
	addRefStatus(ix.status, v.status, id)
	for _, code := range v.tagCodes {
		addRef(ix.tags, code, id)
	}
}

// remove un-indexes id from every non-empty field of the (old) view,
// dropping emptied buckets so churn cannot grow the index without bound.
func (ix *serverIndex) remove(id string, v indexedServer) {
	removeRef(ix.region, v.region, id)
	if v.realm != nil {
		removeRef(ix.realm, *v.realm, id)
	}
	if v.shard != nil {
		removeRef(ix.shard, *v.shard, id)
	}
	removeRef(ix.version, v.version, id)
	removeRef(ix.platform, v.platform, id)
	removeRefStatus(ix.status, v.status, id)
	for _, code := range v.tagCodes {
		removeRef(ix.tags, code, id)
	}
}

// candidates picks the smallest index set among the filter's indexed
// dimensions (region/realm/shard/version/platform/status/tags). The caller
// verifies the full filter per candidate anyway, so choosing one dimension
// as the driver and checking the rest is equivalent to a full intersection
// at a fraction of the set operations. Return values:
//
//	nil             — no indexed filter present; the caller scans the table
//	empty, non-nil  — an indexed value matches nothing (e.g. a tag code no
//	                  server carries): empty result, never a full scan
//	non-empty       — the candidate IDs to verify
func (ix *serverIndex) candidates(f store.ServerFilter) []string {
	var sets []map[string]struct{}
	if f.Region != "" {
		sets = append(sets, ix.region[f.Region])
	}
	if f.Realm != "" {
		sets = append(sets, ix.realm[f.Realm])
	}
	if f.Shard != "" {
		sets = append(sets, ix.shard[f.Shard])
	}
	if f.Version != "" {
		sets = append(sets, ix.version[f.Version])
	}
	if f.Platform != "" {
		sets = append(sets, ix.platform[f.Platform])
	}
	if f.Status != "" {
		sets = append(sets, ix.status[f.Status])
	}
	for _, code := range f.Tags {
		sets = append(sets, ix.tags[code])
	}
	if len(sets) == 0 {
		return nil
	}
	smallest := sets[0]
	for _, set := range sets[1:] {
		if len(set) < len(smallest) {
			smallest = set
		}
	}
	ids := make([]string, 0, len(smallest))
	for id := range smallest {
		ids = append(ids, id)
	}
	return ids
}

func addRef(m map[string]map[string]struct{}, key, id string) {
	if key == "" {
		return
	}
	set, ok := m[key]
	if !ok {
		set = make(map[string]struct{})
		m[key] = set
	}
	set[id] = struct{}{}
}

func removeRef(m map[string]map[string]struct{}, key, id string) {
	if key == "" {
		return
	}
	set, ok := m[key]
	if !ok {
		return
	}
	delete(set, id)
	if len(set) == 0 {
		delete(m, key)
	}
}

func addRefStatus(m map[model.ServerStatus]map[string]struct{}, key model.ServerStatus, id string) {
	if key == "" {
		return
	}
	set, ok := m[key]
	if !ok {
		set = make(map[string]struct{})
		m[key] = set
	}
	set[id] = struct{}{}
}

func removeRefStatus(m map[model.ServerStatus]map[string]struct{}, key model.ServerStatus, id string) {
	if key == "" {
		return
	}
	set, ok := m[key]
	if !ok {
		return
	}
	delete(set, id)
	if len(set) == 0 {
		delete(m, key)
	}
}

// charIndex is the character-directory inverted index. The composite key
// ("accountID:serverID:characterID") never changes for a row, so updates
// touch no index at all — only insert/delete do. That is what makes index
// maintenance O(1) per instruction on the upsert hot path.
type charIndex struct {
	byAccount       map[int64]map[string]struct{}            // accountID → keys
	byServer        map[string]map[string]struct{}           // serverID → keys
	byAccountServer map[int64]map[string]map[string]struct{} // accountID → serverID → keys
	byCharID        map[int64]string                         // global character ID → key
}

func newCharIndex() *charIndex {
	return &charIndex{
		byAccount:       make(map[int64]map[string]struct{}),
		byServer:        make(map[string]map[string]struct{}),
		byAccountServer: make(map[int64]map[string]map[string]struct{}),
		byCharID:        make(map[int64]string),
	}
}

func (ix *charIndex) add(key string, ch *model.Character) {
	addKeyRef(ix.byAccount, ch.AccountID, key)
	addKeyRef(ix.byServer, ch.ServerID, key)
	bySrv, ok := ix.byAccountServer[ch.AccountID]
	if !ok {
		bySrv = make(map[string]map[string]struct{})
		ix.byAccountServer[ch.AccountID] = bySrv
	}
	addKeyRef(bySrv, ch.ServerID, key)
	// Global character IDs are unique by game convention; should a duplicate
	// ever arrive, the newest row wins the point lookup (documented parity
	// with the previous full-scan behavior, which returned an arbitrary
	// match).
	ix.byCharID[ch.CharacterID] = key
}

func (ix *charIndex) remove(key string, ch *model.Character) {
	removeKeyRef(ix.byAccount, ch.AccountID, key)
	removeKeyRef(ix.byServer, ch.ServerID, key)
	if bySrv := ix.byAccountServer[ch.AccountID]; bySrv != nil {
		removeKeyRef(bySrv, ch.ServerID, key)
		if len(bySrv) == 0 {
			delete(ix.byAccountServer, ch.AccountID)
		}
	}
	if ix.byCharID[ch.CharacterID] == key {
		delete(ix.byCharID, ch.CharacterID)
	}
}

func addKeyRef[K comparable](m map[K]map[string]struct{}, key K, id string) {
	set, ok := m[key]
	if !ok {
		set = make(map[string]struct{})
		m[key] = set
	}
	set[id] = struct{}{}
}

func removeKeyRef[K comparable](m map[K]map[string]struct{}, key K, id string) {
	set, ok := m[key]
	if !ok {
		return
	}
	delete(set, id)
	if len(set) == 0 {
		delete(m, key)
	}
}
