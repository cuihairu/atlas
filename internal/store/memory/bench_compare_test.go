package memory

// Self-contained before/after comparison (docs/performance.md §8.5).
//
// The "before" side is replicated inline instead of checking out the
// pre-index / pre-queue commits: listServersFullScan is the old ListServers
// core loop (visit every row, verify inline, sort, page) and
// benchDirectHeartbeat is the old write critical section (build snapshot,
// lock, assign). Both sides therefore share one dataset, one machine, one
// run — no worktree, no patch-porting. The replica is verified against the
// indexed path at every size/filter combination before the timer starts
// (same IDs, same order), so the table can never silently compare two
// different queries.
//
// Unlike bench_test.go this file intentionally touches internals
// (s.mu / s.rtMu / s.servers) — it does not need to compile against the old
// store, it *reimplements* the old behaviour.

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// benchMixedServers registers n servers with every filter dimension spread
// across several buckets, so selective filters are actually selective — the
// shared fixtures (benchRegisterServers) are single-value on status/version
// and would make those dimensions match the whole table.
//
// 分布: region 4 桶 (reg-0 = 1/4), version 3 桶 (1/3), platform 4 桶 (1/4),
// status online 80% / maintenance 10% / starting 10%, tags hot 25% /
// hot+new 12.5%.
func benchMixedServers(ctx context.Context, b *testing.B, s *Store, n int) {
	b.Helper()
	versions := [3]string{"1.0.0", "1.1.0", "1.2.0"}
	platforms := [4]string{"android", "ios", "windows", "mac"}
	for i := 0; i < n; i++ {
		status := model.StatusOnline
		switch i % 10 {
		case 8:
			status = model.StatusMaintenance
		case 9:
			status = model.StatusStarting
		}
		srv := &model.Server{
			ID:       fmt.Sprintf("srv-%05d", i),
			Name:     fmt.Sprintf("srv-%05d", i),
			Region:   fmt.Sprintf("reg-%d", i%benchRegions),
			Version:  versions[i%3],
			Platform: platforms[i%4],
			Status:   status,
			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30000 + i},
			Capacity: 1000,
		}
		if i%4 == 0 {
			srv.Tags = []model.ServerTag{{Code: "hot", Public: true}}
		}
		if i%8 == 0 {
			srv.Tags = append(srv.Tags, model.ServerTag{Code: "new", Public: true})
		}
		if err := s.RegisterServer(ctx, srv); err != nil {
			b.Fatalf("register %d: %v", i, err)
		}
	}
}

// listServersFullScan replicates the pre-index ListServers: one pass over
// the table with the filter verified inline (tags included), matched IDs
// sorted, then paged. Cursor is exercised as empty — pagination semantics
// are contract-tested elsewhere (storetest), the bench measures the scan.
func listServersFullScan(s *Store, f store.ServerFilter) []*model.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > store.ListServersMaxLimit {
		limit = store.ListServersMaxLimit
	}

	var ids []string
	for id, srv := range s.servers {
		if matchServer(srv, f) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var result []*model.Server
	for _, id := range ids {
		if len(result) >= limit {
			break
		}
		cp := *s.servers[id]
		result = append(result, &cp)
	}
	return result
}

// benchAssertSameList pins replica equivalence: identical length, identical
// IDs in identical order. Runs un-timed before each benchmark group.
func benchAssertSameList(b *testing.B, scan, index []*model.Server) {
	b.Helper()
	if len(scan) != len(index) {
		b.Fatalf("replica drift: full-scan rows=%d index rows=%d", len(scan), len(index))
	}
	for i := range scan {
		if scan[i].ID != index[i].ID {
			b.Fatalf("replica drift at row %d: full-scan=%s index=%s", i, scan[i].ID, index[i].ID)
		}
	}
}

var benchCompareFilters = []struct {
	name string
	f    store.ServerFilter
}{
	{"no-filter", store.ServerFilter{}},
	{"region", store.ServerFilter{Region: "reg-0"}},              // 1/4 of the table
	{"version", store.ServerFilter{Version: "1.1.0"}},            // 1/3
	{"platform", store.ServerFilter{Platform: "ios"}},            // 1/4
	{"status", store.ServerFilter{Status: model.StatusOnline}},   // 80%
	{"tags", store.ServerFilter{Tags: []string{"hot"}}},          // 25%
	{"tags-unknown", store.ServerFilter{Tags: []string{"nope"}}}, // 0 — index short-circuits
}

