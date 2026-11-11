// Package discovery implements the server discovery service described in
// docs/api.md §Discovery.
package discovery

import (
	"context"
	"fmt"

	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
)

// Service provides read-only server discovery.
type Service struct {
	servers store.ServerStore
	runtime store.RuntimeStore
	metrics *metrics.Metrics
}

// New creates a new discovery service.
func New(servers store.ServerStore, runtime store.RuntimeStore) *Service {
	return &Service{
		servers: servers,
		runtime: runtime,
	}
}

// WithMetrics attaches Prometheus instrumentation (optional).
func (s *Service) WithMetrics(m *metrics.Metrics) *Service {
	s.metrics = m
	return s
}

// ListServers returns servers matching the filter, with runtime data merged in.
//
// By default (when filter.Status is empty), only visible servers are returned.
// If filter.Status is explicitly set, that exact status is used.
func (s *Service) ListServers(ctx context.Context, f store.ServerFilter) ([]*model.Server, error) {
	s.metrics.CountDiscovery(f)
	ctx, span := atlastracing.Start(ctx, "discovery.list_servers")
	defer span.End()

	servers, err := s.servers.ListServers(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}

	// Default visibility: only return servers with Visible() status unless
	// the caller explicitly requested a status.
	if f.Status == "" {
		filtered := servers[:0]
		for _, srv := range servers {
			if srv.Status.Visible() {
				filtered = append(filtered, srv)
			}
		}
		servers = filtered
	}

	// Merge runtime data into each server: one batched read, not N single
	// key reads — the Redis store executes this as a single pipeline exec
	// (N round trips → 1–2, docs/performance.md §2). Missing snapshots are
	// simply absent from the result.
	ids := make([]string, len(servers))
	for i, srv := range servers {
		ids[i] = srv.ID
	}
	if rtMap, err := s.runtime.GetRuntimes(ctx, ids); err == nil {
		for _, srv := range servers {
			if rt, ok := rtMap[srv.ID]; ok {
				srv.Players = rt.Players
				srv.Load = rt.Load
				srv.LastSeenAt = &rt.LastSeenAt
			}
		}
	} // else: runtime unavailable; degrade to archive-only rows (historical tolerance)

	// Discovery is the player-facing view: only public tags travel (internal
	// markers stay on the admin surface). The admin API reads the store
	// directly and sees the full list.
	for _, srv := range servers {
		srv.Tags = model.PublicTags(srv.Tags)
	}

	return servers, nil
}

// GetServer returns a single server by ID with runtime data merged in.
func (s *Service) GetServer(ctx context.Context, id string) (*model.Server, error) {
	ctx, span := atlastracing.Start(ctx, "discovery.get_server")
	defer span.End()
	srv, err := s.servers.GetServer(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get server: %w", err)
	}

	rt, err := s.runtime.GetRuntime(ctx, id)
	if err == nil {
		srv.Players = rt.Players
		srv.Load = rt.Load
		srv.LastSeenAt = &rt.LastSeenAt
	}

	srv.Tags = model.PublicTags(srv.Tags)
	return srv, nil
}
