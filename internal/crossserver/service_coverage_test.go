package crossserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// failingServers injects ListServers failures so the addressing degradation
// path (signal over-notifies instead of silently dropping) can be driven
// without a real fleet.
type failingServers struct {
	store.ServerStore
	err error
}

func (f *failingServers) ListServers(ctx context.Context, flt store.ServerFilter) ([]*model.Server, error) {
	return nil, f.err
}

// TestUpdatePublishesSnapshot pins the full-document publish wrapper: it
// stores the spec, surfaces the snapshot, and propagates validation
// failures from Save untouched.
func TestUpdatePublishesSnapshotAndRejectsInvalid(t *testing.T) {
	svc, _ := newSvc(t, nil)
	ctx := context.Background()

	snap, err := svc.Update(ctx, UpdateRequest{Spec: testSpec()})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if snap == nil || snap.Version != 1 {
		t.Fatalf("Update snapshot = %+v, want version 1", snap)
	}
	got, err := svc.Get(ctx)
	if err != nil || got.Version != snap.Version {
		t.Fatalf("Get after Update = %+v err=%v, want stored snapshot", got, err)
	}

	// A duplicate cluster id is the canonical invalid publish (same
	// rejection Save enforces).
	dup := testSpec()
	dup.Topology.Clusters = append(dup.Topology.Clusters, model.CrossServerCluster{ID: "cluster-ea"})
	if _, err := svc.Update(ctx, UpdateRequest{Spec: dup}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("Update duplicate cluster id = %v, want ErrInvalid", err)
	}
}

