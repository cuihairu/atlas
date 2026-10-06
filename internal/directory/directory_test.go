package directory

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func newTestService() (*Service, *memory.Store) {
	mem := memory.New()
	return New(mem), mem
}

func TestCreateCharacter(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	ch, err := svc.CreateCharacter(ctx, 10001, "game-1001", 823712, "TestChar", 50, 3)
	if err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	if ch.AccountID != 10001 {
		t.Errorf("expected account_id 10001, got %d", ch.AccountID)
	}
	if ch.Name != "TestChar" {
		t.Errorf("expected name 'TestChar', got %q", ch.Name)
	}
	if ch.Level != 50 {
		t.Errorf("expected level 50, got %d", ch.Level)
	}
}

func TestCreateCharacterInvalid(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	_, err := svc.CreateCharacter(ctx, 0, "game-1001", 1, "name", 1, 1)
	if !errors.Is(err, model.ErrInvalid) {
		t.Errorf("expected ErrInvalid for zero account_id, got: %v", err)
	}

	_, err = svc.CreateCharacter(ctx, 1, "", 1, "name", 1, 1)
	if !errors.Is(err, model.ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty server_id, got: %v", err)
	}
}

func TestUpdateCharacter(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.CreateCharacter(ctx, 10001, "game-1001", 823712, "TestChar", 50, 3); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}

	newLevel := 51
	newName := "Renamed"
	patch := store.CharacterPatch{
		Level: &newLevel,
		Name:  &newName,
	}

	ch, err := svc.UpdateCharacter(ctx, 10001, "game-1001", 823712, patch)
	if err != nil {
		t.Fatalf("UpdateCharacter: %v", err)
	}
	if ch.Level != 51 {
		t.Errorf("expected level 51, got %d", ch.Level)
	}
	if ch.Name != "Renamed" {
		t.Errorf("expected name 'Renamed', got %q", ch.Name)
	}
}

func TestDeleteCharacter(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.CreateCharacter(ctx, 10001, "game-1001", 823712, "TestChar", 50, 3); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}

	if err := svc.DeleteCharacter(ctx, 10001, "game-1001", 823712); err != nil {
		t.Fatalf("DeleteCharacter: %v", err)
	}

	_, err := svc.GetCharacter(ctx, 10001, "game-1001", 823712)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}
}

func TestGetCharacterByCharacterID(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.CreateCharacter(ctx, 10001, "game-1001", 823712, "TestChar", 50, 3); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}

	ch, err := svc.GetCharacterByCharacterID(ctx, 823712)
	if err != nil {
		t.Fatalf("GetCharacterByCharacterID: %v", err)
	}
	if ch.Name != "TestChar" {
		t.Errorf("expected name 'TestChar', got %q", ch.Name)
	}
}

func TestListByAccount(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// Create characters on two different servers for the same account.
	if _, err := svc.CreateCharacter(ctx, 10001, "game-1001", 823712, "Char1", 50, 3); err != nil {
		t.Fatalf("CreateCharacter 1: %v", err)
	}
	if _, err := svc.CreateCharacter(ctx, 10001, "game-1002", 923812, "Char2", 97, 7); err != nil {
		t.Fatalf("CreateCharacter 2: %v", err)
	}
	// Different account.
	if _, err := svc.CreateCharacter(ctx, 10002, "game-1001", 723812, "Char3", 10, 1); err != nil {
		t.Fatalf("CreateCharacter 3: %v", err)
	}

	chars, err := svc.ListByAccount(ctx, 10001)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if len(chars) != 2 {
		t.Fatalf("expected 2 characters, got %d", len(chars))
	}
}

func TestListByServer(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	for i := int64(1); i <= 5; i++ {
		if _, err := svc.CreateCharacter(ctx, 10000+i, "game-1001", 800000+i, "Char", int(i*10), 1); err != nil {
			t.Fatalf("CreateCharacter %d: %v", i, err)
		}
	}

	// First page.
	chars, cursor, err := svc.ListByServer(ctx, "game-1001", 2, "")
	if err != nil {
		t.Fatalf("ListByServer page 1: %v", err)
	}
	if len(chars) != 2 {
		t.Fatalf("expected 2 characters on page 1, got %d", len(chars))
	}
	if cursor == "" {
		t.Fatal("expected non-empty cursor")
	}

	// Second page.
	chars2, cursor2, err := svc.ListByServer(ctx, "game-1001", 2, cursor)
	if err != nil {
		t.Fatalf("ListByServer page 2: %v", err)
	}
	if len(chars2) != 2 {
		t.Fatalf("expected 2 characters on page 2, got %d", len(chars2))
	}
	_ = cursor2

	// Third page (1 remaining).
	chars3, _, err := svc.ListByServer(ctx, "game-1001", 2, cursor2)
	if err != nil {
		t.Fatalf("ListByServer page 3: %v", err)
	}
	if len(chars3) != 1 {
		t.Fatalf("expected 1 character on page 3, got %d", len(chars3))
	}
}
