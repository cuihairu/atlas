// Error-path and edge coverage for routing: listing failures through
// Recommend (strict pass, fallback pass) and Diagnose, the pagination
// cursor beyond the store cap, the strict-mismatch verdict reasons
// (version / platform / over-capacity no-match), the empty-candidates
// merge guard, overlapping maintenance windows (tie does not beat), the
// owned-servers lookup failure, and the load-tie reason scan.
package routing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// faultRoutingStore fails the fleet listing and the account character
// listing on demand; everything else delegates to the backing store.
type faultRoutingStore struct {
	store.Store
	listServersFn func(ctx context.Context, f store.ServerFilter) ([]*model.Server, error)
	listCharsFn   func(ctx context.Context, id int64) ([]*model.Character, error)
}

func (f *faultRoutingStore) ListServers(ctx context.Context, sf store.ServerFilter) ([]*model.Server, error) {
	if f.listServersFn != nil {
		return f.listServersFn(ctx, sf)
	}
	return f.Store.ListServers(ctx, sf)
}

func (f *faultRoutingStore) ListCharactersByAccount(ctx context.Context, id int64) ([]*model.Character, error) {
	if f.listCharsFn != nil {
		return f.listCharsFn(ctx, id)
	}
	return f.Store.ListCharactersByAccount(ctx, id)
}

// TestRecommendListingFailures pins the fail-closed contract: a fleet
// listing that fails in the strict pass or the fallback pass aborts the
// recommendation — never a 404 that hides a store outage.
func TestRecommendListingFailures(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()
	seed(t, mem, "r-1", "eu", model.StatusOnline, 1, 0.1, 100)

	fs := &faultRoutingStore{Store: mem}
	svc := New(fs, fs, fs, fs)

	// Strict pass: the request itself fails to list.
	fs.listServersFn = func(context.Context, store.ServerFilter) ([]*model.Server, error) {
		return nil, errFault
	}
	if _, _, err := svc.Recommend(ctx, Request{Region: "eu"}); !errors.Is(err, errFault) {
		t.Fatalf("recommend strict listing failure = %v, want errFault", err)
	}

	// Fallback pass: the strict request lists fine (and matches nothing),
	// the relaxed one fails on its second listing call.
	calls := 0
	fs.listServersFn = func(c context.Context, sf store.ServerFilter) ([]*model.Server, error) {
		calls++
		if calls == 1 {
			// Region "nowhere" matches nothing → strict pass is empty.
			return f_delegated(ctx, fs, sf)
		}
		return nil, errFault
	}
	if _, _, err := svc.Recommend(ctx, Request{Region: "nowhere"}); !errors.Is(err, errFault) {
		t.Fatalf("recommend fallback listing failure = %v, want errFault", err)
	}
}

// f_delegated runs the backing store's listing for the strict pass.
func f_delegated(ctx context.Context, f *faultRoutingStore, sf store.ServerFilter) ([]*model.Server, error) {
	return f.Store.ListServers(ctx, sf)
}

// TestDiagnoseListingFailure pins the diagnosis listing wrap.
func TestDiagnoseListingFailure(t *testing.T) {
	mem := memory.New()
	fs := &faultRoutingStore{Store: mem, listServersFn: func(context.Context, store.ServerFilter) ([]*model.Server, error) {
		return nil, errFault
	}}
	svc := New(fs, fs, fs, fs)

	if _, err := svc.Diagnose(context.Background(), Request{}); !errors.Is(err, errFault) {
		t.Fatalf("diagnose with failing listing = %v, want errFault", err)
	}
}

// TestListMatchingPagesPastStoreCap pins the cursor advance: a fleet
// larger than the store's per-call cap is walked in full, so server 201
// is still a candidate.
func TestListMatchingPagesPastStoreCap(t *testing.T) {
	mem := memory.New()
	for i := 0; i < store.ListServersMaxLimit+1; i++ {
		seed(t, mem, fmt.Sprintf("pg-%04d", i), "eu", model.StatusOnline, 0, 0.1, 100)
	}
	svc := New(mem, mem, mem, mem)

	srv, _, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("recommend across pages: %v", err)
	}
	if srv == nil || srv.ID != "pg-0000" {
		t.Fatalf("winner = %v, want the first ID of the paged walk", srv)
	}
}

