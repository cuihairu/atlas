package atlas

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
)

// grpcBackend speaks gRPC against the single ATLAS_GRPC_ADDR port where
// all five services are mounted (docs/api.md §gRPC).
type grpcBackend struct {
	conn      *grpc.ClientConn
	policy    retryPolicy
	transport Transport

	registry *pb.RegistryServiceClient
	discover *pb.DiscoveryServiceClient
	director *pb.DirectoryServiceClient
	routing  *pb.RoutingServiceClient
	admin    *pb.AdminServiceClient

	registryToken string
	adminAPIKey   string
}

func newGRPCBackend(opts Options, policy retryPolicy) (*grpcBackend, error) {
	conn, err := grpc.NewClient(
		opts.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("atlas: grpc dial %s: %w", opts.Addr, err)
	}
	reg, disc, dir, rt, adm := pb.NewRegistryServiceClient(conn), pb.NewDiscoveryServiceClient(conn),
		pb.NewDirectoryServiceClient(conn), pb.NewRoutingServiceClient(conn), pb.NewAdminServiceClient(conn)
	return &grpcBackend{
		conn:          conn,
		policy:        policy,
		transport:     opts.Transport,
		registry:      &reg,
		discover:      &disc,
		director:      &dir,
		routing:       &rt,
		admin:         &adm,
		registryToken: opts.RegistryToken,
		adminAPIKey:   opts.AdminAPIKey,
	}, nil
}

// ctx attaches bearer auth matching the API group and a default timeout
// when the caller did not set a deadline.
func (b *grpcBackend) callCtx(ctx context.Context, bearer string) (context.Context, context.CancelFunc) {
	if bearer != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+bearer)
	}
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		return ctx, cancel
	}
	return ctx, func() {}
}

// retry runs fn while the gRPC status is retryable (Unavailable /
// DeadlineExceeded / Internal).
func (b *grpcBackend) retry(ctx context.Context, fn func() error) error {
	max := b.policy.max
	if max < 0 {
		max = 0
	}
	var err error
	for attempt := 0; ; attempt++ {
		err = fn()
		if err == nil || attempt >= max || !isTransientGRPC(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.policy.nextDelay(attempt)):
		}
	}
}

func isTransientGRPC(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Internal:
		return err != nil
	default:
		return false
	}
}

func (b *grpcBackend) close(_ context.Context) error { return b.conn.Close() }

// grpcError converts a gRPC status into *Error with uppercase codes.
func grpcError(err error) error {
	if err == nil {
		return nil
	}
	s, ok := status.FromError(err)
	if !ok {
		return err
	}
	return &Error{Code: s.Code().String(), Message: s.Message()}
}

