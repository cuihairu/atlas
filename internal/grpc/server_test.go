package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// testConn wires the shared services behind an in-memory gRPC connection.
type testConn struct {
	Registry  pb.RegistryServiceClient
	Discovery pb.DiscoveryServiceClient
	Directory pb.DirectoryServiceClient
	Routing   pb.RoutingServiceClient
	Admin     pb.AdminServiceClient
	// Mem is the shared store, exposed so tests can seed states the RPC
	// surface cannot produce (e.g. config-managed servers).
	Mem *memory.Store
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestConn(t *testing.T) *testConn {
	t.Helper()
	ctx := context.Background()

	mem := memory.New()
	adapter := httpadapter.New()
	dirSvc := directory.New(mem)
	if err := adapter.Subscribe(ctx, event.TopicCharacters, func(ctx context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(ctx, e)
		return err
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	srv := New(
		registry.New(mem, mem, testLogger()),
		discovery.New(mem, mem),
		dirSvc,
		routing.New(mem, mem, mem, mem),
		admin.New(mem),
		adapter,
	)

	lis := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	srv.RegisterServices(g)
	go g.Serve(lis) //nolint:errcheck // test server
	t.Cleanup(g.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return &testConn{
		Registry:  pb.NewRegistryServiceClient(conn),
		Discovery: pb.NewDiscoveryServiceClient(conn),
		Directory: pb.NewDirectoryServiceClient(conn),
		Routing:   pb.NewRoutingServiceClient(conn),
		Admin:     pb.NewAdminServiceClient(conn),
		Mem:       mem,
	}
}

// TestEndToEnd exercises all five services over one connection.
func TestEndToEnd(t *testing.T) {
	c := newTestConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Registry: register → heartbeat.
	reg, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "game-1001",
		Name:     "Test Server",
		Type:     "game",
		Region:   "cn-east",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Server.GetId() != "game-1001" {
		t.Fatalf("expected registered id game-1001, got %q", reg.Server.GetId())
	}

	hb, err := c.Registry.Heartbeat(ctx, &pb.HeartbeatRequest{
		ServerId: "game-1001", Players: 10, Load: 0.25,
	})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if hb.Status != "online" {
		t.Errorf("expected status online, got %q", hb.Status)
	}

	// Discovery: list + get.
	list, err := c.Discovery.ListServers(ctx, &pb.ListServersRequest{})
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(list.Servers) != 1 || list.Servers[0].Id != "game-1001" {
		t.Fatalf("expected [game-1001], got %v", list.Servers)
	}
	got, err := c.Discovery.GetServer(ctx, &pb.GetServerRequest{ServerId: "game-1001"})
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.Server.Players != 10 {
		t.Errorf("expected runtime players 10, got %d", got.Server.Players)
	}

	// Directory: create → get → update → list.
	created, err := c.Directory.CreateCharacter(ctx, &pb.CreateCharacterRequest{
		AccountId: 7, ServerId: "game-1001", CharacterId: 823712,
		Name: "剑无尘", Level: 1, ClassId: 3,
	})
	if err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	if created.Status != "created" || created.Character.GetName() != "剑无尘" {
		t.Fatalf("unexpected create response: %+v", created)
	}

	fetch, err := c.Directory.GetCharacter(ctx, &pb.GetCharacterRequest{CharacterId: 823712})
	if err != nil {
		t.Fatalf("GetCharacter: %v", err)
	}
	if fetch.Character.GetAccountId() != 7 {
		t.Errorf("expected account 7, got %d", fetch.Character.GetAccountId())
	}

	newLevel := int32(10)
	upd, err := c.Directory.UpdateCharacter(ctx, &pb.UpdateCharacterRequest{
		CharacterId: 823712, Level: &newLevel,
	})
	if err != nil {
		t.Fatalf("UpdateCharacter: %v", err)
	}
	if upd.Character.GetLevel() != 10 {
		t.Errorf("expected level 10, got %d", upd.Character.GetLevel())
	}

	byAccount, err := c.Directory.ListCharactersByAccount(ctx, &pb.ListCharactersByAccountRequest{AccountId: 7})
	if err != nil {
		t.Fatalf("ListCharactersByAccount: %v", err)
	}
	if len(byAccount.Characters) != 1 {
		t.Errorf("expected 1 character, got %d", len(byAccount.Characters))
	}

	// Routing: recommend (no filters — fallback to status-only ranking).
	rec, err := c.Routing.Recommend(ctx, &pb.RecommendRequest{})
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if rec.Server.GetId() != "game-1001" || rec.Reason == "" {
		t.Errorf("unexpected recommendation: id=%q reason=%q", rec.Server.GetId(), rec.Reason)
	}

	// Admin: stats, search, migrations, lifecycle.
	stats, err := c.Admin.GetStats(ctx, &pb.GetStatsRequest{})
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalServers != 1 || stats.TotalCharacters != 1 {
		t.Errorf("unexpected stats: servers=%d characters=%d", stats.TotalServers, stats.TotalCharacters)
	}

	search, err := c.Admin.SearchCharacters(ctx, &pb.SearchCharactersRequest{Name: "剑"})
	if err != nil {
		t.Fatalf("SearchCharacters: %v", err)
	}
	if len(search.Characters) != 1 {
		t.Errorf("expected 1 search hit, got %d", len(search.Characters))
	}

	// Migration validates that source servers exist.
	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "game-old", Name: "Old Server", Type: "game",
		Region: "cn-east", Version: "1.0.0", Platform: "android",
		Endpoint: &pb.Endpoint{Host: "10.0.0.2", Port: 30002}, Capacity: 500,
	}); err != nil {
		t.Fatalf("Register game-old: %v", err)
	}

	mig, err := c.Admin.CreateMigration(ctx, &pb.CreateMigrationRequest{
		SourceServers: []string{"game-old"}, TargetServer: "game-1001",
	})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	if mig.Migration.GetId() == "" {
		t.Fatal("expected migration id")
	}
	if _, err := c.Admin.GetMigration(ctx, &pb.GetMigrationRequest{MigrationId: mig.Migration.Id}); err != nil {
		t.Fatalf("GetMigration: %v", err)
	}
	migs, err := c.Admin.ListMigrations(ctx, &pb.ListMigrationsRequest{Limit: 10})
	if err != nil || len(migs.Migrations) != 1 {
		t.Fatalf("ListMigrations: err=%v n=%d", err, len(migs.Migrations))
	}
	rb, err := c.Admin.RollbackMigration(ctx, &pb.RollbackMigrationRequest{MigrationId: mig.Migration.Id})
	if err != nil {
		t.Fatalf("RollbackMigration: %v", err)
	}
	if rb.Migration.GetStatus() != "rolled_back" {
		t.Errorf("expected rolled_back, got %q", rb.Migration.GetStatus())
	}

	maint, err := c.Admin.SetMaintenance(ctx, &pb.ServerIdRequest{ServerId: "game-1001"})
	if err != nil {
		t.Fatalf("SetMaintenance: %v", err)
	}
	if maint.GetStatus() != "maintenance" {
		t.Errorf("expected maintenance, got %q", maint.GetStatus())
	}

	// Directory delete → registry unregister.
	if _, err := c.Directory.DeleteCharacter(ctx, &pb.DeleteCharacterRequest{CharacterId: 823712}); err != nil {
		t.Fatalf("DeleteCharacter: %v", err)
	}
	if _, err := c.Directory.GetCharacter(ctx, &pb.GetCharacterRequest{CharacterId: 823712}); status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound after delete, got %v", err)
	}

	off, err := c.Registry.Unregister(ctx, &pb.UnregisterRequest{ServerId: "game-1001"})
	if err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if off.GetStatus() != "offline" {
		t.Errorf("expected offline, got %q", off.GetStatus())
	}
}

// TestErrorMapping checks domain errors surface as gRPC status codes.
func TestErrorMapping(t *testing.T) {
	c := newTestConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.Directory.GetCharacter(ctx, &pb.GetCharacterRequest{CharacterId: 999}); status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
	if _, err := c.Directory.CreateCharacter(ctx, &pb.CreateCharacterRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
	if _, err := c.Routing.Recommend(ctx, &pb.RecommendRequest{}); status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound for empty cluster, got %v", err)
	}
}

// TestAdminLifecycle drives drain → enable → disable and asserts both the
// happy-path statuses and the invalid-transition / missing-server errors.
func TestAdminLifecycle(t *testing.T) {
	c := newTestConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "game-1001", Name: "Test Server", Type: "game", Region: "cn-east",
		Version: "1.0.0", Platform: "android",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 2000,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Server.GetStatus() != "starting" {
		t.Fatalf("expected starting after register, got %q", reg.Server.GetStatus())
	}
	if _, err := c.Registry.Heartbeat(ctx, &pb.HeartbeatRequest{ServerId: "game-1001", Players: 10, Load: 0.25}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// online → draining. Enable/SetMaintenance from draining are rejected.
	drain, err := c.Admin.SetDrain(ctx, &pb.ServerIdRequest{ServerId: "game-1001"})
	if err != nil {
		t.Fatalf("SetDrain: %v", err)
	}
	if drain.GetStatus() != "draining" {
		t.Errorf("expected draining, got %q", drain.GetStatus())
	}
	if _, err := c.Admin.Enable(ctx, &pb.ServerIdRequest{ServerId: "game-1001"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("enable from draining: expected InvalidArgument, got %v", err)
	}
	if _, err := c.Admin.SetMaintenance(ctx, &pb.ServerIdRequest{ServerId: "game-1001"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("maintenance from draining: expected InvalidArgument, got %v", err)
	}

	// draining → disabled → online (Enable is the way back).
	if _, err := c.Admin.Disable(ctx, &pb.ServerIdRequest{ServerId: "game-1001"}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	on, err := c.Admin.Enable(ctx, &pb.ServerIdRequest{ServerId: "game-1001"})
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if on.GetStatus() != "online" {
		t.Errorf("expected online, got %q", on.GetStatus())
	}
	if _, err := c.Admin.Disable(ctx, &pb.ServerIdRequest{ServerId: "game-1001"}); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	// Missing server → NotFound for every lifecycle call.
	for name, call := range map[string]func() error{
		"SetMaintenance": func() error {
			_, err := c.Admin.SetMaintenance(ctx, &pb.ServerIdRequest{ServerId: "game-404"})
			return err
		},
		"SetDrain": func() error {
			_, err := c.Admin.SetDrain(ctx, &pb.ServerIdRequest{ServerId: "game-404"})
			return err
		},
		"Enable": func() error {
			_, err := c.Admin.Enable(ctx, &pb.ServerIdRequest{ServerId: "game-404"})
			return err
		},
		"Disable": func() error {
			_, err := c.Admin.Disable(ctx, &pb.ServerIdRequest{ServerId: "game-404"})
			return err
		},
	} {
		if code := status.Code(call()); code != codes.NotFound {
			t.Errorf("%s on missing server: expected NotFound, got %v", name, code)
		}
	}
}

// TestDirectoryServerCharacters covers the per-server character listing with
// pagination plus the update/delete argument and not-found errors.
func TestDirectoryServerCharacters(t *testing.T) {
	c := newTestConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "game-1001", Name: "Test Server", Type: "game", Region: "cn-east",
		Version: "1.0.0", Platform: "android",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 2000,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for i := int64(1); i <= 3; i++ {
		if _, err := c.Directory.CreateCharacter(ctx, &pb.CreateCharacterRequest{
			AccountId: 7, ServerId: "game-1001", CharacterId: i,
			Name: fmt.Sprintf("角色%d", i), Level: 1, ClassId: 3,
		}); err != nil {
			t.Fatalf("CreateCharacter(%d): %v", i, err)
		}
	}

	page1, err := c.Directory.ListCharactersByServer(ctx, &pb.ListCharactersByServerRequest{
		ServerId: "game-1001", Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListCharactersByServer page 1: %v", err)
	}
	if len(page1.Characters) != 2 || page1.NextCursor == "" {
		t.Fatalf("page 1: expected 2 characters + cursor, got %d + %q", len(page1.Characters), page1.NextCursor)
	}
	page2, err := c.Directory.ListCharactersByServer(ctx, &pb.ListCharactersByServerRequest{
		ServerId: "game-1001", Limit: 2, Cursor: page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("ListCharactersByServer page 2: %v", err)
	}
	// The cursor is the last-seen character ID even on the final page; a
	// short page is the end-of-list signal.
	if len(page2.Characters) != 1 || page2.NextCursor != "3" {
		t.Errorf("page 2: expected 1 character with cursor %q, got %d + %q", "3", len(page2.Characters), page2.NextCursor)
	}

	// UpdateCharacter argument and not-found errors.
	newLevel := int32(10)
	if _, err := c.Directory.UpdateCharacter(ctx, &pb.UpdateCharacterRequest{
		CharacterId: 0, Level: &newLevel,
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("update with character_id=0: expected InvalidArgument, got %v", err)
	}
	if _, err := c.Directory.UpdateCharacter(ctx, &pb.UpdateCharacterRequest{
		CharacterId: 999, Level: &newLevel,
	}); status.Code(err) != codes.NotFound {
		t.Errorf("update missing character: expected NotFound, got %v", err)
	}
	if _, err := c.Directory.DeleteCharacter(ctx, &pb.DeleteCharacterRequest{CharacterId: 999}); status.Code(err) != codes.NotFound {
		t.Errorf("delete missing character: expected NotFound, got %v", err)
	}
}

// TestRegistryErrors checks registry argument validation, unknown-server
// errors, and the config-managed registration conflict mapping.
func TestRegistryErrors(t *testing.T) {
	c := newTestConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Missing endpoint / region fails validation.
	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{ServerId: "game-bad"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("register without endpoint: expected InvalidArgument, got %v", err)
	}
	if _, err := c.Registry.Heartbeat(ctx, &pb.HeartbeatRequest{ServerId: "game-404"}); status.Code(err) != codes.NotFound {
		t.Errorf("heartbeat unknown server: expected NotFound, got %v", err)
	}
	if _, err := c.Registry.Unregister(ctx, &pb.UnregisterRequest{ServerId: "game-404"}); status.Code(err) != codes.NotFound {
		t.Errorf("unregister unknown server: expected NotFound, got %v", err)
	}

	// Config-managed servers reject API registration (AlreadyExists).
	if err := c.Mem.RegisterServer(ctx, &model.Server{
		ID: "game-cfg", Name: "Declared", Region: "cn-east",
		Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30009},
		Status:   model.StatusOnline, Source: "config",
	}); err != nil {
		t.Fatalf("seed config server: %v", err)
	}
	_, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "game-cfg", Name: "Hijack", Region: "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.9", Port: 30009}, Capacity: 100,
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("register config-managed server: expected AlreadyExists, got %v", err)
	}
}

// TestSearchCharactersFilters covers the optional-filter mapping and the
// rollback state machine (completed migrations cannot roll back again).
func TestSearchCharactersFilters(t *testing.T) {
	c := newTestConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.Registry.Register(ctx, &pb.RegisterRequest{
		ServerId: "game-1001", Name: "Test Server", Type: "game", Region: "cn-east",
		Version: "1.0.0", Platform: "android",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 2000,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for i := int64(1); i <= 4; i++ {
		if _, err := c.Directory.CreateCharacter(ctx, &pb.CreateCharacterRequest{
			AccountId: 7, ServerId: "game-1001", CharacterId: i,
			Name: fmt.Sprintf("剑客%d", i), Level: int32(i * 10), ClassId: 3,
		}); err != nil {
			t.Fatalf("CreateCharacter(%d): %v", i, err)
		}
	}

	classID := int32(3)
	minLevel := int32(15)
	maxLevel := int32(35)
	search, err := c.Admin.SearchCharacters(ctx, &pb.SearchCharactersRequest{
		Name: "剑", ServerId: "game-1001", ClassId: &classID,
		MinLevel: &minLevel, MaxLevel: &maxLevel, Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchCharacters: %v", err)
	}
	if len(search.Characters) != 2 {
		t.Errorf("expected 2 characters in [15,35], got %d", len(search.Characters))
	}
	for _, ch := range search.Characters {
		if ch.GetLevel() < int32(minLevel) || ch.GetLevel() > int32(maxLevel) {
			t.Errorf("filter leaked: level %d outside [%d,%d]", ch.GetLevel(), minLevel, maxLevel)
		}
	}

	// Rollback: pending is fine, but a rolled-back migration cannot roll
	// back again.
	mig, err := c.Admin.CreateMigration(ctx, &pb.CreateMigrationRequest{
		SourceServers: []string{"game-1001"}, TargetServer: "game-1001",
	})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	if _, err := c.Admin.RollbackMigration(ctx, &pb.RollbackMigrationRequest{MigrationId: mig.Migration.Id}); err != nil {
		t.Fatalf("first RollbackMigration: %v", err)
	}
	if _, err := c.Admin.RollbackMigration(ctx, &pb.RollbackMigrationRequest{MigrationId: mig.Migration.Id}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("second rollback: expected InvalidArgument, got %v", err)
	}
}

// TestPbMetadataPatchSemantics pins the proto map lift: nil stays nil so an
// absent field keeps PATCH "no change" meaning, a present map is handed
// over by pointer.
func TestPbMetadataPatchSemantics(t *testing.T) {
	if got := pbMetadata(nil); got != nil {
		t.Errorf("pbMetadata(nil) = %v, want nil", got)
	}
	m := map[string]string{"zone": "pvp"}
	got := pbMetadata(m)
	if got == nil || (*got)["zone"] != "pvp" {
		t.Errorf("pbMetadata(map) = %v, want pointer carrying the map", got)
	}
}