// TestDiagnoseStrictMismatchReasons pins the verdict reasons for the
// profile dimensions (version, platform) and the candidacy/eligibility
// split: an over-capacity server still matches the strict filters (rank
// and ranking reason) while its Eligible flag flips off on live runtime.
func TestDiagnoseStrictMismatchReasons(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()

	reg := func(id, version, platform string, players, capacity int) {
		if err := mem.RegisterServer(ctx, &model.Server{
			ID: id, Name: id, Region: "eu", Version: version, Platform: platform,
			Status: model.StatusOnline, Capacity: capacity,
		}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
		if players > 0 {
			if err := mem.RecordHeartbeat(ctx, id, model.Heartbeat{Players: players, Load: float64(players) / float64(capacity)}); err != nil {
				t.Fatalf("heartbeat %s: %v", id, err)
			}
		}
	}
	reg("ver-old", "1.0.0", "gamebox", 1, 100)    // version mismatch
	reg("plat-pc", "9.9.9", "pc", 1, 100)         // platform mismatch
	reg("overfull", "9.9.9", "gamebox", 150, 100) // matches filters, over capacity

	svc := New(mem, mem, mem, mem)
	d, err := svc.Diagnose(ctx, Request{Version: "9.9.9", Platform: "gamebox"})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	verdicts := map[string]ServerVerdict{}
	for _, v := range d.Servers {
		verdicts[v.Server.ID] = v
	}
	if got := verdicts["ver-old"].Reason; !strings.Contains(got, "version=1.0.0") {
		t.Fatalf("version mismatch reason = %q", got)
	}
	if got := verdicts["plat-pc"].Reason; !strings.Contains(got, "platform=pc") {
		t.Fatalf("platform mismatch reason = %q", got)
	}
	// Over capacity is an eligibility fact, not a candidacy filter: the
	// server ranks and carries the ranking reason, with Eligible=false.
	ov := verdicts["overfull"]
	if ov.Rank != 1 || ov.Reason != ReasonLowestLoad {
		t.Fatalf("over-capacity verdict rank/reason = %d/%q, want 1/lowest_load", ov.Rank, ov.Reason)
	}
	if ov.Eligible {
		t.Fatal("over-capacity server must be ineligible")
	}
}

// TestMergeRuntimesEmpty pins the empty guard: no candidates means no
// runtime read at all.
func TestMergeRuntimesEmpty(t *testing.T) {
	svc := New(memory.New(), memory.New(), memory.New(), memory.New())
	live, err := svc.mergeRuntimes(context.Background(), nil)
	if live != nil || err != nil {
		t.Fatalf("mergeRuntimes(nil) = %v/%v, want nil/nil", live, err)
	}
}

// TestOverlappingWindowsTieDoesNotBeat pins the blocklist conflict rule:
// a second blocking window that does not beat the recorded one (same
// activity, no earlier start) never replaces it.
func TestOverlappingWindowsTieDoesNotBeat(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()
	base := time.Now()
	start := base.Add(-time.Hour)
	end := base.Add(time.Hour)
	for _, id := range []string{"w-first", "w-second"} {
		if err := mem.CreateMaintenanceWindow(ctx, &model.MaintenanceWindow{
			ID: id, ServerID: "ov-srv", StartAt: start, EndAt: end,
		}); err != nil {
			t.Fatalf("create window %s: %v", id, err)
		}
	}

	svc := New(mem, mem, mem, mem).withClock(func() time.Time { return base })
	blocked, err := svc.maintenanceBlocklist(ctx, base)
	if err != nil {
		t.Fatalf("blocklist: %v", err)
	}
	if len(blocked) != 1 {
		t.Fatalf("blocked = %d entries, want exactly one winner", len(blocked))
	}
}

// TestWindowBeatsTieBreaker pins the comparator directly: equal activity
// falls through to the earlier start, in both directions.
func TestWindowBeatsTieBreaker(t *testing.T) {
	now := time.Now()
	early := &model.MaintenanceWindow{StartAt: now.Add(-2 * time.Hour), EndAt: now.Add(2 * time.Hour)}
	late := &model.MaintenanceWindow{StartAt: now.Add(-1 * time.Hour), EndAt: now.Add(2 * time.Hour)}

	if !windowBeats(early, late, now) {
		t.Fatal("earlier start must beat the later window")
	}
	if windowBeats(late, early, now) {
		t.Fatal("later start must not beat the earlier window")
	}
}

// TestOwnedServersLookupFailure pins the degrade rule: an unreadable
// character index yields no ownership hints (nil), never an error — the
// recommendation proceeds ownership-blind rather than failing.
func TestOwnedServersLookupFailure(t *testing.T) {
	mem := memory.New()
	fs := &faultRoutingStore{Store: mem, listCharsFn: func(context.Context, int64) ([]*model.Character, error) {
		return nil, errFault
	}}
	svc := New(fs, fs, fs, fs)

	if owned := svc.ownedServers(context.Background(), 42); owned != nil {
		t.Fatalf("owned servers after lookup failure = %v, want nil", owned)
	}
}

// TestReasonForSkipsHigherLoadInTieScan pins the tie scan: with two
// candidates tied at the lowest load and one straggler above it, the
// capacity comparison skips the straggler and the reason names capacity.
func TestReasonForSkipsHigherLoadInTieScan(t *testing.T) {
	mem := memory.New()
	seed(t, mem, "tie-big", "eu", model.StatusOnline, 20, 0.2, 1000)
	seed(t, mem, "tie-small", "eu", model.StatusOnline, 20, 0.2, 100)
	seed(t, mem, "straggler", "eu", model.StatusOnline, 90, 0.9, 5000)

	svc := New(mem, mem, mem, mem)
	srv, reason, err := svc.Recommend(context.Background(), Request{Region: "eu"})
	if err != nil || srv == nil {
		t.Fatalf("recommend: %v/%v", srv, err)
	}
	if srv.ID != "tie-big" {
		t.Fatalf("winner = %s, want the tied candidate with capacity headroom", srv.ID)
	}
	if reason != ReasonHighestCapacity {
		t.Fatalf("reason = %q, want the capacity tie-break", reason)
	}
}
