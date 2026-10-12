package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/fleet"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/store/memory"
	"github.com/cuihairu/atlas/internal/telemetry"
)

// TestGetLoadHeatmap covers the gRPC face of the heatmap: the row join with
// the fleet index, per-server buckets from the sampler, the default window,
// and the INVALID_ARGUMENT on an unknown tier.
func TestGetLoadHeatmap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mem := memory.New()
	idx := fleet.New()
	tracked := fleet.TrackStore(mem, idx)
	if err := idx.Reconcile(context.Background(), tracked, tracked); err != nil {
		t.Fatal(err)
	}

	sampler := telemetry.NewSampler(10*time.Millisecond, time.Minute)
	sampler.Probe(func() map[string]float64 {
		out := map[string]float64{}
		for _, s := range idx.ListAll() {
			out["load.server."+s.ID+".players"] = float64(s.Players)
			out["load.server."+s.ID+".load"] = s.Load
		}
		return out
	})
	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	go sampler.Run(sctx)

	g := grpc.NewServer()
	New(
		registry.New(tracked, tracked, testLogger()),
		discovery.New(tracked, tracked),
		nil, nil,
		admin.New(tracked),
		nil,
	).WithFleetIndex(idx).WithTelemetry(sampler).RegisterServices(g)

	lis := bufconn.Listen(1 << 20)
	go g.Serve(lis) //nolint:errcheck // test server
	defer g.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := pb.NewAdminServiceClient(conn)
	rc := pb.NewRegistryServiceClient(conn)

	if _, err := rc.Register(ctx, &pb.RegisterRequest{
		ServerId: "game-1001", Name: "One", Type: "game", Region: "cn-east", Capacity: 2000,
		Endpoint: &pb.Endpoint{Host: "10.0.0.1", Port: 30001},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := rc.Heartbeat(ctx, &pb.HeartbeatRequest{ServerId: "game-1001", Players: 300, Load: 0.3}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Wait until the sampler has a point for the server.
	deadline := time.Now().Add(2 * time.Second)
	for {
		pts := sampler.Series("load.server.game-1001.load", time.Hour)
		if len(pts) > 0 && pts[len(pts)-1].V == 0.3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sampler never observed game-1001 load")
		}
		time.Sleep(5 * time.Millisecond)
	}

	out, err := c.GetLoadHeatmap(ctx, &pb.LoadHeatmapRequest{})
	if err != nil {
		t.Fatalf("GetLoadHeatmap: %v", err)
	}
	if out.Window != "1h" || out.Bucket != "5m" || out.GeneratedAt == "" {
		t.Errorf("envelope: %+v", out)
	}
	if len(out.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(out.Servers))
	}
	row := out.Servers[0]
	if row.ServerId != "game-1001" || row.Players != 300 || row.Load != 0.3 || row.Capacity != 2000 {
		t.Errorf("row lost live record: %+v", row)
	}
	if len(row.Buckets) != 12 {
		t.Errorf("buckets = %d, want 12", len(row.Buckets))
	}
	last := row.Buckets[len(row.Buckets)-1]
	// Bucket values are averaged over whatever the sampler caught — a tick
	// between Register and Heartbeat adds a zero sample — so pin only that
	// the heartbeat flowed through the join (exact averages live in the
	// telemetry aggregator tests).
	if last.Samples == 0 || last.Load <= 0 || last.Players <= 0 {
		t.Errorf("last bucket = %+v, want sampled data", last)
	}

	// 24h window → 24 hourly buckets.
	out, err = c.GetLoadHeatmap(ctx, &pb.LoadHeatmapRequest{Window: "24h"})
	if err != nil {
		t.Fatalf("GetLoadHeatmap(24h): %v", err)
	}
	if out.Bucket != "1h" || len(out.Servers[0].Buckets) != 24 {
		t.Errorf("24h envelope: bucket=%q buckets=%d", out.Bucket, len(out.Servers[0].Buckets))
	}

	// Unknown window is INVALID_ARGUMENT, not a silent default.
	_, err = c.GetLoadHeatmap(ctx, &pb.LoadHeatmapRequest{Window: "7d"})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("window=7d: code = %v, want InvalidArgument", status.Code(err))
	}
}

// TestGetLoadHeatmapWithoutAttachments pins the embedder shape: no fleet
// index and no sampler is an empty grid, not an error and not nil.
func TestGetLoadHeatmapWithoutAttachments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mem := memory.New()
	g := grpc.NewServer()
	New(
		registry.New(mem, mem, testLogger()),
		discovery.New(mem, mem),
		nil, nil,
		admin.New(mem),
		nil,
	).RegisterServices(g)

	lis := bufconn.Listen(1 << 20)
	go g.Serve(lis) //nolint:errcheck // test server
	defer g.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	out, err := pb.NewAdminServiceClient(conn).GetLoadHeatmap(ctx, &pb.LoadHeatmapRequest{})
	if err != nil {
		t.Fatalf("GetLoadHeatmap: %v", err)
	}
	if len(out.Servers) != 0 {
		t.Errorf("servers = %d, want empty grid without a fleet index", len(out.Servers))
	}
}