// BenchmarkReadCompare is the §8.5 table: full-scan replica vs indexed path,
// same dataset, at 1k and 10k fleet sizes, every filter dimension + tags.
func BenchmarkReadCompare(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			ctx := context.Background()
			s := New()
			defer s.Close()
			benchMixedServers(ctx, b, s, n)

			// Equivalence gate: replica and index path must agree before
			// any timing runs.
			for _, tc := range benchCompareFilters {
				f := tc.f
				f.Limit = 200
				index, err := s.ListServers(ctx, f)
				if err != nil {
					b.Fatal(err)
				}
				benchAssertSameList(b, listServersFullScan(s, f), index)
			}
			b.ResetTimer()

			for _, tc := range benchCompareFilters {
				f := tc.f
				f.Limit = 200
				b.Run("fullscan/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						_ = listServersFullScan(s, f)
					}
				})
				b.Run("index/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						if _, err := s.ListServers(ctx, f); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

// benchDirectHeartbeat replicates the pre-queue write path: build the
// runtime snapshot, take the lock, assign, release — one critical section
// per write, no merging, no batching (the old hot path for heartbeats).
func benchDirectHeartbeat(s *Store, id string, rt model.Runtime) {
	s.rtMu.Lock()
	s.runtimes[id] = rt
	s.rtMu.Unlock()
}

// BenchmarkWriteCompare is the §8.5 write table: direct single-write (old)
// vs instruction queue (new), uncontended single-server and 14-core storm
// shapes. The same-target storm also reports the merge rate (%merged) and
// average committed batch size (avg-batch) straight from QueueStats —
// batch commit is what turns one critical section per write into one per
// batch.
func BenchmarkWriteCompare(b *testing.B) {
	ctx := context.Background()

	const stormSrv = 50
	ids := make([]string, stormSrv)
	setup := New()
	defer setup.Close()
	for i := range ids {
		ids[i] = fmt.Sprintf("srv-%05d", i)
		if err := setup.RegisterServer(ctx, &model.Server{
			ID: ids[i], Name: ids[i], Region: "reg-0", Version: "1.0.0",
			Status:   model.StatusOnline,
			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30000 + i},
		}); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("single/direct", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchDirectHeartbeat(setup, ids[0], model.Runtime{
				Players: i % 500, Load: 0.5, LastSeenAt: time.Now(),
			})
		}
	})
	b.Run("single/queue", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := setup.RecordHeartbeat(ctx, ids[0], model.Heartbeat{Players: i % 500, Load: 0.5}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("storm-direct/50srv", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				benchDirectHeartbeat(setup, ids[i%stormSrv], model.Runtime{
					Players: i % 500, Load: 0.5, LastSeenAt: time.Now(),
				})
				i++
			}
		})
	})
	b.Run("storm-queue/50srv", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if err := setup.RecordHeartbeat(ctx, ids[i%stormSrv], model.Heartbeat{Players: i % 500, Load: 0.5}); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	})

	// Same-target storm: the merge case — every heartbeat hits one server,
	// so within a committed batch the older ones are superseded. Reports
	// the merge rate and the applied fraction straight from QueueStats
	// deltas (the recent-flush ring only holds the drain tail, so avg
	// batch size is not reported from it — %merged over the whole run is
	// the reliable batch-shape signal).
	b.Run("storm-queue/same-target", func(b *testing.B) {
		b.ReportAllocs()
		before := setup.QueueStats()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if err := setup.RecordHeartbeat(ctx, ids[0], model.Heartbeat{Players: 100, Load: 0.5}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.StopTimer()
		after := setup.QueueStats()

		if enq := after.Enqueued - before.Enqueued; enq > 0 {
			b.ReportMetric(100*float64(after.Merged-before.Merged)/float64(enq), "%merged")
			b.ReportMetric(100*float64(after.Applied-before.Applied)/float64(enq), "%applied")
		}
	})
}