// TestDiffTargetsDomainChanges drives the match-domain diff branch by
// branch: add / remove / rename / membership / params all name the domain,
// an unchanged document falls back to the global target.
func TestDiffTargetsDomainChanges(t *testing.T) {
	base := model.CrossServerSpec{
		MatchDomains: []model.CrossServerMatchDomain{
			{ID: "d1", Name: "战场", Servers: []string{"a", "b"}, Params: map[string]string{"mode": "capture"}},
		},
	}
	domains := func(in ...model.CrossServerMatchDomain) model.CrossServerSpec {
		upd := model.CrossServerSpec{MatchDomains: append([]model.CrossServerMatchDomain{}, in...)}
		return upd
	}

	added := domains(base.MatchDomains[0], model.CrossServerMatchDomain{ID: "d2", Name: "副本", Servers: []string{"c"}})
	renamed := domains(model.CrossServerMatchDomain{ID: "d1", Name: "战场改", Servers: []string{"a", "b"}, Params: map[string]string{"mode": "capture"}})
	memberChanged := domains(model.CrossServerMatchDomain{ID: "d1", Name: "战场", Servers: []string{"a", "c"}, Params: map[string]string{"mode": "capture"}})
	memberShrunk := domains(model.CrossServerMatchDomain{ID: "d1", Name: "战场", Servers: []string{"a"}, Params: map[string]string{"mode": "capture"}})
	paramValChanged := domains(model.CrossServerMatchDomain{ID: "d1", Name: "战场", Servers: []string{"a", "b"}, Params: map[string]string{"mode": "siege"}})
	paramSetChanged := domains(model.CrossServerMatchDomain{ID: "d1", Name: "战场", Servers: []string{"a", "b"}, Params: map[string]string{"mode": "capture", "cap": "5"}})

	cases := []struct {
		name string
		upd  model.CrossServerSpec
		want []string
	}{
		{"unchanged falls back to global", base, []string{model.TargetAll}},
		{"added domain", added, []string{"d2"}},
		{"removed domain", domains(), []string{"d1"}},
		{"renamed domain", renamed, []string{"d1"}},
		{"membership change", memberChanged, []string{"d1"}},
		{"membership shrink", memberShrunk, []string{"d1"}},
		{"params value change", paramValChanged, []string{"d1"}},
		{"params set change", paramSetChanged, []string{"d1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diffTargets(base, tc.upd)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("diffTargets = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReceiversAddressingBranches covers the addressing decisions around
// the main path: global short-circuit, nil server store, and a change that
// names no members at all.
func TestReceiversAddressingBranches(t *testing.T) {
	ctx := context.Background()

	svc, _ := newSvc(t, nil)
	if got := svc.receivers(ctx, testSpec(), testSpec(), []string{model.TargetAll}); !slices.Equal(got, []string{model.TargetAll}) {
		t.Fatalf("global target receivers = %v, want [\"*\"]", got)
	}

	mem := memory.New()
	nilServers := New(mem, nil, nil, nil)
	if got := nilServers.receivers(ctx, testSpec(), testSpec(), []string{"cluster-ea"}); got != nil {
		t.Fatalf("receivers without a server store = %v, want nil", got)
	}

	svcLive, _ := newSvc(t, nil)
	empty := model.CrossServerSpec{
		Topology: model.CrossServerTopology{Clusters: []model.CrossServerCluster{{ID: "c1", Servers: []string{}}}},
	}
	if got := svcLive.receivers(ctx, empty, empty, []string{"c1"}); got != nil {
		t.Fatalf("receivers for a memberless target = %v, want nil", got)
	}
}

// TestReceiversDegradeToAllOnStoreError pins the degrade-not-drop rule:
// when the fleet list cannot be read the signal addresses everyone.
func TestReceiversDegradeToAllOnStoreError(t *testing.T) {
	mem := memory.New()
	svc := New(mem, &failingServers{err: errors.New("list unavailable")}, nil, nil)
	got := svc.receivers(context.Background(), testSpec(), testSpec(), []string{"cluster-ea"})
	if !slices.Equal(got, []string{model.TargetAll}) {
		t.Fatalf("degraded receivers = %v, want [\"*\"]", got)
	}
}

// pagingServers hands out pre-baked pages in order, so the pager itself
// can be driven across a page boundary (the memory store caps ListServers
// at 200 — that contract mismatch is tracked separately in TODO.md).
type pagingServers struct {
	store.ServerStore
	pages [][]*model.Server
	calls int
}

func (p *pagingServers) ListServers(ctx context.Context, flt store.ServerFilter) ([]*model.Server, error) {
	i := p.calls
	p.calls++
	if i < len(p.pages) {
		return p.pages[i], nil
	}
	return nil, nil
}

// TestListServersAllPaginates pins the page-through contract: a fleet
// larger than one page is fully collected instead of silently stopping at
// the first page.
func TestListServersAllPaginates(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	mk := func(from, to int) []*model.Server {
		page := make([]*model.Server, 0, to-from)
		for i := from; i < to; i++ {
			srv := &model.Server{
				ID:       fmt.Sprintf("srv-%04d", i),
				Name:     fmt.Sprintf("srv-%04d", i),
				Type:     "game",
				Region:   "cn-east",
				Endpoint: model.Endpoint{Host: "127.0.0.1", Port: 30000 + i},
			}
			page = append(page, srv)
		}
		return page
	}
	pager := &pagingServers{pages: [][]*model.Server{mk(0, 500), mk(500, 501)}}
	svc := New(mem, pager, nil, nil)

	got, err := svc.listServersAll(ctx)
	if err != nil {
		t.Fatalf("listServersAll: %v", err)
	}
	if len(got) != 501 {
		t.Fatalf("listServersAll = %d servers, want 501 (full page + remainder)", len(got))
	}
	if got[0].ID != "srv-0000" || got[500].ID != "srv-0500" {
		t.Fatalf("page order broken: first=%s last=%s", got[0].ID, got[500].ID)
	}
}

// TestListServersAllPropagatesStoreError pins the error path behind the
// degrade: the pager itself reports the failure, receivers decides what to
// do with it.
func TestListServersAllPropagatesStoreError(t *testing.T) {
	mem := memory.New()
	svc := New(mem, &failingServers{err: errors.New("boom")}, nil, nil)
	if _, err := svc.listServersAll(context.Background()); err == nil {
		t.Fatal("listServersAll store error: want propagation, got nil")
	}
}
