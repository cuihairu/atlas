package grpc

import (
	"time"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/model"
)

// fmtTime renders a timestamp as RFC 3339, the JSON API's wire format.
func fmtTime(t time.Time) string { return t.Format(time.RFC3339Nano) }

func fmtTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// pbServer converts a model.Server to its wire form.
func pbServer(srv *model.Server) *pb.Server {
	out := &pb.Server{
		Id:        srv.ID,
		Name:      srv.Name,
		Type:      srv.Type,
		Region:    srv.Region,
		Version:   srv.Version,
		Platform:  srv.Platform,
		Endpoint:  &pb.Endpoint{Host: srv.Endpoint.Host, Port: int32(srv.Endpoint.Port)},
		Capacity:  int32(srv.Capacity),
		Metadata:  srv.Metadata,
		Status:    string(srv.Status),
		Players:   int32(srv.Players),
		Load:      srv.Load,
		CreatedAt: fmtTime(srv.CreatedAt),
		UpdatedAt: fmtTime(srv.UpdatedAt),
	}
	if srv.RealmID != nil {
		out.RealmId = *srv.RealmID
	}
	if srv.ShardID != nil {
		out.ShardId = *srv.ShardID
	}
	out.LastSeenAt = fmtTimePtr(srv.LastSeenAt)
	return out
}

// pbCharacter converts a model.Character to its wire form.
func pbCharacter(ch *model.Character) *pb.Character {
	out := &pb.Character{
		AccountId:   ch.AccountID,
		ServerId:    ch.ServerID,
		CharacterId: ch.CharacterID,
		Name:        ch.Name,
		Level:       int32(ch.Level),
		ClassId:     int32(ch.ClassID),
		Avatar:      ch.Avatar,
		Metadata:    ch.Metadata,
		CreatedAt:   fmtTime(ch.CreatedAt),
		UpdatedAt:   fmtTime(ch.UpdatedAt),
	}
	out.LastLoginAt = fmtTimePtr(ch.LastLoginAt)
	return out
}

// pbMigration converts a model.Migration to its wire form.
func pbMigration(m *model.Migration) *pb.Migration {
	return &pb.Migration{
		Id:            m.ID,
		SourceServers: m.SourceServers,
		TargetServer:  m.TargetServer,
		Status:        string(m.Status),
		StartedAt:     fmtTime(m.StartedAt),
		CompletedAt:   fmtTimePtr(m.CompletedAt),
	}
}

// pbMetadata lifts a proto map into the event patch shape; nil stays nil so
// an absent field means "no change" (PATCH semantics).
func pbMetadata(m map[string]string) *map[string]string {
	if m == nil {
		return nil
	}
	return &m
}
