// Package grpc provides the gRPC API for Atlas (TODO v0.1.5).
//
// It offers feature parity with the REST API: Registry, Discovery,
// Directory, Routing, and Admin services share the same underlying
// service layer, so behavior stays consistent across transports.
package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/cuihairu/atlas/api/gen/atlasv1"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store"
)

// Server implements all five Atlas gRPC services on top of the shared
// service layer.
type Server struct {
	pb.UnimplementedRegistryServer
	pb.UnimplementedDiscoveryServer
	pb.UnimplementedDirectoryServer
	pb.UnimplementedRoutingServer
	pb.UnimplementedAdminServer

	reg     *registry.Service
	disc    *discovery.Service
	dir     *directory.Service
	adm     *admin.Service
	routing *routing.Service
}

// New creates a gRPC Server. Any service may be nil if its RPCs are unused
// (helpers return Unimplemented in that case); production wires all five.
func New(reg *registry.Service, disc *discovery.Service, dir *directory.Service, adm *admin.Service, rt *routing.Service) *Server {
	return &Server{reg: reg, disc: disc, dir: dir, adm: adm, routing: rt}
}

// ---------------------------------------------------------------------------
// Error mapping
// ---------------------------------------------------------------------------

func toStatusErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, model.ErrNotFound) || errors.Is(err, store.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, model.ErrInvalid) || errors.Is(err, store.ErrInvalid) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// ---------------------------------------------------------------------------
// Model conversions
// ---------------------------------------------------------------------------

func serverToProto(s *model.Server) *pb.Server {
	if s == nil {
		return nil
	}
	out := &pb.Server{
		Id:       s.ID,
		Name:     s.Name,
		Type:     s.Type,
		Region:   s.Region,
		Version:  s.Version,
		Platform: s.Platform,
		Endpoint: &pb.Endpoint{
			Host: s.Endpoint.Host,
			Port: int32(s.Endpoint.Port),
		},
		Capacity: int32(s.Capacity),
		Metadata: s.Metadata,
		Status:   string(s.Status),
		Players:  int32(s.Players),
		Load:     s.Load,
	}
	if s.RealmID != nil {
		out.RealmId = *s.RealmID
	}
	if s.ShardID != nil {
		out.ShardId = *s.ShardID
	}
	if s.LastSeenAt != nil {
		out.LastSeenAt = timestamppb.New(*s.LastSeenAt)
	}
	if !s.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(s.CreatedAt)
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(s.UpdatedAt)
	}
	return out
}

func characterToProto(c *model.Character) *pb.Character {
	if c == nil {
		return nil
	}
	out := &pb.Character{
		AccountId:   c.AccountID,
		ServerId:    c.ServerID,
		CharacterId: c.CharacterID,
		Name:        c.Name,
		Level:       int32(c.Level),
		ClassId:     int32(c.ClassID),
		Avatar:      c.Avatar,
		Metadata:    c.Metadata,
	}
	if c.LastLoginAt != nil {
		out.LastLoginAt = timestamppb.New(*c.LastLoginAt)
	}
	if !c.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(c.CreatedAt)
	}
	if !c.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(c.UpdatedAt)
	}
	return out
}

func migrationToProto(m *model.Migration) *pb.Migration {
	if m == nil {
		return nil
	}
	out := &pb.Migration{
		Id:            m.ID,
		SourceServers: m.SourceServers,
		TargetServer:  m.TargetServer,
		Status:        string(m.Status),
	}
	if !m.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(m.StartedAt)
	}
	if m.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*m.CompletedAt)
	}
	return out
}

