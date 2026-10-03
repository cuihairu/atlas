package crossserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/event"
	httpEvent "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func testSpec() model.CrossServerSpec {
	return model.CrossServerSpec{
		Topology: model.CrossServerTopology{Clusters: []model.CrossServerCluster{
			{ID: "cluster-ea", Name: "华东战场", Region: "cn-east", Servers: []string{"game-1001", "game-1002"}},
		}},
		Groups: []model.CrossServerGroup{
			{ID: "season-1", Name: "第一期", Servers: []string{"game-1001"}},
		},
		Features:     map[string]bool{"cross_battlefield": true},
		MatchDomains: []model.CrossServerMatchDomain{},
		CrossPlayTypes: []model.CrossPlayType{
			{ID: "battlefield", Name: "跨服战场", Lifecycle: model.CrossPlayLifecycleSeasonal, Matchmaking: true, Ranking: true, IDPrefix: "xb"},
		},
	}
}

func newSvc(t *testing.T, events event.EventAdapter) (*Service, *memory.Store) {
	t.Helper()
	mem := memory.New()
	return New(mem, mem, events, nil), mem
}

func TestGetStrictVersusSnapshot(t *testing.T) {
	svc, _ := newSvc(t, nil)

	if _, err := svc.Get(context.Background()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get before publish = %v, want ErrNotFound (strict startup pull)", err)
	}
	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Version != 0 {
		t.Errorf("empty snapshot version = %d, want 0", snap.Version)
	}

	if _, err := svc.Save(context.Background(), testSpec()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get after publish: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("first publish version = %d, want 1", got.Version)
	}
}

