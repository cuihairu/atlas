package grpc

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
)

// CreateCharacter mirrors POST /v1/directory/characters: publish a created
// event through the adapter, then read the projection back on synchronous
// transports ("queued" on asynchronous ones).
func (s *Server) CreateCharacter(ctx context.Context, req *pb.CreateCharacterRequest) (*pb.CreateCharacterResponse, error) {
	if req.AccountId <= 0 || req.ServerId == "" || req.CharacterId <= 0 {
		return nil, statusErr(errInvalid("account_id, server_id and character_id are required"))
	}

	// Registration gating (server tags), mirroring the REST handler:
	// 禁止注册 always rejects; 维护中 follows ATLAS_MAINTENANCE_ENFORCE.
	verdict, err := s.registry.CheckRegistration(ctx, req.ServerId)
	if err != nil {
		return nil, statusErr(err)
	}
	if verdict.Code != "" {
		return nil, status.Errorf(codes.PermissionDenied, "%s: %s", verdict.Code, verdict.Message)
	}

	level, classID := int(req.Level), int(req.ClassId)
	evt := &event.Event{
		Type:        event.EventCharacterCreated,
		AccountID:   req.AccountId,
		ServerID:    req.ServerId,
		CharacterID: req.CharacterId,
		Name:        req.Name,
		Level:       &level,
		ClassID:     &classID,
		Timestamp:   time.Now(),
	}
	if err := s.events.Publish(ctx, evt); err != nil {
		return nil, statusErr(err)
	}
	if !s.events.Synchronous() {
		return &pb.CreateCharacterResponse{Status: "queued"}, nil
	}

	ch, err := s.directory.GetCharacterByCharacterID(ctx, req.CharacterId)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.CreateCharacterResponse{Character: pbCharacter(ch), Status: "created"}, nil
}

// GetCharacter mirrors GET /v1/directory/characters/{character_id}.
func (s *Server) GetCharacter(ctx context.Context, req *pb.GetCharacterRequest) (*pb.GetCharacterResponse, error) {
	ch, err := s.directory.GetCharacterByCharacterID(ctx, req.CharacterId)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.GetCharacterResponse{Character: pbCharacter(ch)}, nil
}

// ListCharactersByAccount mirrors GET /v1/directory/accounts/{id}/characters.
func (s *Server) ListCharactersByAccount(ctx context.Context, req *pb.ListCharactersByAccountRequest) (*pb.ListCharactersByAccountResponse, error) {
	chars, err := s.directory.ListByAccount(ctx, req.AccountId)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.ListCharactersByAccountResponse{Characters: pbCharacters(chars)}, nil
}

// ListCharactersByServer mirrors GET /v1/directory/servers/{id}/characters.
func (s *Server) ListCharactersByServer(ctx context.Context, req *pb.ListCharactersByServerRequest) (*pb.ListCharactersByServerResponse, error) {
	chars, next, err := s.directory.ListByServer(ctx, req.ServerId, int(req.Limit), req.Cursor)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.ListCharactersByServerResponse{
		Characters: pbCharacters(chars),
		NextCursor: next,
	}, nil
}

// UpdateCharacter mirrors PATCH /v1/directory/characters/{character_id}.
func (s *Server) UpdateCharacter(ctx context.Context, req *pb.UpdateCharacterRequest) (*pb.UpdateCharacterResponse, error) {
	if req.CharacterId <= 0 {
		return nil, statusErr(errInvalid("character_id must be > 0"))
	}

	evt := &event.Event{
		Type:        event.EventCharacterUpdated,
		AccountID:   req.AccountId,
		ServerID:    req.ServerId,
		CharacterID: req.CharacterId,
	}
	if req.Name != nil {
		evt.Name = *req.Name
	}
	if req.Level != nil {
		v := int(*req.Level)
		evt.Level = &v
	}
	if req.ClassId != nil {
		v := int(*req.ClassId)
		evt.ClassID = &v
	}
	if req.Avatar != nil {
		evt.Avatar = req.Avatar
	}

	if err := s.events.Publish(ctx, evt); err != nil {
		return nil, statusErr(err)
	}
	if !s.events.Synchronous() {
		return &pb.UpdateCharacterResponse{Status: "queued"}, nil
	}

	ch, err := s.directory.GetCharacterByCharacterID(ctx, req.CharacterId)
	if err != nil {
		return nil, statusErr(err)
	}
	return &pb.UpdateCharacterResponse{Character: pbCharacter(ch), Status: "updated"}, nil
}

// DeleteCharacter mirrors DELETE /v1/directory/characters/{character_id}.
func (s *Server) DeleteCharacter(ctx context.Context, req *pb.DeleteCharacterRequest) (*pb.DeleteCharacterResponse, error) {
	// Resolve first so asynchronous adapters never enqueue a missing row.
	ch, err := s.directory.GetCharacterByCharacterID(ctx, req.CharacterId)
	if err != nil {
		return nil, statusErr(err)
	}

	evt := &event.Event{
		Type:        event.EventCharacterDeleted,
		AccountID:   ch.AccountID,
		ServerID:    ch.ServerID,
		CharacterID: req.CharacterId,
	}
	if err := s.events.Publish(ctx, evt); err != nil {
		return nil, statusErr(err)
	}
	if !s.events.Synchronous() {
		return &pb.DeleteCharacterResponse{Status: "queued"}, nil
	}
	return &pb.DeleteCharacterResponse{Status: "deleted"}, nil
}

// pbCharacters converts a slice of characters.
func pbCharacters(chars []*model.Character) []*pb.Character {
	out := make([]*pb.Character, 0, len(chars))
	for _, ch := range chars {
		out = append(out, pbCharacter(ch))
	}
	return out
}

// errInvalid builds an invalid-argument domain error.
func errInvalid(msg string) error {
	return fmt.Errorf("%w: %s", model.ErrInvalid, msg)
}
