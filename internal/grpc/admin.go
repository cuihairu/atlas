package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/telemetry"
)

// SetMaintenance mirrors POST /v1/admin/servers/{id}/maintenance.
func (s *Server) SetMaintenance(ctx context.Context, req *pb.ServerIdRequest) (*pb.OkResponse, error) {
	if err := s.admin.SetMaintenance(ctx, req.ServerId); err != nil {
		return nil, statusErr(err)
	}
	return &pb.OkResponse{ServerId: req.ServerId, Status: "maintenance"}, nil
}

// SetDrain mirrors POST /v1/admin/servers/{id}/drain.
func (s *Server) SetDrain(ctx context.Context, req *pb.ServerIdRequest) (*pb.OkResponse, error) {
	if err := s.admin.SetDrain(ctx, req.ServerId); err != nil {
		return nil, statusErr(err)
	}
	return &pb.OkResponse{ServerId: req.ServerId, Status: "draining"}, nil
}

// Enable mirrors POST /v1/admin/servers/{id}/enable.
func (s *Server) Enable(ctx context.Context, req *pb.ServerIdRequest) (*pb.OkResponse, error) {
	if err := s.admin.Enable(ctx, req.ServerId); err != nil {
		return nil, statusErr(err)
	}
	return &pb.OkResponse{ServerId: req.ServerId, Status: "online"}, nil
}

// Disable mirrors POST /v1/admin/servers/{id}/disable.
func (s *Server) Disable(ctx context.Context, req *pb.ServerIdRequest) (*pb.OkResponse, error) {
	if err := s.admin.Disable(ctx, req.ServerId); err != nil {
		return nil, statusErr(err)
	}
	return &pb.OkResponse{ServerId: req.ServerId, Status: "disabled"}, nil
}

// GetStats mirrors GET /v1/admin/stats.
func (s *Server) GetStats(ctx context.Context, _ *pb.GetStatsRequest) (*pb.GetStatsResponse, error) {
	stats, err := s.admin.GetStats(ctx)
	if err != nil {
		return nil, statusErr(err)
	}
	out := &pb.GetStatsResponse{
		TotalServers:     int32(stats.TotalServers),
		ServersByStatus:  make(map[string]int32, len(stats.ServersByStatus)),
		ServersByRegion:  make(map[string]int32, len(stats.ServersByRegion)),
		ServersByVersion: make(map[string]int32, len(stats.ServersByVersion)),
		TotalPlayers:     int32(stats.TotalPlayers),
		TotalCapacity:    int32(stats.TotalCapacity),
		TotalCharacters:  int32(stats.TotalCharacters),
	}
	for k, v := range stats.ServersByStatus {
		out.ServersByStatus[k] = int32(v)
	}
	for k, v := range stats.ServersByRegion {
		out.ServersByRegion[k] = int32(v)
	}
	for k, v := range stats.ServersByVersion {
		out.ServersByVersion[k] = int32(v)
	}
	return out, nil
}

// GetLoadHeatmap mirrors GET /v1/admin/load-heatmap (服务器热力图): one row
// per live server, bucketed load averages over the window. The fleet index
// and sampler are optional attachments — without them rows come back with
// empty buckets, never an error.
func (s *Server) GetLoadHeatmap(ctx context.Context, req *pb.LoadHeatmapRequest) (*pb.LoadHeatmapResponse, error) {
	window, bucketCount, ok := telemetry.ParseHeatmapWindow(req.GetWindow())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "window must be one of 1h/24h")
	}
	windowLabel := req.GetWindow()
	if windowLabel == "" {
		windowLabel = "1h"
	}

	var servers []*model.Server
	if s.fleetIdx != nil {
		servers = s.fleetIdx.ListAll()
	}
	var series func(string, time.Duration) []telemetry.Point
	if s.sampler != nil {
		series = s.sampler.Series
	}
	rows := telemetry.BuildServerHeatmap(servers, series, window, bucketCount, time.Now())

	out := &pb.LoadHeatmapResponse{
		Window:      windowLabel,
		Bucket:      telemetry.BucketSpanLabel(window / time.Duration(bucketCount)),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Servers:     make([]*pb.LoadHeatmapServer, 0, len(rows)),
	}
	for _, r := range rows {
		row := &pb.LoadHeatmapServer{
			ServerId: r.ServerID,
			Name:     r.Name,
			Region:   r.Region,
			Type:     r.Type,
			Status:   r.Status,
			Players:  int32(r.Players),
			Capacity: int32(r.Capacity),
			Load:     r.Load,
			Buckets:  make([]*pb.LoadHeatmapBucket, 0, len(r.Buckets)),
		}
		for _, b := range r.Buckets {
			row.Buckets = append(row.Buckets, &pb.LoadHeatmapBucket{
				T:       b.T.UTC().Format(time.RFC3339),
				Load:    b.Load,
				Players: b.Players,
				Samples: int32(b.Samples),
			})
		}
		out.Servers = append(out.Servers, row)
	}
	return out, nil
}

