package directory

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
)

func TestApplyEventCreated(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	ch, err := svc.ApplyEvent(ctx, &event.Event{
		Type:        event.EventCharacterCreated,
		AccountID:   10001,
		ServerID:    "game-1001",
		CharacterID: 823712,
		Name:        "剑无尘",
		Level:       intPtr(1),
		ClassID:     intPtr(3),
	})
	if err != nil {
		t.Fatalf("ApplyEvent: %v", err)
	}
	if ch.ServerID != "game-1001" || ch.Name != "剑无尘" || ch.Level != 1 || ch.ClassID != 3 {
		t.Errorf("unexpected character: %+v", ch)
	}
	if ch.LastLoginAt == nil {
		t.Errorf("expected last_login_at set on created")
	}
}

func TestApplyEventCreatedIdempotent(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	evt := &event.Event{
		Type:        event.EventCharacterCreated,
		AccountID:   10001,
		ServerID:    "game-1001",
		CharacterID: 823712,
		Name:        "剑无尘",
		Level:       intPtr(1),
	}
	if _, err := svc.ApplyEvent(ctx, evt); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	evt.Level = intPtr(5)
	if _, err := svc.ApplyEvent(ctx, evt); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	ch, err := svc.GetCharacter(ctx, 10001, "game-1001", 823712)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.Level != 5 {
		t.Errorf("expected redelivery to upsert level=5, got %d", ch.Level)
	}
}

func TestApplyEventUpdatedResolvesByKey(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 10001, ServerID: "game-1001",
		CharacterID: 823712, Name: "剑无尘", Level: intPtr(1),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Event carries only character_id: the composite key must be resolved.
	ch, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterUpdated, CharacterID: 823712, Name: "改名",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if ch.Name != "改名" {
		t.Errorf("expected rename, got %q", ch.Name)
	}
}

func TestApplyEventUpdatedNotFound(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterUpdated, CharacterID: 999,
	})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestApplyEventDeletedAndLogin(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	create := &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1",
		CharacterID: 7, Name: "c", Level: intPtr(1),
	}
	if _, err := svc.ApplyEvent(ctx, create); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.ApplyEvent(ctx, &event.Event{Type: event.EventCharacterLogin, CharacterID: 7}); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := svc.ApplyEvent(ctx, &event.Event{Type: event.EventCharacterDeleted, CharacterID: 7}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetCharacterByCharacterID(ctx, 7); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected deleted, got %v", err)
	}

	_, err := svc.ApplyEvent(ctx, &event.Event{Type: event.EventCharacterLogin, CharacterID: 7})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("login after delete: expected ErrNotFound, got %v", err)
	}
}

func TestApplyEventMoved(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1",
		CharacterID: 7, Name: "c", Level: intPtr(9),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ch, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterMoved, CharacterID: 7, TargetServerID: "s2",
	})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if ch.ServerID != "s2" || ch.Level != 9 || ch.Name != "c" {
		t.Errorf("moved character lost data: %+v", ch)
	}
	if _, err := svc.GetCharacter(ctx, 1, "s1", 7); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected source row deleted, got %v", err)
	}
}

func TestApplyEventMovedRequiresTarget(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterMoved, CharacterID: 7,
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestApplyEventUnknownType(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.ApplyEvent(context.Background(), &event.Event{Type: "nope"})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestApplyEventCreatedValidation(t *testing.T) {
	svc, _ := newTestService()
	cases := []*event.Event{
		{Type: event.EventCharacterCreated, ServerID: "s1", CharacterID: 1},
		{Type: event.EventCharacterCreated, AccountID: 1, CharacterID: 1},
		{Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1"},
	}
	for i, evt := range cases {
		if _, err := svc.ApplyEvent(context.Background(), evt); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("case %d: expected ErrInvalid, got %v", i, err)
		}
	}
}

func intPtr(i int) *int { return &i }
