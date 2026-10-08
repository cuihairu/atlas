package memory

// Tags-filter benchmark — separate file because store.ServerFilter.Tags is
// new (the pre-index implementation has no field to compile against).

import (
	"context"
	"fmt"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

func BenchmarkListServersTags(b *testing.B) {
	ctx := context.Background()
	s := New()
	defer s.Close()
	for i := 0; i < benchServerN; i++ {
		srv := &model.Server{
			ID:       fmt.Sprintf("srv-%05d", i),
			Name:     fmt.Sprintf("srv-%05d", i),
			Region:   fmt.Sprintf("reg-%d", i%benchRegions),
			Version:  "1.0.0",
			Platform: "android",
			Status:   model.StatusOnline,
			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30000 + i},
		}
		if i%4 == 0 {
			srv.Tags = []model.ServerTag{{Code: "hot", Public: true}}
		}
		if i%8 == 0 {
			srv.Tags = append(srv.Tags, model.ServerTag{Code: "new", Public: true})
		}
		if err := s.RegisterServer(ctx, srv); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()

	b.Run("tag-hot", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Tags: []string{"hot"}, Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("tag-hot+new", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Tags: []string{"hot", "new"}, Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("tag-unknown", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListServers(ctx, store.ServerFilter{Tags: []string{"nope"}, Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
