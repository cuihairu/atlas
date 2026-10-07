// Package routing implements the server recommendation logic described in
// TODO v0.1.4: filter by region/version/platform/status, rank by load and
// remaining capacity, tie-break on the account's existing character.
package routing

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
)

// Recommendation reasons (API contract).
const (
	ReasonLowestLoad      = "lowest_load"
	ReasonHighestCapacity = "highest_capacity"
	ReasonHasCharacter    = "has_character"
	ReasonFallback        = "fallback"
)

// DefaultPreMaintenanceLead is how far ahead of a maintenance window start
// recommendations stop steering players to that server (维护前引导): the
// server is still "online" — the health monitor only flips it at start_at —
// but landing there means the session gets dropped minutes into play.
const DefaultPreMaintenanceLead = 5 * time.Minute

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
	// windows supplies maintenance windows; a server with an active or
	// imminent window is excluded from recommendations (安全约束). nil
	// means window awareness is off — only used by tests, never in prod.
	windows store.MaintenanceWindowStore

	// now is the clock seam for window arithmetic (tests freeze it).
	now func() time.Time
	// maintLead is the pre-maintenance steering horizon: a window starting
	// within this lead disqualifies its server even though the monitor has
	// not flipped it to maintenance yet. 0 = only active windows block.
	maintLead time.Duration
}

// New creates a routing service.
func New(servers store.ServerStore, runtime store.RuntimeStore, characters store.CharacterStore, windows store.MaintenanceWindowStore) *Service {
	return &Service{
		servers:    servers,
		runtime:    runtime,
		characters: characters,
		windows:    windows,
		now:        time.Now,
		maintLead:  DefaultPreMaintenanceLead,
	}
}

// WithMaintenanceLead overrides the pre-maintenance steering horizon
// (default DefaultPreMaintenanceLead). A non-positive lead disables the
// upcoming-window exclusion; active windows still block.
func (s *Service) WithMaintenanceLead(d time.Duration) *Service {
	if d > 0 {
		s.maintLead = d
	} else {
		s.maintLead = 0
	}
	return s
}

// withClock freezes the window arithmetic for tests.
func (s *Service) withClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Recommend picks the best server for the request and explains why.
//
// Selection order: strict filters first; if they match nothing, filters are
// relaxed to status-only (reason "fallback") so matchmaking never starves.
// Among candidates: existing character beats all, then lowest load, then
// highest remaining capacity, then stable server ID for determinism.
func (s *Service) Recommend(ctx context.Context, req Request) (*model.Server, string, error) {
	ctx, span := atlastracing.Start(ctx, "routing.recommend")
	defer span.End()
	if req.Status == "" {
		req.Status = model.StatusOnline
	}

	// 维护前引导 (TODO v0.2+ candidate): servers with an active maintenance
	// window — or one starting within the lead — are not safe landing spots,
	// no matter how the rest of the request filters look. Like status, the
	// window exclusion applies to the fallback pass too: recommending a
	// server that drops sessions minutes after join is worse than 404.
	now := s.now()
	blocked, err := s.maintenanceBlocklist(ctx, now)
	if err != nil {
		return nil, "", err
	}

	candidates, err := s.listMatching(ctx, req)
	if err != nil {
		return nil, "", err
	}
	candidates = withoutBlocked(candidates, blocked)
	strict := len(candidates) > 0
	if !strict {
		candidates, err = s.listMatching(ctx, Request{Status: req.Status})
		if err != nil {
			return nil, "", err
		}
		candidates = withoutBlocked(candidates, blocked)
		if len(candidates) == 0 {
			return nil, "", fmt.Errorf("no %s server available: %w", req.Status, model.ErrNotFound)
		}
	}

	// Merge runtime player counts and load so scoring sees live data.
	if err := s.mergeRuntimes(ctx, candidates); err != nil {
		return nil, "", err
	}

	owned := s.ownedServers(ctx, req.AccountID)
	sortCandidates(candidates, owned)

	winner := candidates[0]
	return winner, s.reasonFor(candidates, winner, owned, strict), nil
}