func statsToProto(s *model.Stats) *pb.Stats {
	if s == nil {
		return nil
	}
	toInt32 := func(m map[string]int) map[string]int32 {
		if m == nil {
			return nil
		}
		out := make(map[string]int32, len(m))
		for k, v := range m {
			out[k] = int32(v)
		}
		return out
	}
	return &pb.Stats{
		TotalServers:     int32(s.TotalServers),
		ServersByStatus:  toInt32(s.ServersByStatus),
		ServersByRegion:  toInt32(s.ServersByRegion),
		ServersByVersion: toInt32(s.ServersByVersion),
		TotalPlayers:     int32(s.TotalPlayers),
		TotalCapacity:    int32(s.TotalCapacity),
		TotalCharacters:  int32(s.TotalCharacters),
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Register implements RegistryServer.
func (s *Server) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if s.reg == nil {
		return nil, status.Error(codes.Unimplemented, "registry service not configured")
	}
	srv, err := s.reg.Register(ctx, registry.RegisterRequest{
		ID:       req.GetId(),
		Name:     req.GetName(),
		Type:     req.GetType(),
		Region:   req.GetRegion(),
		RealmID:  strPtrOrNil(req.GetRealmId()),
		ShardID:  strPtrOrNil(req.GetShardId()),
		Version:  req.GetVersion(),
		Platform: req.GetPlatform(),
		Endpoint: model.Endpoint{Host: req.GetEndpoint().GetHost(), Port: int(req.GetEndpoint().GetPort())},
		Capacity: int(req.GetCapacity()),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.RegisterResponse{ServerId: srv.ID, Status: string(srv.Status)}, nil
}

// Heartbeat implements RegistryServer.
func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if s.reg == nil {
		return nil, status.Error(codes.Unimplemented, "registry service not configured")
	}
	hb := model.Heartbeat{
		Players: int(req.GetHeartbeat().GetPlayers()),
		Load:    req.GetHeartbeat().GetLoad(),
		Status:  model.ServerStatus(req.GetHeartbeat().GetStatus()),
	}
	if err := s.reg.Heartbeat(ctx, req.GetServerId(), hb); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.HeartbeatResponse{ServerId: req.GetServerId(), Status: "online", NextHeartbeatIn: 10}, nil
}

// Unregister implements RegistryServer.
func (s *Server) Unregister(ctx context.Context, req *pb.UnregisterRequest) (*pb.UnregisterResponse, error) {
	if s.reg == nil {
		return nil, status.Error(codes.Unimplemented, "registry service not configured")
	}
	if err := s.reg.Unregister(ctx, req.GetServerId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.UnregisterResponse{ServerId: req.GetServerId(), Status: "offline"}, nil
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

// ListServers implements DiscoveryServer.
func (s *Server) ListServers(ctx context.Context, req *pb.ListServersRequest) (*pb.ListServersResponse, error) {
	if s.disc == nil {
		return nil, status.Error(codes.Unimplemented, "discovery service not configured")
	}
	servers, err := s.disc.ListServers(ctx, store.ServerFilter{
		Region:   req.GetRegion(),
		Realm:    req.GetRealm(),
		Shard:    req.GetShard(),
		Version:  req.GetVersion(),
		Platform: req.GetPlatform(),
		Status:   model.ServerStatus(req.GetStatus()),
		Limit:    int(req.GetLimit()),
		Cursor:   req.GetCursor(),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := &pb.ListServersResponse{Servers: make([]*pb.Server, 0, len(servers))}
	for _, srv := range servers {
		out.Servers = append(out.Servers, serverToProto(srv))
	}
	return out, nil
}

// GetServer implements DiscoveryServer.
func (s *Server) GetServer(ctx context.Context, req *pb.GetServerRequest) (*pb.Server, error) {
	if s.disc == nil {
		return nil, status.Error(codes.Unimplemented, "discovery service not configured")
	}
	srv, err := s.disc.GetServer(ctx, req.GetId())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return serverToProto(srv), nil
}

// ---------------------------------------------------------------------------
// Directory
// ---------------------------------------------------------------------------

// UpsertCharacter implements DirectoryServer.
func (s *Server) UpsertCharacter(ctx context.Context, req *pb.UpsertCharacterRequest) (*pb.Character, error) {
	if s.dir == nil {
		return nil, status.Error(codes.Unimplemented, "directory service not configured")
	}
	ch, err := s.dir.CreateCharacter(ctx, req.GetAccountId(), req.GetServerId(), req.GetCharacterId(), req.GetName(), int(req.GetLevel()), int(req.GetClassId()))
	if err != nil {
		return nil, toStatusErr(err)
	}
	// Apply avatar/metadata when provided (REST parity: create is minimal,
	// extras are patched on top).
	if req.GetAvatar() != "" || len(req.GetMetadata()) > 0 {
		patch := store.CharacterPatch{}
		if req.GetAvatar() != "" {
			avatar := req.GetAvatar()
			patch.Avatar = &avatar
		}
		if len(req.GetMetadata()) > 0 {
			md := req.GetMetadata()
			patch.Metadata = &md
		}
		updated, err := s.dir.UpdateCharacter(ctx, req.GetAccountId(), req.GetServerId(), req.GetCharacterId(), patch)
		if err != nil {
			return nil, toStatusErr(err)
		}
		ch = updated
	}
	return characterToProto(ch), nil
}

// GetCharacter implements DirectoryServer.
func (s *Server) GetCharacter(ctx context.Context, req *pb.GetCharacterRequest) (*pb.Character, error) {
	if s.dir == nil {
		return nil, status.Error(codes.Unimplemented, "directory service not configured")
	}
	ch, err := s.dir.GetCharacter(ctx, req.GetAccountId(), req.GetServerId(), req.GetCharacterId())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return characterToProto(ch), nil
}

// GetCharacterByID implements DirectoryServer.
func (s *Server) GetCharacterByID(ctx context.Context, req *pb.GetCharacterByIDRequest) (*pb.Character, error) {
	if s.dir == nil {
		return nil, status.Error(codes.Unimplemented, "directory service not configured")
	}
	ch, err := s.dir.GetCharacterByCharacterID(ctx, req.GetCharacterId())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return characterToProto(ch), nil
}

// ListByAccount implements DirectoryServer.
func (s *Server) ListByAccount(ctx context.Context, req *pb.ListByAccountRequest) (*pb.ListByAccountResponse, error) {
	if s.dir == nil {
		return nil, status.Error(codes.Unimplemented, "directory service not configured")
	}
	chars, err := s.dir.ListByAccount(ctx, req.GetAccountId())
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := &pb.ListByAccountResponse{Characters: make([]*pb.Character, 0, len(chars))}
	for _, ch := range chars {
		out.Characters = append(out.Characters, characterToProto(ch))
	}
	return out, nil
}

// ListByServer implements DirectoryServer.
func (s *Server) ListByServer(ctx context.Context, req *pb.ListByServerRequest) (*pb.ListByServerResponse, error) {
	if s.dir == nil {
		return nil, status.Error(codes.Unimplemented, "directory service not configured")
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	chars, next, err := s.dir.ListByServer(ctx, req.GetServerId(), limit, req.GetCursor())
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := &pb.ListByServerResponse{NextCursor: next, Characters: make([]*pb.Character, 0, len(chars))}
	for _, ch := range chars {
		out.Characters = append(out.Characters, characterToProto(ch))
	}
	return out, nil
}

// UpdateCharacter implements DirectoryServer.
func (s *Server) UpdateCharacter(ctx context.Context, req *pb.UpdateCharacterRequest) (*pb.Character, error) {
	if s.dir == nil {
		return nil, status.Error(codes.Unimplemented, "directory service not configured")
	}
	patch := store.CharacterPatch{}
	if req.GetHasName() {
		name := req.GetName()
		patch.Name = &name
	}
	if req.GetHasLevel() {
		level := int(req.GetLevel())
		patch.Level = &level
	}
	if req.GetHasClassId() {
		classID := int(req.GetClassId())
		patch.ClassID = &classID
	}
	if req.GetHasAvatar() {
		avatar := req.GetAvatar()
		patch.Avatar = &avatar
	}
	ch, err := s.dir.UpdateCharacter(ctx, req.GetAccountId(), req.GetServerId(), req.GetCharacterId(), patch)
	if err != nil {
		return nil, toStatusErr(err)
	}
	return characterToProto(ch), nil
}

// DeleteCharacter implements DirectoryServer.
func (s *Server) DeleteCharacter(ctx context.Context, req *pb.DeleteCharacterRequest) (*pb.DeleteCharacterResponse, error) {
	if s.dir == nil {
		return nil, status.Error(codes.Unimplemented, "directory service not configured")
	}
	if err := s.dir.DeleteCharacter(ctx, req.GetAccountId(), req.GetServerId(), req.GetCharacterId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.DeleteCharacterResponse{Status: "deleted"}, nil
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

// Recommend implements RoutingServer.
func (s *Server) Recommend(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	if s.routing == nil {
		return nil, status.Error(codes.Unimplemented, "routing service not configured")
	}
	srv, reason, err := s.routing.Recommend(ctx, routing.Request{
		AccountID: req.GetAccountId(),
		Region:    req.GetRegion(),
		Version:   req.GetVersion(),
		Platform:  req.GetPlatform(),
		Status:    model.ServerStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.RecommendResponse{Server: serverToProto(srv), Reason: reason}, nil
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

// SetMaintenance implements AdminServer.
func (s *Server) SetMaintenance(ctx context.Context, req *pb.ServerLifecycleRequest) (*pb.ServerLifecycleResponse, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	if err := s.adm.SetMaintenance(ctx, req.GetServerId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.ServerLifecycleResponse{ServerId: req.GetServerId(), Status: "maintenance"}, nil
}

// SetDrain implements AdminServer.
func (s *Server) SetDrain(ctx context.Context, req *pb.ServerLifecycleRequest) (*pb.ServerLifecycleResponse, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	if err := s.adm.SetDrain(ctx, req.GetServerId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.ServerLifecycleResponse{ServerId: req.GetServerId(), Status: "draining"}, nil
}

// Enable implements AdminServer.
func (s *Server) Enable(ctx context.Context, req *pb.ServerLifecycleRequest) (*pb.ServerLifecycleResponse, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	if err := s.adm.Enable(ctx, req.GetServerId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.ServerLifecycleResponse{ServerId: req.GetServerId(), Status: "online"}, nil
}

// Disable implements AdminServer.
func (s *Server) Disable(ctx context.Context, req *pb.ServerLifecycleRequest) (*pb.ServerLifecycleResponse, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	if err := s.adm.Disable(ctx, req.GetServerId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.ServerLifecycleResponse{ServerId: req.GetServerId(), Status: "disabled"}, nil
}

// GetStats implements AdminServer.
func (s *Server) GetStats(ctx context.Context, _ *pb.GetStatsRequest) (*pb.Stats, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	stats, err := s.adm.GetStats(ctx)
	if err != nil {
		return nil, toStatusErr(err)
	}
	return statsToProto(stats), nil
}

// SearchCharacters implements AdminServer.
func (s *Server) SearchCharacters(ctx context.Context, req *pb.SearchCharactersRequest) (*pb.SearchCharactersResponse, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	filter := store.CharacterSearchFilter{
		Name:     req.GetName(),
		ServerID: req.GetServerId(),
		Limit:    int(req.GetLimit()),
		Cursor:   req.GetCursor(),
	}
	if req.GetHasClassId() {
		v := int(req.GetClassId())
		filter.ClassID = &v
	}
	if req.GetHasMinLevel() {
		v := int(req.GetMinLevel())
		filter.MinLevel = &v
	}
	if req.GetHasMaxLevel() {
		v := int(req.GetMaxLevel())
		filter.MaxLevel = &v
	}
	chars, next, err := s.adm.SearchCharacters(ctx, filter)
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := &pb.SearchCharactersResponse{NextCursor: next, Characters: make([]*pb.Character, 0, len(chars))}
	for _, ch := range chars {
		out.Characters = append(out.Characters, characterToProto(ch))
	}
	return out, nil
}

// CreateMigration implements AdminServer.
func (s *Server) CreateMigration(ctx context.Context, req *pb.CreateMigrationRequest) (*pb.Migration, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	m, err := s.adm.CreateMigration(ctx, admin.CreateMigrationRequest{
		SourceServers: req.GetSourceServers(),
		TargetServer:  req.GetTargetServer(),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}
	return migrationToProto(m), nil
}

// GetMigration implements AdminServer.
func (s *Server) GetMigration(ctx context.Context, req *pb.GetMigrationRequest) (*pb.Migration, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	m, err := s.adm.GetMigration(ctx, req.GetId())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return migrationToProto(m), nil
}

// ListMigrations implements AdminServer.
func (s *Server) ListMigrations(ctx context.Context, req *pb.ListMigrationsRequest) (*pb.ListMigrationsResponse, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	migrations, err := s.adm.ListMigrations(ctx, limit)
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := &pb.ListMigrationsResponse{Migrations: make([]*pb.Migration, 0, len(migrations))}
	for _, m := range migrations {
		out.Migrations = append(out.Migrations, migrationToProto(m))
	}
	return out, nil
}

// RollbackMigration implements AdminServer.
func (s *Server) RollbackMigration(ctx context.Context, req *pb.RollbackMigrationRequest) (*pb.RollbackMigrationResponse, error) {
	if s.adm == nil {
		return nil, status.Error(codes.Unimplemented, "admin service not configured")
	}
	if err := s.adm.RollbackMigration(ctx, req.GetId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &pb.RollbackMigrationResponse{Id: req.GetId(), Status: "rolled_back"}, nil
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