func TestSinceConditionalPull(t *testing.T) {
	svc, _ := newSvc(t, nil)
	if _, err := svc.Save(context.Background(), testSpec()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ctx := context.Background()

	// Nothing in hand: changed.
	if _, changed, err := svc.Since(ctx, 0, ""); err != nil || !changed {
		t.Fatalf("Since(0,\"\") = changed=%v err=%v, want true/nil", changed, err)
	}
	// Up to date: version covers it.
	if _, changed, err := svc.Since(ctx, 1, ""); err != nil || changed {
		t.Fatalf("Since(1,\"\") = changed=%v err=%v, want false/nil", changed, err)
	}
	// Same content by hash: changed=false even for a stale version number.
	cur, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := svc.Since(ctx, 0, cur.Hash); err != nil || changed {
		t.Fatalf("Since(0,hash) = changed=%v err=%v, want false/nil", changed, err)
	}
}

func TestSavePublishesBusSignalAndBumpsVersion(t *testing.T) {
	adapter := httpEvent.New()
	var mu sync.Mutex
	var got []*event.Event
	_ = adapter.Subscribe(context.Background(), event.TopicConfig, func(_ context.Context, e *event.Event) error {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
		return nil
	})
	svc, _ := newSvc(t, adapter)

	res, err := svc.Save(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Config.Version != 1 {
		t.Errorf("version = %d, want 1", res.Config.Version)
	}
	if res.Notify.Idempotent {
		t.Error("first save must not be idempotent")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("bus signals = %d, want 1", len(got))
	}
	e := got[0]
	if e.Type != event.EventConfigUpdated {
		t.Errorf("signal type = %q", e.Type)
	}
	if e.ConfigVersion != 1 || e.ConfigHash != res.Config.Hash {
		t.Errorf("signal carries version=%d hash=%q, want 1/%s", e.ConfigVersion, e.ConfigHash, res.Config.Hash)
	}
	if len(e.ConfigTargets) == 0 {
		t.Error("signal has no targets (first publish must interest everyone)")
	}
}

func TestSaveIdempotentSameContent(t *testing.T) {
	adapter := httpEvent.New()
	var mu sync.Mutex
	signals := 0
	_ = adapter.Subscribe(context.Background(), event.TopicConfig, func(_ context.Context, _ *event.Event) error {
		mu.Lock()
		signals++
		mu.Unlock()
		return nil
	})
	svc, _ := newSvc(t, adapter)
	ctx := context.Background()

	if _, err := svc.Save(ctx, testSpec()); err != nil {
		t.Fatalf("first save: %v", err)
	}
	res, err := svc.Save(ctx, testSpec())
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if !res.Notify.Idempotent {
		t.Error("identical content must be idempotent")
	}
	if res.Config.Version != 1 {
		t.Errorf("version moved to %d on idempotent save, want 1", res.Config.Version)
	}
	mu.Lock()
	defer mu.Unlock()
	if signals != 1 {
		t.Errorf("bus signals = %d, want 1 (no signal for idempotent save)", signals)
	}
}

func TestSaveMonotonicVersion(t *testing.T) {
	svc, _ := newSvc(t, nil)
	ctx := context.Background()
	for i, featureVal := range []bool{true, false, true} {
		spec := testSpec()
		spec.Features["cross_battlefield"] = featureVal
		res, err := svc.Save(ctx, spec)
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if res.Config.Version != i+1 {
			t.Errorf("save %d → version %d, want %d", i, res.Config.Version, i+1)
		}
	}
}

func TestSaveRejectsInvalidSpec(t *testing.T) {
	svc, _ := newSvc(t, nil)

	spec := testSpec()
	spec.Topology.Clusters = append(spec.Topology.Clusters, model.CrossServerCluster{ID: "cluster-ea"})
	if _, err := svc.Save(context.Background(), spec); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("duplicate cluster id: err = %v, want ErrInvalid", err)
	}
}

func TestDiffTargets(t *testing.T) {
	old := model.NormalizeCrossServerSpec(testSpec())
	updated := model.NormalizeCrossServerSpec(testSpec())
	updated.Topology.Clusters[0].Servers = []string{"game-1001"} // game-1002 dropped

	got := diffTargets(old, updated)
	if len(got) != 1 || got[0] != "cluster-ea" {
		t.Errorf("targets = %v, want [cluster-ea]", got)
	}

	// Feature flips are global.
	flipped := model.NormalizeCrossServerSpec(testSpec())
	flipped.Features["cross_battlefield"] = false
	got = diffTargets(old, flipped)
	if len(got) != 1 || got[0] != model.TargetAll {
		t.Errorf("feature flip targets = %v, want [*]", got)
	}

	// Unrelated change on one section scopes the signal to that section.
	other := model.NormalizeCrossServerSpec(testSpec())
	other.Groups = append(other.Groups, model.CrossServerGroup{ID: "season-2", Servers: []string{"game-3001"}})
	got = diffTargets(old, other)
	if len(got) != 1 || got[0] != "season-2" {
		t.Errorf("new group targets = %v, want [season-2]", got)
	}

	// The cross-play type table is a fleet-wide declaration: any edit
	// (add / rename / field flip) is global, like a feature switch.
	typed := model.NormalizeCrossServerSpec(testSpec())
	typed.CrossPlayTypes[0].Ranking = !typed.CrossPlayTypes[0].Ranking
	got = diffTargets(old, typed)
	if len(got) != 1 || got[0] != model.TargetAll {
		t.Errorf("type table edit targets = %v, want [*]", got)
	}

	// ...and an unchanged type table stays out of the signal when only
	// a section changed: the diff must not escalate scoped edits.
	scoped := model.NormalizeCrossServerSpec(testSpec())
	scoped.MatchDomains = append(scoped.MatchDomains, model.CrossServerMatchDomain{ID: "md-2", Servers: []string{"game-3001"}})
	got = diffTargets(old, scoped)
	if len(got) != 1 || got[0] != "md-2" {
		t.Errorf("new domain targets = %v, want [md-2]", got)
	}
}

func registerCallbackServer(t *testing.T, mem *memory.Store, id, url string, status model.ServerStatus) {
	t.Helper()
	srv := &model.Server{
		ID: id, Name: id, Type: "game", Region: "cn-east",
		Endpoint: model.Endpoint{Host: "127.0.0.1", Port: 30001},
		Capacity: 100, NotifyMode: model.NotifyModeCallback, NotifyCallbackURL: url,
	}
	if err := mem.RegisterServer(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
	if err := mem.UpdateServerStatus(context.Background(), id, status); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackDispatchDeliversSignalWithoutBody(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	svc, mem := newSvc(t, nil)
	svc.WithPublicURL("http://atlas.example:8081")
	registerCallbackServer(t, mem, "game-9001", receiver.URL, model.StatusOnline)

	res, err := svc.Save(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Notify.Callbacks.Targets != 1 || res.Notify.Callbacks.Delivered != 1 || res.Notify.Callbacks.Failed != 0 {
		t.Fatalf("callbacks = %+v, want 1 target delivered", res.Notify.Callbacks)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("receiver hits = %d, want 1 (no retries on success)", len(bodies))
	}
	body := bodies[0]
	if body["type"] != "config.updated" {
		t.Errorf("signal type = %v", body["type"])
	}
	if v, _ := body["version"].(float64); int(v) != res.Config.Version {
		t.Errorf("signal version = %v, want %d", body["version"], res.Config.Version)
	}
	if body["crossserver_url"] != "http://atlas.example:8081/v1/crossserver/config" {
		t.Errorf("crossserver_url = %v", body["crossserver_url"])
	}
	if _, hasSpec := body["spec"]; hasSpec {
		t.Error("signal carried the config body — notify-then-pull forbids that")
	}
}

func TestCallbackDispatchRetriesThenReportsFailure(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer receiver.Close()

	svc, mem := newSvc(t, nil)
	svc.WithCallbackPolicy(250*time.Millisecond, 3, time.Millisecond)
	registerCallbackServer(t, mem, "game-9002", receiver.URL, model.StatusOnline)

	res, err := svc.Save(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Save: %v (a failed callback must not fail the save)", err)
	}
	cb := res.Notify.Callbacks
	if cb.Targets != 1 || cb.Delivered != 0 || cb.Failed != 1 {
		t.Fatalf("callbacks = %+v, want 1 target failed", cb)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Errorf("receiver attempts = %d, want 3 (retry with backoff)", attempts)
	}
}

func TestCallbackDispatchSkipsNonCallbackAndNonLive(t *testing.T) {
	svc, mem := newSvc(t, nil)

	// Same URL for all: a hit means a dispatch we did not want.
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	registerCallbackServer(t, mem, "game-online", receiver.URL, model.StatusOnline)
	registerCallbackServer(t, mem, "game-starting", receiver.URL, model.StatusStarting)
	registerCallbackServer(t, mem, "game-offline", receiver.URL, model.StatusOffline)

	poll := &model.Server{ID: "game-poll", Name: "p", Type: "game", Region: "cn-east",
		Endpoint: model.Endpoint{Host: "127.0.0.1", Port: 30002}, Capacity: 10,
		NotifyMode: model.NotifyModePoll}
	if err := mem.RegisterServer(context.Background(), poll); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Save(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Notify.Callbacks.Targets != 1 {
		t.Fatalf("targets = %d, want 1 (online callback server only; starting/offline/poll skipped)", res.Notify.Callbacks.Targets)
	}
}

func TestNotifyWithoutBusIsNotAnError(t *testing.T) {
	// The default http adapter has no subscriber on the config topic —
	// publish reports ErrNoSubscriber and that must degrade, not fail.
	svc, _ := newSvc(t, httpEvent.New())
	res, err := svc.Save(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Save with subscriberless bus: %v", err)
	}
	if res.Notify.BusError == "" {
		t.Error("expected BusError to record the subscriberless publish")
	}
}

func TestSaveWithNilEventsAndServers(t *testing.T) {
	mem := memory.New()
	svc := New(mem, nil, nil, nil)
	res, err := svc.Save(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Notify.Bus != "none" {
		t.Errorf("bus name = %q, want none", res.Notify.Bus)
	}
	if res.Notify.Callbacks.Targets != 0 {
		t.Errorf("targets = %d, want 0 without a server store", res.Notify.Callbacks.Targets)
	}
}

// registerPollServer registers a poll-mode server and forces its status,
// mirroring registerCallbackServer (bus addressing cares about status,
// not about the callback URL).
func registerPollServer(t *testing.T, mem *memory.Store, id string, status model.ServerStatus) {
	t.Helper()
	srv := &model.Server{
		ID: id, Name: id, Type: "game", Region: "cn-east",
		Endpoint: model.Endpoint{Host: "127.0.0.1", Port: 30001},
		Capacity: 100, NotifyMode: model.NotifyModePoll,
	}
	if err := mem.RegisterServer(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
	if err := mem.UpdateServerStatus(context.Background(), id, status); err != nil {
		t.Fatal(err)
	}
}

// TestBusSignalAddressesAffectedServers pins 按服务/分组定向投递: the bus
// event names the server IDs affected by the change — members of the
// changed cluster under the document before AND after the save — while
// targets keep naming the changed structures. Offline registrations are
// not addressed (下线即退订), a global change addresses everyone, and a
// change that affects no live server addresses nobody (subscribers then
// have nothing to pull).
func TestBusSignalAddressesAffectedServers(t *testing.T) {
	adapter := httpEvent.New()
	var mu sync.Mutex
	var got []*event.Event
	_ = adapter.Subscribe(context.Background(), event.TopicConfig, func(_ context.Context, e *event.Event) error {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
		return nil
	})
	svc, mem := newSvc(t, adapter)

	registerPollServer(t, mem, "game-1001", model.StatusOnline)
	registerPollServer(t, mem, "game-1002", model.StatusOffline)
	registerPollServer(t, mem, "game-2001", model.StatusOnline)

	save := func(spec model.CrossServerSpec, step string) *SaveResult {
		t.Helper()
		res, err := svc.Save(context.Background(), spec)
		if err != nil {
			t.Fatalf("%s: Save: %v", step, err)
		}
		return res
	}
	assertReceivers := func(step string, res *SaveResult, want ...string) {
		t.Helper()
		if !slices.Equal(res.Notify.Receivers, want) {
			t.Errorf("%s: receivers = %v, want %v", step, res.Notify.Receivers, want)
		}
		mu.Lock()
		e := got[len(got)-1]
		mu.Unlock()
		if !slices.Equal(e.ConfigServers, want) {
			t.Errorf("%s: event config_servers = %v, want %v", step, e.ConfigServers, want)
		}
	}

	// 1) First publish introduces the feature switches → global.
	spec := testSpec()
	res := save(spec, "first publish")
	assertReceivers("first publish", res, model.TargetAll)

	// 2) Adding a server to the cluster addresses the members before and
	// after: game-1001 (was, still), game-2001 (now); offline game-1002
	// is dropped. Targets keep naming the structure, not the servers.
	spec.Topology.Clusters[0].Servers = append(spec.Topology.Clusters[0].Servers, "game-2001")
	res = save(spec, "add member")
	if !slices.Equal(res.Notify.Targets, []string{"cluster-ea"}) {
		t.Errorf("add member: targets = %v, want [cluster-ea]", res.Notify.Targets)
	}
	assertReceivers("add member", res, "game-1001", "game-2001")

	// 3) Removing a live member still addresses it — it has to tear the
	// feature down even though it is no longer in the document.
	spec.Topology.Clusters[0].Servers = []string{"game-1002", "game-2001"}
	res = save(spec, "remove member")
	assertReceivers("remove member", res, "game-1001", "game-2001")

	// 4) A feature-switch change is global.
	spec.Features["cross_battlefield"] = false
	res = save(spec, "feature flip")
	if !slices.Equal(res.Notify.Targets, []string{model.TargetAll}) {
		t.Errorf("feature flip: targets = %v, want [*]", res.Notify.Targets)
	}
	assertReceivers("feature flip", res, model.TargetAll)

	// 5) Removing the last live member still addresses it (the
	// old-membership rule), even though only offline servers remain.
	spec.Topology.Clusters[0].Servers = []string{"game-1002"}
	res = save(spec, "last live member removed")
	assertReceivers("last live member removed", res, "game-2001")

	// 6) What remains is offline or unregistered: nobody live is affected,
	// the signal is addressed to nobody, and subscribers have nothing to do.
	spec.Topology.Clusters[0].Servers = []string{"game-9999"}
	res = save(spec, "no live member")
	assertReceivers("no live member", res)
}
