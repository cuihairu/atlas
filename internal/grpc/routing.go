package grpc

import (
	"context"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/routing"
)

// Recommend mirrors GET /v1/routing/recommended.
func (s *Server) Recommend(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	srv, reason, err := s.routing.Recommend(ctx, routing.Request{
		AccountID: req.AccountId,
		Region:    req.Region,
		Version:   req.Version,
		Platform:  req.Platform,
		Status:    model.ServerStatus(req.Status),
	})
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.RecommendResponse{Server: pbServer(srv), Reason: reason}, nil
}