func (b *grpcBackend) register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	ctx, cancel := b.callCtx(ctx, b.registryToken)
	defer cancel()
	var out *RegisterResult
	err := b.retry(ctx, func() error {
		resp, err := (*b.registry).Register(ctx, &pb.RegisterRequest{
			ServerId: req.ServerID, Name: req.Name, Type: req.Type, Region: req.Region,
			RealmId: strDeref(req.RealmID), ShardId: strDeref(req.ShardID),
			Version: req.Version, Platform: req.Platform,
			Endpoint: &pb.Endpoint{Host: req.Endpoint.Host, Port: int32(req.Endpoint.Port)},
			Capacity: int32(req.Capacity),
		})
		if err != nil {
			return err
		}
		out = &RegisterResult{ServerID: resp.Server.GetId(), Status: resp.Server.GetStatus()}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) heartbeat(ctx context.Context, serverID string, req HeartbeatRequest) (*HeartbeatResult, error) {
	ctx, cancel := b.callCtx(ctx, b.registryToken)
	defer cancel()
	var out *HeartbeatResult
	err := b.retry(ctx, func() error {
		resp, err := (*b.registry).Heartbeat(ctx, &pb.HeartbeatRequest{
			ServerId: serverID, Players: int32(req.Players), Load: req.Load,
		})
		if err != nil {
			return err
		}
		out = &HeartbeatResult{ServerID: serverID, Status: resp.Status}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) unregister(ctx context.Context, serverID string) (*StatusResult, error) {
	ctx, cancel := b.callCtx(ctx, b.registryToken)
	defer cancel()
	var out *StatusResult
	err := b.retry(ctx, func() error {
		resp, err := (*b.registry).Unregister(ctx, &pb.UnregisterRequest{ServerId: serverID})
		if err != nil {
			return err
		}
		out = &StatusResult{ServerID: serverID, Status: resp.Status}
		return nil
	})
	return out, grpcError(err)
}

// fetchCrossServerConfig is REST-only for now: the config pull endpoint
// has no gRPC counterpart, so a gRPC-transport client reaches the
// Registry HTTP listener directly through the REST base. Servers that
// need the config center should use the REST transport (the default).
func (b *grpcBackend) fetchCrossServerConfig(ctx context.Context) (*CrossServerConfig, error) {
	return nil, fmt.Errorf("atlas: cross-server config pull requires the REST transport (got %s)", b.transport)
}

func (b *grpcBackend) listServers(ctx context.Context, f ServerFilter) ([]Server, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out []Server
	err := b.retry(ctx, func() error {
		resp, err := (*b.discover).ListServers(ctx, &pb.ListServersRequest{
			Region: f.Region, Version: f.Version, Platform: f.Platform, Status: f.Status, Limit: int32(f.Limit),
		})
		if err != nil {
			return err
		}
		out = make([]Server, 0, len(resp.Servers))
		for _, s := range resp.Servers {
			out = append(out, serverFromPB(s))
		}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) getServer(ctx context.Context, id string) (*Server, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out *Server
	err := b.retry(ctx, func() error {
		resp, err := (*b.discover).GetServer(ctx, &pb.GetServerRequest{ServerId: id})
		if err != nil {
			return err
		}
		s := serverFromPB(resp.Server)
		out = &s
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) createCharacter(ctx context.Context, req CreateCharacterRequest) (*CharacterWriteResult, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out *CharacterWriteResult
	err := b.retry(ctx, func() error {
		resp, err := (*b.director).CreateCharacter(ctx, &pb.CreateCharacterRequest{
			AccountId: req.AccountID, ServerId: req.ServerID, CharacterId: req.CharacterID,
			Name: req.Name, Level: int32(req.Level), ClassId: int32(req.ClassID),
		})
		if err != nil {
			return err
		}
		out = characterResultFromPB(resp.Character, resp.Status)
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) getCharacter(ctx context.Context, characterID int64) (*Character, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out *Character
	err := b.retry(ctx, func() error {
		resp, err := (*b.director).GetCharacter(ctx, &pb.GetCharacterRequest{CharacterId: characterID})
		if err != nil {
			return err
		}
		out = characterFromPB(resp.Character)
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) listCharactersByAccount(ctx context.Context, accountID int64) ([]Character, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out []Character
	err := b.retry(ctx, func() error {
		resp, err := (*b.director).ListCharactersByAccount(ctx, &pb.ListCharactersByAccountRequest{AccountId: accountID})
		if err != nil {
			return err
		}
		out = charactersFromPB(resp.Characters)
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) listCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) (*CharacterPage, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out *CharacterPage
	err := b.retry(ctx, func() error {
		resp, err := (*b.director).ListCharactersByServer(ctx, &pb.ListCharactersByServerRequest{
			ServerId: serverID, Limit: int32(limit), Cursor: cursor,
		})
		if err != nil {
			return err
		}
		out = &CharacterPage{Characters: charactersFromPB(resp.Characters), NextCursor: resp.NextCursor}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) updateCharacter(ctx context.Context, characterID int64, req UpdateCharacterRequest) (*CharacterWriteResult, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	pbReq := &pb.UpdateCharacterRequest{CharacterId: characterID}
	if req.Name != nil {
		pbReq.Name = req.Name
	}
	if req.Level != nil {
		v := int32(*req.Level)
		pbReq.Level = &v
	}
	if req.ClassID != nil {
		v := int32(*req.ClassID)
		pbReq.ClassId = &v
	}
	if req.Avatar != nil {
		pbReq.Avatar = req.Avatar
	}
	var out *CharacterWriteResult
	err := b.retry(ctx, func() error {
		resp, err := (*b.director).UpdateCharacter(ctx, pbReq)
		if err != nil {
			return err
		}
		out = characterResultFromPB(resp.Character, resp.Status)
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) deleteCharacter(ctx context.Context, characterID int64) (*CharacterWriteResult, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out *CharacterWriteResult
	err := b.retry(ctx, func() error {
		resp, err := (*b.director).DeleteCharacter(ctx, &pb.DeleteCharacterRequest{CharacterId: characterID})
		if err != nil {
			return err
		}
		out = &CharacterWriteResult{Status: resp.Status}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) recommend(ctx context.Context, accountID int64, region, version, platform string) (*Recommendation, error) {
	ctx, cancel := b.callCtx(ctx, "")
	defer cancel()
	var out *Recommendation
	err := b.retry(ctx, func() error {
		resp, err := (*b.routing).Recommend(ctx, &pb.RecommendRequest{
			AccountId: accountID, Region: region, Version: version, Platform: platform,
		})
		if err != nil {
			return err
		}
		out = &Recommendation{Server: serverFromPB(resp.Server), Reason: resp.Reason}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) lifecycle(ctx context.Context, call func(context.Context, *pb.ServerIdRequest) (*pb.OkResponse, error), serverID string) (*StatusResult, error) {
	ctx, cancel := b.callCtx(ctx, b.adminAPIKey)
	defer cancel()
	var out *StatusResult
	err := b.retry(ctx, func() error {
		resp, err := call(ctx, &pb.ServerIdRequest{ServerId: serverID})
		if err != nil {
			return err
		}
		out = &StatusResult{ServerID: resp.ServerId, Status: resp.Status}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) setMaintenance(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, func(c context.Context, r *pb.ServerIdRequest) (*pb.OkResponse, error) {
		return (*b.admin).SetMaintenance(c, r)
	}, serverID)
}

func (b *grpcBackend) setDrain(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, func(c context.Context, r *pb.ServerIdRequest) (*pb.OkResponse, error) {
		return (*b.admin).SetDrain(c, r)
	}, serverID)
}

func (b *grpcBackend) enable(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, func(c context.Context, r *pb.ServerIdRequest) (*pb.OkResponse, error) {
		return (*b.admin).Enable(c, r)
	}, serverID)
}

func (b *grpcBackend) disable(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, func(c context.Context, r *pb.ServerIdRequest) (*pb.OkResponse, error) {
		return (*b.admin).Disable(c, r)
	}, serverID)
}

func (b *grpcBackend) stats(ctx context.Context) (*Stats, error) {
	ctx, cancel := b.callCtx(ctx, b.adminAPIKey)
	defer cancel()
	var out *Stats
	err := b.retry(ctx, func() error {
		resp, err := (*b.admin).GetStats(ctx, &pb.GetStatsRequest{})
		if err != nil {
			return err
		}
		out = &Stats{
			TotalServers: int(resp.TotalServers), ServersByStatus: int32Map(resp.ServersByStatus),
			ServersByRegion: int32Map(resp.ServersByRegion), ServersByVersion: int32Map(resp.ServersByVersion),
			TotalPlayers: int(resp.TotalPlayers), TotalCapacity: int(resp.TotalCapacity),
			TotalCharacters: int(resp.TotalCharacters),
		}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) searchCharacters(ctx context.Context, f CharacterFilter) (*CharacterPage, error) {
	ctx, cancel := b.callCtx(ctx, b.adminAPIKey)
	defer cancel()
	pbReq := &pb.SearchCharactersRequest{Name: f.Name, ServerId: f.ServerID, Limit: int32(f.Limit), Cursor: f.Cursor}
	if f.ClassID != nil {
		v := int32(*f.ClassID)
		pbReq.ClassId = &v
	}
	if f.MinLevel != nil {
		v := int32(*f.MinLevel)
		pbReq.MinLevel = &v
	}
	if f.MaxLevel != nil {
		v := int32(*f.MaxLevel)
		pbReq.MaxLevel = &v
	}
	var out *CharacterPage
	err := b.retry(ctx, func() error {
		resp, err := (*b.admin).SearchCharacters(ctx, pbReq)
		if err != nil {
			return err
		}
		out = &CharacterPage{Characters: charactersFromPB(resp.Characters), NextCursor: resp.NextCursor}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) createMigration(ctx context.Context, req CreateMigrationRequest) (*Migration, error) {
	ctx, cancel := b.callCtx(ctx, b.adminAPIKey)
	defer cancel()
	var out *Migration
	err := b.retry(ctx, func() error {
		resp, err := (*b.admin).CreateMigration(ctx, &pb.CreateMigrationRequest{
			SourceServers: req.SourceServers, TargetServer: req.TargetServer,
		})
		if err != nil {
			return err
		}
		out = migrationFromPB(resp.Migration)
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) getMigration(ctx context.Context, id string) (*Migration, error) {
	ctx, cancel := b.callCtx(ctx, b.adminAPIKey)
	defer cancel()
	var out *Migration
	err := b.retry(ctx, func() error {
		resp, err := (*b.admin).GetMigration(ctx, &pb.GetMigrationRequest{MigrationId: id})
		if err != nil {
			return err
		}
		out = migrationFromPB(resp.Migration)
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) listMigrations(ctx context.Context, limit int) ([]Migration, error) {
	ctx, cancel := b.callCtx(ctx, b.adminAPIKey)
	defer cancel()
	var out []Migration
	err := b.retry(ctx, func() error {
		resp, err := (*b.admin).ListMigrations(ctx, &pb.ListMigrationsRequest{Limit: int32(limit)})
		if err != nil {
			return err
		}
		out = make([]Migration, 0, len(resp.Migrations))
		for _, m := range resp.Migrations {
			out = append(out, *migrationFromPB(m))
		}
		return nil
	})
	return out, grpcError(err)
}

func (b *grpcBackend) rollbackMigration(ctx context.Context, id string) (*Migration, error) {
	ctx, cancel := b.callCtx(ctx, b.adminAPIKey)
	defer cancel()
	var out *Migration
	err := b.retry(ctx, func() error {
		resp, err := (*b.admin).RollbackMigration(ctx, &pb.RollbackMigrationRequest{MigrationId: id})
		if err != nil {
			return err
		}
		out = migrationFromPB(resp.Migration)
		return nil
	})
	return out, grpcError(err)
}

// ── pb → SDK converters ─────────────────────────────────────

func serverFromPB(s *pb.Server) Server {
	out := Server{
		ID: s.Id, Name: s.Name, Type: s.Type, Region: s.Region,
		RealmID: strPtr(s.RealmId), ShardID: strPtr(s.ShardId), Version: s.Version, Platform: s.Platform,
		Capacity: int(s.Capacity), Metadata: s.Metadata, Status: s.Status,
		Players: int(s.Players), Load: s.Load,
	}
	if s.Endpoint != nil {
		out.Endpoint = Endpoint{Host: s.Endpoint.Host, Port: int(s.Endpoint.Port)}
	}
	if s.LastSeenAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, s.LastSeenAt); err == nil {
			out.LastSeenAt = &t
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, s.CreatedAt); err == nil {
		out.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, s.UpdatedAt); err == nil {
		out.UpdatedAt = t
	}
	return out
}

func characterFromPB(c *pb.Character) *Character {
	out := &Character{
		AccountID: c.AccountId, ServerID: c.ServerId, CharacterID: c.CharacterId,
		Name: c.Name, Level: int(c.Level), ClassID: int(c.ClassId),
		Avatar: c.Avatar, Metadata: c.Metadata,
	}
	if c.LastLoginAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, c.LastLoginAt); err == nil {
			out.LastLoginAt = &t
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, c.CreatedAt); err == nil {
		out.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, c.UpdatedAt); err == nil {
		out.UpdatedAt = t
	}
	return out
}

func charactersFromPB(chars []*pb.Character) []Character {
	out := make([]Character, 0, len(chars))
	for _, c := range chars {
		out = append(out, *characterFromPB(c))
	}
	return out
}

// characterResultFromPB shapes a directory write reply; "queued" carries
// no character (asynchronous event adapter).
func characterResultFromPB(c *pb.Character, status string) *CharacterWriteResult {
	if status == "queued" || c == nil {
		return &CharacterWriteResult{Status: status}
	}
	return &CharacterWriteResult{Character: characterFromPB(c), Status: status}
}

func migrationFromPB(m *pb.Migration) *Migration {
	out := &Migration{
		ID: m.Id, SourceServers: m.SourceServers, TargetServer: m.TargetServer, Status: m.Status,
	}
	if t, err := time.Parse(time.RFC3339Nano, m.StartedAt); err == nil {
		out.StartedAt = t
	}
	if m.CompletedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, m.CompletedAt); err == nil {
			out.CompletedAt = &t
		}
	}
	return out
}

func int32Map(in map[string]int32) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = int(v)
	}
	return out
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
