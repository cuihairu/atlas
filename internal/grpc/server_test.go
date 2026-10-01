package grpc

import (
	"context"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/cuihairu/atlas/api/gen/atlasv1"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newTestClients(t *testing.T) (pb.RegistryClient, pb.DiscoveryClient, pb.DirectoryClient, pb.RoutingClient, pb.AdminClient) {
	t.Helper()
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	regSvc := registry.New(mem, mem, logger)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem)
	admSvc := admin.New(mem)
	rtSvc := routing.New(mem, mem, mem)
	srv := New(regSvc, discSvc, dirSvc, admSvc, rtSvc)

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterRegistryServer(gs, srv)
	pb.RegisterDiscoveryServer(gs, srv)
	pb.RegisterDirectoryServer(gs, srv)
	pb.RegisterRoutingServer(gs, srv)
	pb.RegisterAdminServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	dial := func(t *testing.T) *grpc.ClientConn {
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	conn := dial(t)
	return pb.NewRegistryClient(conn), pb.NewDiscoveryClient(conn),
		pb.NewDirectoryClient(conn), pb.NewRoutingClient(conn), pb.NewAdminClient(conn)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestRegistryDiscoveryRoutingParity(t *testing.T) {
	reg, disc, _, routingCli, _ := newTestClients(t)
	ctx := context.Background()

	if _, err := reg.Register(ctx, &pb.RegisterRequest{
		Id: "game-1001", Name: "one", Type: "game", Region: "cn-east",
		Version: "1.8.2", Platform: "android",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 2000,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := reg.Heartbeat(ctx, &pb.HeartbeatRequest{
		ServerId: "game-1001", Heartbeat: &pb.Heartbeat{Players: 10, Load: 0.1},
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	got, err := disc.GetServer(ctx, &pb.GetServerRequest{Id: "game-1001"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.GetId() != "game-1001" || got.GetPlayers() != 10 {
		t.Fatalf("unexpected server: %+v", got)
	}

	list, err := disc.ListServers(ctx, &pb.ListServersRequest{Region: "cn-east"})
	if err != nil || len(list.GetServers()) != 1 {
		t.Fatalf("list: %v %+v", err, list)
	}

	rec, err := routingCli.Recommend(ctx, &pb.RecommendRequest{Region: "cn-east"})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if rec.GetServer().GetId() != "game-1001" || rec.GetReason() == "" {
		t.Fatalf("unexpected recommendation: %+v", rec)
	}
}

func TestDirectoryAdminParity(t *testing.T) {
	reg, _, dir, _, adm := newTestClients(t)
	ctx := context.Background()

	if _, err := reg.Register(ctx, &pb.RegisterRequest{
		Id: "game-1001", Region: "cn-east",
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 2000,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	ch, err := dir.UpsertCharacter(ctx, &pb.UpsertCharacterRequest{
		AccountId: 10001, ServerId: "game-1001", CharacterId: 823712,
		Name: "sword", Level: 10, ClassId: 3,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if ch.GetName() != "sword" {
		t.Fatalf("unexpected character: %+v", ch)
	}

	byID, err := dir.GetCharacterByID(ctx, &pb.GetCharacterByIDRequest{CharacterId: 823712})
	if err != nil || byID.GetAccountId() != 10001 {
		t.Fatalf("get by id: %v %+v", err, byID)
	}

	updated, err := dir.UpdateCharacter(ctx, &pb.UpdateCharacterRequest{
		AccountId: 10001, ServerId: "game-1001", CharacterId: 823712,
		Level: 11, HasLevel: true,
	})
	if err != nil || updated.GetLevel() != 11 {
		t.Fatalf("update: %v %+v", err, updated)
	}

	stats, err := adm.GetStats(ctx, &pb.GetStatsRequest{})
	if err != nil || stats.GetTotalServers() != 1 {
		t.Fatalf("stats: %v %+v", err, stats)
	}

	m, err := adm.CreateMigration(ctx, &pb.CreateMigrationRequest{
		SourceServers: []string{"game-1001"}, TargetServer: "game-1001",
	})
	if err != nil || m.GetId() == "" {
		t.Fatalf("create migration: %v %+v", err, m)
	}
	if _, err := adm.RollbackMigration(ctx, &pb.RollbackMigrationRequest{Id: m.GetId()}); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if _, err := dir.DeleteCharacter(ctx, &pb.DeleteCharacterRequest{
		AccountId: 10001, ServerId: "game-1001", CharacterId: 823712,
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