// SearchCharacters mirrors GET /v1/admin/characters/search. The deprecated
// class_id field is accepted but ignored (platform de-hardening): filter via
// metadata_key/metadata_value instead.
func (s *Server) SearchCharacters(ctx context.Context, req *pb.SearchCharactersRequest) (*pb.SearchCharactersResponse, error) {
	filter := store.CharacterSearchFilter{
		Name:          req.Name,
		ServerID:      req.ServerId,
		AccountID:     req.AccountId,
		MetadataKey:   req.MetadataKey,
		MetadataValue: req.MetadataValue,
		Limit:         int(req.Limit),
		Cursor:        req.Cursor,
	}
	if req.MinLevel != nil {
		v := int(*req.MinLevel)
		filter.MinLevel = &v
	}
	if req.MaxLevel != nil {
		v := int(*req.MaxLevel)
		filter.MaxLevel = &v
	}

	chars, next, err := s.admin.SearchCharacters(ctx, filter)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.SearchCharactersResponse{Characters: pbCharacters(chars), NextCursor: next}, nil
}

// CreateMigration mirrors POST /v1/admin/migrations.
func (s *Server) CreateMigration(ctx context.Context, req *pb.CreateMigrationRequest) (*pb.CreateMigrationResponse, error) {
	m, err := s.admin.CreateMigration(ctx, admin.CreateMigrationRequest{
		SourceServers: req.SourceServers,
		TargetServer:  req.TargetServer,
	})
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.CreateMigrationResponse{Migration: pbMigration(m)}, nil
}

// GetMigration mirrors GET /v1/admin/migrations/{id}.
func (s *Server) GetMigration(ctx context.Context, req *pb.GetMigrationRequest) (*pb.GetMigrationResponse, error) {
	m, err := s.admin.GetMigration(ctx, req.MigrationId)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.GetMigrationResponse{Migration: pbMigration(m)}, nil
}

// ListMigrations mirrors GET /v1/admin/migrations.
func (s *Server) ListMigrations(ctx context.Context, req *pb.ListMigrationsRequest) (*pb.ListMigrationsResponse, error) {
	migrations, err := s.admin.ListMigrations(ctx, int(req.Limit))
	if err != nil {
		return nil, statusErr(err)
	}
	out := &pb.ListMigrationsResponse{Migrations: make([]*pb.Migration, 0, len(migrations))}
	for _, m := range migrations {
		out.Migrations = append(out.Migrations, pbMigration(m))
	}
	return out, nil
}

// RollbackMigration mirrors POST /v1/admin/migrations/{id}/rollback.
func (s *Server) RollbackMigration(ctx context.Context, req *pb.RollbackMigrationRequest) (*pb.RollbackMigrationResponse, error) {
	if err := s.admin.RollbackMigration(ctx, req.MigrationId); err != nil {
		return nil, statusErr(err)
	}
	m, err := s.admin.GetMigration(ctx, req.MigrationId)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.RollbackMigrationResponse{Migration: pbMigration(m)}, nil
}
