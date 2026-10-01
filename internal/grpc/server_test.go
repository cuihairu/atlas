package grpc

import (
	"context"
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
		routing.New(mem, mem, mem),
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
