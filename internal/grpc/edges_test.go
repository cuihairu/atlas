// Error-path and field-mapping coverage for the gRPC surface: every RPC's
// service-failure wrap is driven with a fault-wrapped store (per-verb hooks
// over a real memory store), the registration-gating verdict and the
// realm/shard / patch field copies run through real services, and the
// small mappers (statusErr, httpStatusOf, methodOf, nil rate limiter) are
// pinned directly.
package grpc

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// faultStore wraps a real memory.Store and lets individual verbs fail on
// demand; unset hooks delegate to the backing store, so each test enables
// exactly the one failure it is exercising.
type faultStore struct {
	store.Store
	base *memory.Store

	listServersFn              func(ctx context.Context, f store.ServerFilter) ([]*model.Server, error)
	getServerFn                func(ctx context.Context, id string) (*model.Server, error)
	recordHeartbeatFn          func(ctx context.Context, id string, hb model.Heartbeat) error
	getCharacterByCharacterIDF func(ctx context.Context, id int64) (*model.Character, error)
	listByAccountFn            func(ctx context.Context, id int64) ([]*model.Character, error)
	listByServerFn             func(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, error)
	getStatsFn                 func(ctx context.Context) (*model.Stats, error)
	searchCharactersFn         func(ctx context.Context, f store.CharacterSearchFilter) ([]*model.Character, string, error)
	listMigrationsFn           func(ctx context.Context, limit int) ([]*model.Migration, error)
	getMigrationF              func(ctx context.Context, id string) (*model.Migration, error)
}

func (f *faultStore) ListServers(ctx context.Context, sf store.ServerFilter) ([]*model.Server, error) {
	if f.listServersFn != nil {
		return f.listServersFn(ctx, sf)
	}
	return f.base.ListServers(ctx, sf)
}

func (f *faultStore) GetServer(ctx context.Context, id string) (*model.Server, error) {
	if f.getServerFn != nil {
		return f.getServerFn(ctx, id)
	}
	return f.base.GetServer(ctx, id)
}

func (f *faultStore) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	if f.recordHeartbeatFn != nil {
		return f.recordHeartbeatFn(ctx, id, hb)
	}
	return f.base.RecordHeartbeat(ctx, id, hb)
}

func (f *faultStore) GetCharacterByCharacterID(ctx context.Context, id int64) (*model.Character, error) {
	if f.getCharacterByCharacterIDF != nil {
		return f.getCharacterByCharacterIDF(ctx, id)
	}
	return f.base.GetCharacterByCharacterID(ctx, id)
}

func (f *faultStore) ListCharactersByAccount(ctx context.Context, id int64) ([]*model.Character, error) {
	if f.listByAccountFn != nil {
		return f.listByAccountFn(ctx, id)
	}
	return f.base.ListCharactersByAccount(ctx, id)
}

func (f *faultStore) ListCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, error) {
	if f.listByServerFn != nil {
		return f.listByServerFn(ctx, serverID, limit, cursor)
	}
	return f.base.ListCharactersByServer(ctx, serverID, limit, cursor)
}

func (f *faultStore) GetStats(ctx context.Context) (*model.Stats, error) {
	if f.getStatsFn != nil {
		return f.getStatsFn(ctx)
	}
	return f.base.GetStats(ctx)
}

func (f *faultStore) SearchCharacters(ctx context.Context, f2 store.CharacterSearchFilter) ([]*model.Character, string, error) {
	if f.searchCharactersFn != nil {
		return f.searchCharactersFn(ctx, f2)
	}
	return f.base.SearchCharacters(ctx, f2)
}

func (f *faultStore) ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error) {
	if f.listMigrationsFn != nil {
		return f.listMigrationsFn(ctx, limit)
	}
	return f.base.ListMigrations(ctx, limit)
}

func (f *faultStore) GetMigration(ctx context.Context, id string) (*model.Migration, error) {
	if f.getMigrationF != nil {
		return f.getMigrationF(ctx, id)
	}
	return f.base.GetMigration(ctx, id)
}

// stubAdapter is a controllable EventAdapter: fail flips Publish to an
// error, async flips Synchronous (pinning the queued returns), and every
// accepted event is recorded for field assertions.
type stubAdapter struct {
	fail  bool
	async bool
	seen  []*event.Event
}

func (a *stubAdapter) Name() string      { return "stub" }
func (a *stubAdapter) Synchronous() bool { return !a.async }

func (a *stubAdapter) Publish(_ context.Context, e *event.Event) error {
	if a.fail {
		return errFault
	}
	cp := *e
	a.seen = append(a.seen, &cp)
	return nil
}

