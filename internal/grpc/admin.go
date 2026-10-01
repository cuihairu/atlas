package grpc

import (
	"context"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/store"
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

// SearchCharacters mirrors GET /v1/admin/characters/search.
func (s *Server) SearchCharacters(ctx context.Context, req *pb.SearchCharactersRequest) (*pb.SearchCharactersResponse, error) {
	filter := store.CharacterSearchFilter{
		Name:     req.Name,
		ServerID: req.ServerId,
		Limit:    int(req.Limit),
		Cursor:   req.Cursor,
	}
	if req.ClassId != nil {
		v := int(*req.ClassId)
		filter.ClassID = &v
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
