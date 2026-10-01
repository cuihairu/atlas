package grpc

import (
	"context"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
)

// Register mirrors POST /v1/registry/servers/register.
func (s *Server) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	regReq := registry.RegisterRequest{
		ID:       req.ServerId,
		Name:     req.Name,
		Type:     req.Type,
		Region:   req.Region,
		Version:  req.Version,
		Platform: req.Platform,
		Capacity: int(req.Capacity),
	}
	if req.RealmId != "" {
		regReq.RealmID = &req.RealmId
	}
	if req.ShardId != "" {
		regReq.ShardID = &req.ShardId
	}
	if req.Endpoint != nil {
		regReq.Endpoint = model.Endpoint{Host: req.Endpoint.Host, Port: int(req.Endpoint.Port)}
	}

	srv, err := s.registry.Register(ctx, regReq)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.RegisterResponse{Server: pbServer(srv)}, nil
}

// Heartbeat mirrors POST /v1/registry/servers/{id}/heartbeat.
func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	hb := model.Heartbeat{
		Players: int(req.Players),
		Load:    req.Load,
	}
	if req.Status != "" {
		hb.Status = model.ServerStatus(req.Status)
	}
	st, err := s.registry.Heartbeat(ctx, req.ServerId, hb)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.HeartbeatResponse{
		ServerId:        req.ServerId,
		Status:          string(st),
		NextHeartbeatIn: 10,
	}, nil
}

// Unregister mirrors POST /v1/registry/servers/{id}/unregister.
func (s *Server) Unregister(ctx context.Context, req *pb.UnregisterRequest) (*pb.UnregisterResponse, error) {
	if err := s.registry.Unregister(ctx, req.ServerId); err != nil {
		return nil, statusErr(err)
	}
	return &pb.UnregisterResponse{
		ServerId: req.ServerId,
		Status:   string(model.StatusOffline),
	}, nil
}