// ── 玩家视角排查 (管理台「排查/诊断」) ────────────────────────────────
//
// Diagnose runs the exact Recommend pipeline with every intermediate result
// exposed: which servers the strict filters kept, whether the fallback pass
// took over, per-server eligibility for a login right now, the ranking, and
// the winner's reason. It calls the same listMatching predicate, the same
// runtime merge, the same ownedServers tiebreak, the same sortCandidates and
// reasonFor — 摊开中间结果, never a second criteria set (the 排查页 decree).

// Diagnosis is the full walkthrough of one recommendation request.
type Diagnosis struct {
	// Request as normalized (Status defaulted to online when empty).
	Request Request `json:"request"`
	// Stage: "strict" (filters matched), "fallback" (status-only pass took
	// over), or "none" (no server matched even status-only).
	Stage string `json:"stage"`
	// Servers covers the whole fleet with per-server verdicts, candidates
	// first (rank ascending), then the rejected with reasons.
	Servers []ServerVerdict `json:"servers"`
	// WinnerID / WinnerReason name the server Recommend would return (the
	// top-ranked eligible candidate) and why. Empty when none.
	WinnerID     string `json:"winner_id,omitempty"`
	WinnerReason string `json:"winner_reason,omitempty"`
}

// ServerVerdict explains one server's place in the decision.
type ServerVerdict struct {
	Server *model.Server `json:"server"`
	// Rank is the 1-based position among the sorted candidates; 0 = not a
	// candidate (see Reason for why).
	Rank int `json:"rank"`
	// MatchedStrict / MatchedFallback report which pass kept the server.
	MatchedStrict   bool `json:"matched_strict"`
	MatchedFallback bool `json:"matched_fallback"`
	// Owned: the account already has a character here (tiebreak #1).
	Owned bool `json:"owned"`
	// MaintenanceWindow is the window disqualifying this server right now
	// (active, or starting within the pre-maintenance lead); nil when none.
	MaintenanceWindow *model.MaintenanceWindow `json:"maintenance_window,omitempty"`
	// Eligible: the server would accept this login right now — status
	// accepts traffic, headroom remains, and no maintenance window blocks.
	Eligible bool `json:"eligible"`
	// Reason names the rejection cause for non-candidates ("status=maintenance"),
	// or, for the winner, the ranking reason from reasonFor.
	Reason string `json:"reason,omitempty"`
}

