package memory

// Benchmarks for the indexed store (docs/performance.md §性能设计).
// Public-API only so the file also compiles against the pre-index full-scan
// implementation — the before/after numbers in the doc come from running
// exactly this file on both sides. The tags-filter benchmark lives in
// bench_tags_test.go because ServerFilter.Tags is new.

import (
	"context"
	"fmt"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

const (
	benchServerN   = 2000
	benchRegions   = 4
	benchAccountN  = 200
	benchCharsAcct = 20 // characters per account = one per indexed server
)

func benchRegisterServers(ctx context.Context, b *testing.B, s *Store, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		srv := &model.Server{
			ID:       fmt.Sprintf("srv-%05d", i),
			Name:     fmt.Sprintf("srv-%05d", i),
			Region:   fmt.Sprintf("reg-%d", i%benchRegions),
			Version:  "1.0.0",
			Platform: []string{"android", "ios", "windows", "mac"}[i%4],
			Status:   model.StatusOnline,
			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30000 + i},
			Capacity: 1000,
		}
		if err := s.RegisterServer(ctx, srv); err != nil {
			b.Fatalf("register %d: %v", i, err)
		}
	}
}

func benchUpsertCharacters(ctx context.Context, b *testing.B, s *Store) {
	b.Helper()
	for acct := int64(1); acct <= benchAccountN; acct++ {
		for sv := 0; sv < benchCharsAcct; sv++ {
			ch := &model.Character{
				AccountID:   acct,
				ServerID:    fmt.Sprintf("srv-%05d", sv*100), // 20 of the 2000 servers hold all characters
				CharacterID: acct*1000 + int64(sv),
				Name:        fmt.Sprintf("char-%d-%d", acct, sv),
				Level:       int(acct % 100),
			}
			if err := s.UpsertCharacter(ctx, ch); err != nil {
				b.Fatalf("upsert %d/%s: %v", acct, ch.ServerID, err)
			}
		}
	}
}

func BenchmarkListServers(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	benchRegisterServers(ctx, b, s, benchServerN)
	b.ResetTimer()

	b.Run("no-filter", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("region", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Region: "reg-0", Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("version", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Version: "1.0.0", Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("platform", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Platform: "ios", Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("status", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Status: model.StatusOnline, Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkListServersScaling pins the index payoff curve: the full-scan
// cost grows linearly with the table, while the indexed query visits only
// the matched bucket (plus the sort that pagination requires). Same query —
// region filter matching 1/4 of the table — at two fleet sizes.
func BenchmarkListServersScaling(b *testing.B) {
	for _, n := range []int{2000, 20000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			ctx := context.Background()
			s := New()
			defer s.Close()
			benchRegisterServers(ctx, b, s, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.ListServers(ctx, store.ServerFilter{Region: "reg-0", Limit: 200}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkGetServer(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	benchRegisterServers(ctx, b, s, benchServerN)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetServer(ctx, "srv-00042"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListCharactersByAccount(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	benchUpsertCharacters(ctx, b, s)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListCharactersByAccount(ctx, 7); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListCharactersByServer(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	benchUpsertCharacters(ctx, b, s)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListCharactersByServer(ctx, "srv-00000", 200, ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetCharacterByCharacterID(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	benchUpsertCharacters(ctx, b, s)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetCharacterByCharacterID(ctx, 7*1000+3); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRecordHeartbeat measures the uncontended single-write path:
// enqueue → lane apply → ack. The pre-queue direct-lock path is the baseline.
func BenchmarkRecordHeartbeat(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	benchRegisterServers(ctx, b, s, 8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.RecordHeartbeat(ctx, "srv-00000", model.Heartbeat{Players: i % 500, Load: 0.5}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRecordHeartbeatBurst hammers 50 servers from every core — the
// heartbeat-storm shape (monitor sweep + login wave). This is where lane
// batching shows up: one critical section per committed batch instead of
// one per write, same-server heartbeats coalesced to the newest.
func BenchmarkRecordHeartbeatBurst(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	ids := make([]string, 50)
	for i := range ids {
		ids[i] = fmt.Sprintf("srv-%05d", i)
		if err := s.RegisterServer(ctx, &model.Server{
			ID: ids[i], Name: ids[i], Region: "reg-0", Version: "1.0.0",
			Status:   model.StatusOnline,
			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30000 + i},
		}); err != nil {
			b.Fatal(err)
		}
	}
	hb := model.Heartbeat{Players: 100, Load: 0.5}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if err := s.RecordHeartbeat(ctx, ids[i%len(ids)], hb); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
