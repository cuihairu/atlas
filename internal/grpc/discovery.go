package grpc

import (
	"context"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// ListServers mirrors GET /v1/discovery/servers.
func (s *Server) ListServers(ctx context.Context, req *pb.ListServersRequest) (*pb.ListServersResponse, error) {
	f := store.ServerFilter{
		Status:   model.ServerStatus(req.Status),
		Region:   req.Region,
		Realm:    req.Realm,
		Shard:    req.Shard,
		Version:  req.Version,
		Platform: req.Platform,
		Limit:    int(req.Limit),
		Cursor:   req.Cursor,
	}
	servers, err := s.discovery.ListServers(ctx, f)
	if err != nil {
		return nil, statusErr(err)
	}
	out := &pb.ListServersResponse{Servers: make([]*pb.Server, 0, len(servers))}
	for _, srv := range servers {
		out.Servers = append(out.Servers, pbServer(srv))
	}
	return out, nil
}

// GetServer mirrors GET /v1/discovery/servers/{id}.
func (s *Server) GetServer(ctx context.Context, req *pb.GetServerRequest) (*pb.GetServerResponse, error) {
	srv, err := s.discovery.GetServer(ctx, req.ServerId)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.GetServerResponse{Server: pbServer(srv)}, nil
}