// Diagnose explains what Recommend would do for this request, server by
// server. The whole fleet is listed (cursor-paginated at the store's
// list cap) so the rejected servers carry their rejection reason instead
// of silently disappearing.
func (s *Service) Diagnose(ctx context.Context, req Request) (*Diagnosis, error) {
	ctx, span := atlastracing.Start(ctx, "routing.diagnose")
	defer span.End()
	if req.Status == "" {
		req.Status = model.StatusOnline
	}

	now := s.now()
	blocked, err := s.maintenanceBlocklist(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("list maintenance windows: %w", err)
	}

	// The whole fleet, cursor-paginated at the store's list cap: one bare
	// call truncates at the cap, and servers past it would silently vanish
	// from the diagnosis — the exact disappearance this endpoint exists
	// to rule out.
	var all []*model.Server
	cursor := ""
	for {
		page, err := s.servers.ListServers(ctx, store.ServerFilter{Limit: store.ListServersMaxLimit, Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("list servers: %w", err)
		}
		all = append(all, page...)
		if len(page) < store.ListServersMaxLimit {
			break
		}
		cursor = page[len(page)-1].ID
	}

	// The same predicate listMatching pushes into the store filter — applied
	// locally so non-matching servers stay visible with a reason. The
	// maintenance window exclusion rides along the same way it does in
	// Recommend: a hard constraint applied to both passes.
	matchStrict := func(srv *model.Server) bool {
		return blocked[srv.ID] == nil &&
			srv.Status == req.Status &&
			(req.Region == "" || srv.Region == req.Region) &&
			(req.Version == "" || srv.Version == req.Version) &&
			(req.Platform == "" || srv.Platform == req.Platform)
	}
	matchFallback := func(srv *model.Server) bool {
		return blocked[srv.ID] == nil && srv.Status == req.Status
	}

	// Runtime merge for every server (the merge Recommend applies to its
	// candidates): eligibility must be judged on live players/load too.
	if err := s.mergeRuntimes(ctx, all); err != nil {
		return nil, err
	}

	var candidates []*model.Server
	stage := "strict"
	for _, srv := range all {
		if matchStrict(srv) {
			candidates = append(candidates, srv)
		}
	}
	if len(candidates) == 0 {
		stage = "fallback"
		for _, srv := range all {
			if matchFallback(srv) {
				candidates = append(candidates, srv)
			}
		}
		if len(candidates) == 0 {
			stage = "none"
		}
	}

	owned := s.ownedServers(ctx, req.AccountID)
	sortCandidates(candidates, owned)

	d := &Diagnosis{Request: req, Stage: stage}
	inCandidates := make(map[string]int, len(candidates))
	for i, srv := range candidates {
		inCandidates[srv.ID] = i + 1
	}

	for _, srv := range all {
		v := ServerVerdict{
			Server:            srv,
			Rank:              inCandidates[srv.ID],
			MatchedStrict:     matchStrict(srv),
			MatchedFallback:   matchFallback(srv),
			Owned:             owned[srv.ID],
			MaintenanceWindow: blocked[srv.ID],
			Eligible:          srv.Status.AcceptsTraffic() && !srv.OverCapacity() && blocked[srv.ID] == nil,
		}
		switch {
		case v.Rank > 0:
			// Candidate: the winner carries the ranking reason.
			if v.Rank == 1 {
				v.Reason = s.reasonFor(candidates, srv, owned, stage == "strict")
			}
		default:
			// Not a candidate: name the first failing dimension of the
			// strict request (status and maintenance window outrank the
			// profile filters). Even a server that would match the fallback
			// pass gets its strict rejection reason — the fallback only runs
			// when strict matched nothing at all.
			switch {
			case srv.Status != req.Status:
				v.Reason = fmt.Sprintf("status=%s (需要 %s)", srv.Status, req.Status)
			case blocked[srv.ID] != nil:
				if blocked[srv.ID].Active(now) {
					v.Reason = "maintenance_window=active"
				} else {
					v.Reason = "maintenance_window=upcoming"
				}
			case req.Region != "" && srv.Region != req.Region:
				v.Reason = fmt.Sprintf("region=%s (需要 %s)", srv.Region, req.Region)
			case req.Version != "" && srv.Version != req.Version:
				v.Reason = fmt.Sprintf("version=%s (需要 %s)", srv.Version, req.Version)
			case req.Platform != "" && srv.Platform != req.Platform:
				v.Reason = fmt.Sprintf("platform=%s (需要 %s)", srv.Platform, req.Platform)
			default:
				v.Reason = "no match"
			}
		}
		d.Servers = append(d.Servers, v)
	}

	if len(candidates) > 0 {
		winner := candidates[0]
		d.WinnerID = winner.ID
		d.WinnerReason = s.reasonFor(candidates, winner, owned, stage == "strict")
	}
	return d, nil
}

// listMatching applies strict request filters to the complete filtered
// set, cursor-paginated at the store's list cap. A bare call would take
// the store's 50-row default page and silently drop every candidate past
// it — with 60+ eligible servers only the lowest IDs would ever be
// recommended.
func (s *Service) listMatching(ctx context.Context, req Request) ([]*model.Server, error) {
	var out []*model.Server
	cursor := ""
	for {
		page, err := s.servers.ListServers(ctx, store.ServerFilter{
			Status:   req.Status,
			Region:   req.Region,
			Version:  req.Version,
			Platform: req.Platform,
			Limit:    store.ListServersMaxLimit,
			Cursor:   cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("list servers: %w", err)
		}
		out = append(out, page...)
		if len(page) < store.ListServersMaxLimit {
			return out, nil
		}
		cursor = page[len(page)-1].ID
	}
}

// mergeRuntimes folds live players/load into the given servers with one
// batched read (the same GetRuntimes call Discovery's list path uses —
// one Redis pipeline exec instead of N single-key round trips). Missing
// keys are simply skipped: no snapshot means the server has not
// heartbeated, and its archive values stand. The error propagates —
// this is a decision surface, not a display: ranking candidates on
// unknown load risks steering players into a full or dying server,
// the same fail-closed rule the maintenance blocklist follows.
func (s *Service) mergeRuntimes(ctx context.Context, servers []*model.Server) error {
	if len(servers) == 0 {
		return nil
	}
	ids := make([]string, len(servers))
	for i, srv := range servers {
		ids[i] = srv.ID
	}
	rtMap, err := s.runtime.GetRuntimes(ctx, ids)
	if err != nil {
		return fmt.Errorf("read runtimes: %w", err)
	}
	for _, srv := range servers {
		if rt, ok := rtMap[srv.ID]; ok {
			srv.Players = rt.Players
			srv.Load = rt.Load
		}
	}
	return nil
}

// maintenanceBlocklist returns, per server, the maintenance window that
// disqualifies it from recommendations: an active window wins over an
// upcoming one, earlier start wins over later. One list call covers the
// whole fleet — windows are transient and few, and the health monitor
// already lists them the same way per sweep. An empty result (or nil
// window store) means no server is blocked. Failures propagate: a
// recommendation built on unknown window state is untrustworthy, so we
// fail closed rather than steer into a server that may be about to fall.
func (s *Service) maintenanceBlocklist(ctx context.Context, now time.Time) (map[string]*model.MaintenanceWindow, error) {
	if s.windows == nil {
		return nil, nil
	}
	windows, err := s.windows.ListMaintenanceWindows(ctx, "", 0)
	if err != nil {
		return nil, fmt.Errorf("list maintenance windows: %w", err)
	}
	blocked := make(map[string]*model.MaintenanceWindow, len(windows))
	for _, w := range windows {
		if !windowBlocks(w, now, s.maintLead) {
			continue
		}
		if cur, ok := blocked[w.ServerID]; ok && !windowBeats(w, cur, now) {
			continue
		}
		blocked[w.ServerID] = w
	}
	return blocked, nil
}

// windowBlocks reports whether the window disqualifies its server: it
// covers now, or it starts within the lead horizon. An ended-but-not-yet-
// swept window does not block — the monitor has restored the server even
// if it has not deleted the record yet.
func windowBlocks(w *model.MaintenanceWindow, now time.Time, lead time.Duration) bool {
	if w.Active(now) {
		return true
	}
	return lead > 0 && w.StartAt.After(now) && w.StartAt.Sub(now) <= lead
}

// windowBeats picks the more immediate blocker for the same server: active
// outranks upcoming, earlier start outranks later.
func windowBeats(a, b *model.MaintenanceWindow, now time.Time) bool {
	if a.Active(now) != b.Active(now) {
		return a.Active(now)
	}
	return a.StartAt.Before(b.StartAt)
}

// withoutBlocked drops servers disqualified by a maintenance window,
// keeping the untouched slice when nothing is blocked.
func withoutBlocked(candidates []*model.Server, blocked map[string]*model.MaintenanceWindow) []*model.Server {
	if len(blocked) == 0 {
		return candidates
	}
	out := make([]*model.Server, 0, len(candidates))
	for _, srv := range candidates {
		if blocked[srv.ID] == nil {
			out = append(out, srv)
		}
	}
	return out
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