func (a *stubAdapter) Subscribe(context.Context, string, event.Handler) error { return nil }
func (a *stubAdapter) Ack(context.Context, *event.Event) error                { return nil }
func (a *stubAdapter) Close() error                                           { return nil }

// newEdgeServer wires a Server directly over a fault store (and a stub
// adapter), so RPC-level wraps can be driven without the bufconn harness.
func newEdgeServer(fs *faultStore, events event.EventAdapter) *Server {
	return &Server{
		registry:  registry.New(fs, fs, testLogger()),
		discovery: discovery.New(fs, fs),
		directory: directory.New(fs),
		routing:   routing.New(fs.base, fs.base, fs.base, fs.base),
		admin:     admin.New(fs),
		events:    events,
	}
}

func newFaultStore() *faultStore {
	base := memory.New()
	return &faultStore{Store: base, base: base}
}

func seedServer(t *testing.T, mem *memory.Store, id string, tags ...model.ServerTag) {
	t.Helper()
	if err := mem.RegisterServer(context.Background(), &model.Server{
		ID:       id,
		Name:     id,
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30009},
		Status:   model.StatusOnline,
		Tags:     tags,
	}); err != nil {
		t.Fatalf("seed server %s: %v", id, err)
	}
}

// TestRegisterCopiesRealmAndShard pins the optional-pointer field copies on
// both sides: a Register carrying realm/shard must reach the store and come
// back on the wire response.
func TestRegisterCopiesRealmAndShard(t *testing.T) {
	s := newEdgeServer(newFaultStore(), &stubAdapter{})
	resp, err := s.Register(context.Background(), &pb.RegisterRequest{
		ServerId: "game-rs",
		Name:     "Realm Server",
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: &pb.Endpoint{Host: "10.0.0.2", Port: 30002},
		Capacity: 100,
		RealmId:  "realm-9",
		ShardId:  "shard-9",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if resp.Server.GetRealmId() != "realm-9" || resp.Server.GetShardId() != "shard-9" {
		t.Fatalf("realm/shard = %q/%q, want realm-9/shard-9", resp.Server.GetRealmId(), resp.Server.GetShardId())
	}
}

// TestHeartbeatServiceError pins the heartbeat wrap: a runtime write that
// fails surfaces as Internal, never a silent success (liveness data must
// not be lost).
func TestHeartbeatServiceError(t *testing.T) {
	fs := newFaultStore()
	seedServer(t, fs.base, "hb-srv")
	fs.recordHeartbeatFn = func(context.Context, string, model.Heartbeat) error { return errFault }
	s := newEdgeServer(fs, &stubAdapter{})

	if _, err := s.Heartbeat(context.Background(), &pb.HeartbeatRequest{ServerId: "hb-srv", Players: 3, Status: "online"}); status.Code(err) != codes.Internal {
		t.Fatalf("heartbeat with failing runtime write = %v, want Internal", err)
	}
}

// TestDiscoveryServiceErrors pins the two discovery wraps: listing and
// single-lookup failures surface as Internal.
func TestDiscoveryServiceErrors(t *testing.T) {
	fs := newFaultStore()
	fs.listServersFn = func(context.Context, store.ServerFilter) ([]*model.Server, error) {
		return nil, errFault
	}
	fs.getServerFn = func(context.Context, string) (*model.Server, error) {
		return nil, errFault
	}
	s := newEdgeServer(fs, &stubAdapter{})
	ctx := context.Background()

	if _, err := s.ListServers(ctx, &pb.ListServersRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("list servers with failing store = %v, want Internal", err)
	}
	if _, err := s.GetServer(ctx, &pb.GetServerRequest{ServerId: "any"}); status.Code(err) != codes.Internal {
		t.Fatalf("get server with failing store = %v, want Internal", err)
	}
}

// TestCreateCharacterFailurePaths pins the three pre-projection wraps: a
// registration-check store error (Internal), a 禁止注册 verdict
// (PermissionDenied), a publish failure (Internal), and a read-back failure
// after a synchronous publish (Internal).
func TestCreateCharacterFailurePaths(t *testing.T) {
	ctx := context.Background()
	t.Run("check error", func(t *testing.T) {
		fs := newFaultStore()
		fs.getServerFn = func(context.Context, string) (*model.Server, error) { return nil, errFault }
		s := newEdgeServer(fs, &stubAdapter{})
		if _, err := s.CreateCharacter(ctx, &pb.CreateCharacterRequest{AccountId: 1, ServerId: "srv", CharacterId: 10}); status.Code(err) != codes.Internal {
			t.Fatalf("check error = %v, want Internal", err)
		}
	})
	t.Run("no_register tag", func(t *testing.T) {
		fs := newFaultStore()
		seedServer(t, fs.base, "gated", model.ServerTag{Code: model.TagNoRegister})
		s := newEdgeServer(fs, &stubAdapter{})
		if _, err := s.CreateCharacter(ctx, &pb.CreateCharacterRequest{AccountId: 1, ServerId: "gated", CharacterId: 11}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("no_register verdict = %v, want PermissionDenied", err)
		}
	})
	t.Run("publish error", func(t *testing.T) {
		fs := newFaultStore()
		seedServer(t, fs.base, "pub-srv")
		s := newEdgeServer(fs, &stubAdapter{fail: true})
		if _, err := s.CreateCharacter(ctx, &pb.CreateCharacterRequest{AccountId: 1, ServerId: "pub-srv", CharacterId: 12}); status.Code(err) != codes.Internal {
			t.Fatalf("publish error = %v, want Internal", err)
		}
	})
	t.Run("read-back error", func(t *testing.T) {
		fs := newFaultStore()
		seedServer(t, fs.base, "rb-srv")
		fs.getCharacterByCharacterIDF = func(context.Context, int64) (*model.Character, error) { return nil, errFault }
		s := newEdgeServer(fs, &stubAdapter{})
		if _, err := s.CreateCharacter(ctx, &pb.CreateCharacterRequest{AccountId: 1, ServerId: "rb-srv", CharacterId: 13}); status.Code(err) != codes.Internal {
			t.Fatalf("read-back error = %v, want Internal", err)
		}
	})
}

// TestCreateCharacterQueuedOnAsync pins the asynchronous contract: with an
// enqueue-only adapter the RPC answers "queued" without a projection read.
func TestCreateCharacterQueuedOnAsync(t *testing.T) {
	fs := newFaultStore()
	seedServer(t, fs.base, "q-srv")
	ad := &stubAdapter{async: true}
	s := newEdgeServer(fs, ad)

	resp, err := s.CreateCharacter(context.Background(), &pb.CreateCharacterRequest{AccountId: 1, ServerId: "q-srv", CharacterId: 15})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp.GetStatus() != "queued" || len(ad.seen) != 1 {
		t.Fatalf("async create = %q with %d events, want queued/1", resp.GetStatus(), len(ad.seen))
	}
}

// TestListCharactersServiceErrors pins the two list wraps.
func TestListCharactersServiceErrors(t *testing.T) {
	fs := newFaultStore()
	fs.listByAccountFn = func(context.Context, int64) ([]*model.Character, error) { return nil, errFault }
	fs.listByServerFn = func(context.Context, string, int, string) ([]*model.Character, error) {
		return nil, errFault
	}
	s := newEdgeServer(fs, &stubAdapter{})
	ctx := context.Background()

	if _, err := s.ListCharactersByAccount(ctx, &pb.ListCharactersByAccountRequest{AccountId: 1}); status.Code(err) != codes.Internal {
		t.Fatalf("list by account = %v, want Internal", err)
	}
	if _, err := s.ListCharactersByServer(ctx, &pb.ListCharactersByServerRequest{ServerId: "srv"}); status.Code(err) != codes.Internal {
		t.Fatalf("list by server = %v, want Internal", err)
	}
}

// TestUpdateCharacterFullPatchQueued pins the patch field copies (name,
// class, avatar, metadata) and the asynchronous "queued" contract in one
// shot: an async adapter must return queued with every optional field
// carried onto the published event.
func TestUpdateCharacterFullPatchQueued(t *testing.T) {
	fs := newFaultStore()
	seedServer(t, fs.base, "upd-srv")
	ad := &stubAdapter{async: true}
	s := newEdgeServer(fs, ad)

	name, class, avatar := "Renamed", int32(7), "https://cdn.example.com/a.png"
	resp, err := s.UpdateCharacter(context.Background(), &pb.UpdateCharacterRequest{
		AccountId:   5,
		ServerId:    "upd-srv",
		CharacterId: 21,
		Name:        &name,
		ClassId:     &class,
		Avatar:      &avatar,
		Metadata:    map[string]string{"tier": "gold"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if resp.GetStatus() != "queued" {
		t.Fatalf("async update status = %q, want queued", resp.GetStatus())
	}
	if len(ad.seen) != 1 {
		t.Fatalf("published %d events, want 1", len(ad.seen))
	}
	e := ad.seen[0]
	if e.Name != "Renamed" || e.ClassID == nil || *e.ClassID != 7 || e.Avatar == nil || *e.Avatar != avatar {
		t.Fatalf("patch fields not carried: name=%q class=%v avatar=%v", e.Name, e.ClassID, e.Avatar)
	}
	if e.Metadata == nil || (*e.Metadata)["tier"] != "gold" {
		t.Fatalf("metadata not carried: %v", e.Metadata)
	}
}

// TestUpdateCharacterSyncReadFailure pins the synchronous read-back wrap:
// a projection read that fails after a successful publish surfaces as
// Internal (the write landed, but the caller must not see a stale row).
func TestUpdateCharacterSyncReadFailure(t *testing.T) {
	fs := newFaultStore()
	seedServer(t, fs.base, "upd-rb")
	fs.getCharacterByCharacterIDF = func(context.Context, int64) (*model.Character, error) { return nil, errFault }
	s := newEdgeServer(fs, &stubAdapter{})

	if _, err := s.UpdateCharacter(context.Background(), &pb.UpdateCharacterRequest{AccountId: 5, ServerId: "upd-rb", CharacterId: 22}); status.Code(err) != codes.Internal {
		t.Fatalf("sync read-back = %v, want Internal", err)
	}
}

// TestDeleteCharacterFailurePaths pins the two delete wraps: a resolve
// failure (never enqueue a missing row on async transports) and a publish
// failure.
func TestDeleteCharacterFailurePaths(t *testing.T) {
	ctx := context.Background()
	t.Run("resolve error", func(t *testing.T) {
		fs := newFaultStore()
		fs.getCharacterByCharacterIDF = func(context.Context, int64) (*model.Character, error) { return nil, errFault }
		s := newEdgeServer(fs, &stubAdapter{})
		if _, err := s.DeleteCharacter(ctx, &pb.DeleteCharacterRequest{CharacterId: 31}); status.Code(err) != codes.Internal {
			t.Fatalf("resolve error = %v, want Internal", err)
		}
	})
	t.Run("publish error", func(t *testing.T) {
		fs := newFaultStore()
		if err := fs.base.UpsertCharacter(ctx, &model.Character{AccountID: 9, ServerID: "del-srv", CharacterID: 32, Name: "Doomed"}); err != nil {
			t.Fatalf("seed character: %v", err)
		}
		s := newEdgeServer(fs, &stubAdapter{fail: true})
		if _, err := s.DeleteCharacter(ctx, &pb.DeleteCharacterRequest{CharacterId: 32}); status.Code(err) != codes.Internal {
			t.Fatalf("publish error = %v, want Internal", err)
		}
	})
}

// TestDeleteCharacterQueuedOnAsync pins the asynchronous contract for
// deletes: the resolve still happens (a missing row is never enqueued),
// then the RPC answers "queued" without waiting for the projection.
func TestDeleteCharacterQueuedOnAsync(t *testing.T) {
	fs := newFaultStore()
	ctx := context.Background()
	if err := fs.base.UpsertCharacter(ctx, &model.Character{AccountID: 9, ServerID: "delq-srv", CharacterID: 33, Name: "Async"}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	s := newEdgeServer(fs, &stubAdapter{async: true})

	resp, err := s.DeleteCharacter(ctx, &pb.DeleteCharacterRequest{CharacterId: 33})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.GetStatus() != "queued" {
		t.Fatalf("async delete status = %q, want queued", resp.GetStatus())
	}
}

// TestRollbackMigrationReadAfterSuccess pins the post-rollback wrap: a
// rollback that lands but whose confirmation read fails surfaces as an
// error — the caller must not be told "rolled back" on faith.
func TestRollbackMigrationReadAfterSuccess(t *testing.T) {
	fs := newFaultStore()
	ctx := context.Background()
	seedServer(t, fs.base, "mig-a")
	seedServer(t, fs.base, "mig-b")
	s := newEdgeServer(fs, &stubAdapter{})

	created, err := s.CreateMigration(ctx, &pb.CreateMigrationRequest{SourceServers: []string{"mig-a"}, TargetServer: "mig-b"})
	if err != nil {
		t.Fatalf("create migration: %v", err)
	}

	// Fail only the confirmation read: the first GetMigration (inside the
	// rollback transition) still delegates, the second one faults.
	calls := 0
	fs.getMigrationF = func(c context.Context, id string) (*model.Migration, error) {
		calls++
		if calls > 1 {
			return nil, errFault
		}
		return fs.base.GetMigration(c, id)
	}

	if _, err := s.RollbackMigration(ctx, &pb.RollbackMigrationRequest{MigrationId: created.Migration.GetId()}); status.Code(err) != codes.Internal {
		t.Fatalf("rollback with failing confirmation read = %v, want Internal", err)
	}
}

// TestAdminServiceErrors pins every admin RPC's failure wrap: stats and
// search through faulted store verbs, and the migration verbs through the
// migration service's own not-found/lookup errors.
func TestAdminServiceErrors(t *testing.T) {
	fs := newFaultStore()
	fs.getStatsFn = func(context.Context) (*model.Stats, error) { return nil, errFault }
	fs.searchCharactersFn = func(context.Context, store.CharacterSearchFilter) ([]*model.Character, string, error) {
		return nil, "", errFault
	}
	fs.listMigrationsFn = func(context.Context, int) ([]*model.Migration, error) { return nil, errFault }
	s := newEdgeServer(fs, &stubAdapter{})
	ctx := context.Background()

	if _, err := s.GetStats(ctx, &pb.GetStatsRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("stats = %v, want Internal", err)
	}
	if _, err := s.SearchCharacters(ctx, &pb.SearchCharactersRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("search = %v, want Internal", err)
	}
	if _, err := s.ListMigrations(ctx, &pb.ListMigrationsRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("list migrations = %v, want Internal", err)
	}
	// The migration lifecycle verbs fail through their own validation:
	// sources/targets must exist, and unknown IDs are NotFound.
	if _, err := s.CreateMigration(ctx, &pb.CreateMigrationRequest{SourceServers: []string{"ghost"}, TargetServer: "ghost-2"}); err == nil {
		t.Fatal("create migration with unknown servers: expected error, got nil")
	}
	if _, err := s.GetMigration(ctx, &pb.GetMigrationRequest{MigrationId: "nope"}); status.Code(err) != codes.NotFound {
		t.Fatalf("get unknown migration = %v, want NotFound", err)
	}
	if _, err := s.RollbackMigration(ctx, &pb.RollbackMigrationRequest{MigrationId: "nope"}); status.Code(err) != codes.NotFound {
		t.Fatalf("rollback unknown migration = %v, want NotFound", err)
	}
}

// TestAuthAdminIPBlocked pins the admin-domain IP gate: an address outside
// the whitelist is denied before the handler runs, fail-closed like REST.
func TestAuthAdminIPBlocked(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/24")
	ic := UnaryAuth(AuthConfig{AdminIPs: []*net.IPNet{cidr}})

	if called, err := invoke(t, ic, admMethod("GetStats"), ctxWithPeer("10.0.0.7")); !called || err != nil {
		t.Fatalf("whitelisted admin ip rejected: called=%v err=%v", called, err)
	}
	if _, err := invoke(t, ic, admMethod("GetStats"), ctxWithPeer("10.9.9.9")); codeOf(t, err) != codes.PermissionDenied {
		t.Errorf("outside admin ip = %v, want PermissionDenied", err)
	}
}

// TestRateLimitNilLimiterPinsPassthrough pins the unconfigured contract: a
// nil limiter means no rate limiting at all, not a deny-all.
func TestRateLimitNilLimiterPinsPassthrough(t *testing.T) {
	called, err := invoke(t, UnaryRateLimit(nil), regMethod("Register"), context.Background())
	if !called || err != nil {
		t.Fatalf("nil limiter must pass through: called=%v err=%v", called, err)
	}
}

// TestStatusErrMappingPinsEdges pins the mapper's boundary arms: nil in,
// nil out, and an unrecognized error maps to Internal.
func TestStatusErrMappingPinsEdges(t *testing.T) {
	if statusErr(nil) != nil {
		t.Fatal("statusErr(nil) must be nil")
	}
	if got := statusErr(errors.New("mystery")); status.Code(got) != codes.Internal {
		t.Fatalf("generic error mapped to %v, want Internal", status.Code(got))
	}
}

// TestMethodOfWithoutSlash pins the degenerate input: a method string with
// no separator is returned whole, not mis-sliced.
func TestMethodOfWithoutSlash(t *testing.T) {
	if got := methodOf("Register"); got != "Register" {
		t.Fatalf("methodOf(Register) = %q", got)
	}
}

// TestHTTPStatusOfCanceled pins the one unmapped arm of the audit status
// vocabulary: a canceled gRPC call reports as HTTP 499, like nginx.
func TestHTTPStatusOfCanceled(t *testing.T) {
	if got := httpStatusOf(codes.Canceled); got != 499 {
		t.Fatalf("httpStatusOf(Canceled) = %d, want 499", got)
	}
}
