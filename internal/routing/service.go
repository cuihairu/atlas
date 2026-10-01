// Package routing implements the server recommendation logic described in
// TODO v0.1.4: filter by region/version/platform/status, rank by load and
// remaining capacity, tie-break on the account's existing character.
package routing

import (
	"context"
	"fmt"
	"sort"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Recommendation reasons (API contract).
const (
	ReasonLowestLoad      = "lowest_load"
	ReasonHighestCapacity = "highest_capacity"
	ReasonHasCharacter    = "has_character"
	ReasonFallback        = "fallback"
)

// Request describes what kind of server to recommend.
type Request struct {
	// AccountID, when set, prefers servers where the account already owns
	// a character.
	AccountID int64

	// Region/Version/Platform are strict filters; empty means "any".
	Region   string
	Version  string
	Platform string

	// Status defaults to online.
	Status model.ServerStatus
}

// Service recommends a server for a player session.
type Service struct {
	servers    store.ServerStore
	runtime    store.RuntimeStore
	characters store.CharacterStore
}

// New creates a routing service.
func New(servers store.ServerStore, runtime store.RuntimeStore, characters store.CharacterStore) *Service {
	return &Service{servers: servers, runtime: runtime, characters: characters}
}

// Recommend picks the best server for the request and explains why.
//
// Selection order: strict filters first; if they match nothing, filters are
// relaxed to status-only (reason "fallback") so matchmaking never starves.
// Among candidates: existing character beats all, then lowest load, then
// highest remaining capacity, then stable server ID for determinism.
func (s *Service) Recommend(ctx context.Context, req Request) (*model.Server, string, error) {
	if req.Status == "" {
		req.Status = model.StatusOnline
	}

	candidates, err := s.listMatching(ctx, req)
	if err != nil {
		return nil, "", err
	}
	strict := len(candidates) > 0
	if !strict {
		candidates, err = s.listMatching(ctx, Request{Status: req.Status})
		if err != nil {
			return nil, "", err
		}
		if len(candidates) == 0 {
			return nil, "", fmt.Errorf("no %s server available: %w", req.Status, model.ErrNotFound)
		}
	}

	// Merge runtime player counts and load so scoring sees live data.
	for _, srv := range candidates {
		if rt, err := s.runtime.GetRuntime(ctx, srv.ID); err == nil {
			srv.Players = rt.Players
			srv.Load = rt.Load
		}
	}

	owned := s.ownedServers(ctx, req.AccountID)
	sortCandidates(candidates, owned)

	winner := candidates[0]
	return winner, s.reasonFor(candidates, winner, owned, strict), nil
}

// listMatching applies strict request filters.
func (s *Service) listMatching(ctx context.Context, req Request) ([]*model.Server, error) {
	servers, err := s.servers.ListServers(ctx, store.ServerFilter{
		Status:   req.Status,
		Region:   req.Region,
		Version:  req.Version,
		Platform: req.Platform,
	})
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	return servers, nil
}

// ownedServers returns the set of server IDs the account already has a
// character on. Empty account or lookup failure means "no tiebreak data".
func (s *Service) ownedServers(ctx context.Context, accountID int64) map[string]bool {
	if accountID <= 0 || s.characters == nil {
		return nil
	}
	chars, err := s.characters.ListCharactersByAccount(ctx, accountID)
	if err != nil {
		return nil
	}
	owned := make(map[string]bool, len(chars))
	for _, ch := range chars {
		owned[ch.ServerID] = true
	}
	return owned
}

// sortCandidates orders candidates best-first.
func sortCandidates(candidates []*model.Server, owned map[string]bool) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]

		// 1. Servers where the account already plays come first.
		if (owned[a.ID] && !owned[b.ID]) || (!owned[a.ID] && owned[b.ID]) {
			return owned[a.ID]
		}

		// 2. Lower load wins.
		if a.Load != b.Load {
			return a.Load < b.Load
		}

		// 3. More remaining capacity wins.
		if ra, rb := remaining(a), remaining(b); ra != rb {
			return ra > rb
		}

		// 4. Deterministic tiebreak.
		return a.ID < b.ID
	})
}

// remaining returns how many more players the server can take.
func remaining(s *model.Server) int {
	return s.Capacity - s.Players
}

// reasonFor explains the winner's selection.
func (s *Service) reasonFor(candidates []*model.Server, winner *model.Server, owned map[string]bool, strict bool) string {
	if owned[winner.ID] {
		return ReasonHasCharacter
	}
	if !strict {
		return ReasonFallback
	}

	// Was the win decided by load or by capacity headroom?
	minLoad := winner.Load
	tiedOnLoad := 0
	for _, srv := range candidates {
		if srv.Load == minLoad {
			tiedOnLoad++
		}
	}
	if tiedOnLoad == 1 {
		return ReasonLowestLoad
	}

	// Tied on load: capacity decides among the tied leaders.
	bestRemaining := remaining(winner)
	unique := true
	for _, srv := range candidates {
		if srv.Load != minLoad {
			continue
		}
		if srv.ID == winner.ID {
			continue
		}
		if remaining(srv) >= bestRemaining {
			unique = false
			break
		}
	}
	if unique {
		return ReasonHighestCapacity
	}
	return ReasonLowestLoad
}
