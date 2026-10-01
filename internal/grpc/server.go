// Package grpc implements the gRPC API (TODO v0.1.5) with feature parity
// against the REST endpoints in docs/api.md. Every RPC delegates to the
// same services the HTTP handlers use; directory writes travel through the
// event adapter just like their REST counterparts.
package grpc

import (
	"errors"

	"google.golang.org/grpc"
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
)

// Server implements all Atlas gRPC services.
type Server struct {
	pb.UnimplementedRegistryServiceServer
	pb.UnimplementedDiscoveryServiceServer
	pb.UnimplementedDirectoryServiceServer
	pb.UnimplementedRoutingServiceServer
	pb.UnimplementedAdminServiceServer

	registry  *registry.Service
	discovery *discovery.Service
	directory *directory.Service
	routing   *routing.Service
	admin     *admin.Service
	events    event.EventAdapter
}

// New creates a gRPC server backed by the shared service layer.
func New(reg *registry.Service, disc *discovery.Service, dir *directory.Service, rt *routing.Service, adm *admin.Service, events event.EventAdapter) *Server {
	return &Server{
		registry:  reg,
		discovery: disc,
		directory: dir,
		routing:   rt,
		admin:     adm,
		events:    events,
	}
}

// RegisterServices mounts every service on a google.golang.org/grpc
// server. (Named to avoid clashing with the Register RPC.)
func (s *Server) RegisterServices(g *grpc.Server) {
	pb.RegisterRegistryServiceServer(g, s)
	pb.RegisterDiscoveryServiceServer(g, s)
	pb.RegisterDirectoryServiceServer(g, s)
	pb.RegisterRoutingServiceServer(g, s)
	pb.RegisterAdminServiceServer(g, s)
}

// statusErr maps domain errors to gRPC status codes.
func statusErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, model.ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, model.ErrConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
