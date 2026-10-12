package directory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
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

// TestApplyEventDeletedTwice pins delete idempotency: at-least-once
// transports re-deliver deletes, and a row that is already gone must be a
// successful no-op — an error here would feed the event back into the
// adapter retry loop (hot requeue on rabbitmq) with no way to ever succeed.
func TestApplyEventDeletedTwice(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	create := &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1",
		CharacterID: 9, Name: "c", Level: intPtr(1),
	}
	if _, err := svc.ApplyEvent(ctx, create); err != nil {
		t.Fatalf("create: %v", err)
	}
	del := &event.Event{Type: event.EventCharacterDeleted, CharacterID: 9}
	if _, err := svc.ApplyEvent(ctx, del); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	// Re-delivery with the key resolved from the event itself...
	if _, err := svc.ApplyEvent(ctx, del); err != nil {
		t.Fatalf("redelivered delete: %v", err)
	}
	// ...and a re-delivery that still has to resolve account/server.
	if _, err := svc.ApplyEvent(ctx, del); err != nil {
		t.Fatalf("redelivered delete (resolve): %v", err)
	}
	// A delete for a character that never existed is equally a no-op.
	never := &event.Event{Type: event.EventCharacterDeleted, CharacterID: 424242}
	if _, err := svc.ApplyEvent(ctx, never); err != nil {
		t.Fatalf("delete of unknown character: %v", err)
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

// failingCharacters injects errors at chosen store operations while
// delegating everything else to the memory store — the projection's
// non-NotFound error branches are unreachable through a healthy store.
type failingCharacters struct {
	store.CharacterStore
	failUpsert, failDelete, failGetByID, failUpdate error
}

func (f *failingCharacters) UpsertCharacter(ctx context.Context, ch *model.Character) error {
	if f.failUpsert != nil {
		return f.failUpsert
	}
	return f.CharacterStore.UpsertCharacter(ctx, ch)
}

func (f *failingCharacters) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	if f.failDelete != nil {
		return f.failDelete
	}
	return f.CharacterStore.DeleteCharacter(ctx, accountID, serverID, characterID)
}

func (f *failingCharacters) GetCharacterByCharacterID(ctx context.Context, characterID int64) (*model.Character, error) {
	if f.failGetByID != nil {
		return nil, f.failGetByID
	}
	return f.CharacterStore.GetCharacterByCharacterID(ctx, characterID)
}

func TestApplyEventMovedWithFullComposite(t *testing.T) {
	// A moved event carrying the whole composite key skips the global
	// character-ID lookup and resolves the source directly.
	svc, _ := newTestService()
	ctx := context.Background()
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 2, ServerID: "s1",
		CharacterID: 8, Name: "c8",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ch, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterMoved, AccountID: 2, ServerID: "s1",
		CharacterID: 8, TargetServerID: "s2",
	})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if ch.ServerID != "s2" || ch.Name != "c8" {
		t.Errorf("moved character lost data: %+v", ch)
	}
}

func TestApplyEventMovedUnknownCharacter(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterMoved, CharacterID: 424242, TargetServerID: "s2",
	})
	if err == nil {
		t.Fatal("moving an unknown character must fail")
	}
}

func TestApplyEventMovedStoreErrors(t *testing.T) {
	boom := errors.New("shard offline")
	t.Run("target upsert fails", func(t *testing.T) {
		f := &failingCharacters{CharacterStore: memory.New()}
		svc := New(f)
		if _, err := svc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1", CharacterID: 7,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		f.failUpsert = boom // arm after the seed, the create is a clean upsert
		if _, err := svc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterMoved, CharacterID: 7, TargetServerID: "s2",
		}); !errors.Is(err, boom) {
			t.Errorf("upsert error should propagate, got %v", err)
		}
	})
	t.Run("source delete fails", func(t *testing.T) {
		f := &failingCharacters{CharacterStore: memory.New(), failDelete: boom}
		svc := New(f)
		if _, err := svc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1", CharacterID: 7,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := svc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterMoved, CharacterID: 7, TargetServerID: "s2",
		}); !errors.Is(err, boom) {
			t.Errorf("delete error should propagate, got %v", err)
		}
	})
}

func TestApplyEventDeletedStoreError(t *testing.T) {
	boom := errors.New("disk gone")
	f := &failingCharacters{CharacterStore: memory.New(), failGetByID: boom}
	svc := New(f)
	_, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterDeleted, CharacterID: 5,
	})
	if !errors.Is(err, boom) {
		t.Errorf("resolution error should propagate, got %v", err)
	}
}

func TestApplyEventLoginResolvesByCharacterID(t *testing.T) {
	// A login event carrying only character_id resolves its composite key
	// from the index and stamps the supplied timestamp (not wall clock).
	svc, mem := newTestService()
	ctx := context.Background()
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 3, ServerID: "s1", CharacterID: 9,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, CharacterID: 9, Timestamp: at,
	}); err != nil {
		t.Fatalf("login: %v", err)
	}
	ch, err := mem.GetCharacter(ctx, 3, "s1", 9)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.LastLoginAt == nil || !ch.LastLoginAt.Equal(at) {
		t.Errorf("last_login_at = %v, want event timestamp %v", ch.LastLoginAt, at)
	}

	// Login of an unknown character propagates the resolution failure.
	if _, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterLogin, CharacterID: 424242,
	}); err == nil {
		t.Error("login of unknown character must fail")
	}
}

func TestApplyEventCreatedNilOptionals(t *testing.T) {
	// A create event without any optional fields exercises the nil-deref
	// helpers: level/class zero, empty avatar, nil metadata.
	svc, mem := newTestService()
	ctx := context.Background()
	ch, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 4, ServerID: "s1", CharacterID: 11,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ch.Level != 0 || ch.ClassID != 0 || ch.Avatar != "" || ch.Metadata != nil {
		t.Errorf("nil optionals must default: %+v", ch)
	}
	got, err := mem.GetCharacter(ctx, 4, "s1", 11)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Metadata != nil {
		t.Errorf("metadata should stay nil, got %v", got.Metadata)
	}
}

func (f *failingCharacters) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	if f.failUpdate != nil {
		return f.failUpdate
	}
	return f.CharacterStore.UpdateCharacter(ctx, accountID, serverID, characterID, patch)
}

func TestApplyEventNilEvent(t *testing.T) {
	svc, _ := newTestService()
	if _, err := svc.ApplyEvent(context.Background(), nil); err == nil {
		t.Fatal("nil event must be rejected")
	}
}

func TestApplyEventInvalidCharacterID(t *testing.T) {
	// character_id <= 0 is rejected up front by every projection that
	// cannot resolve a row without it.
	svc, _ := newTestService()
	for typ, want := range map[event.EventType]string{
		event.EventCharacterUpdated: "update",
		event.EventCharacterDeleted: "delete",
		event.EventCharacterLogin:   "login",
	} {
		_, err := svc.ApplyEvent(context.Background(), &event.Event{Type: typ, CharacterID: 0})
		if err == nil || (want != "" && !errors.Is(err, model.ErrInvalid)) {
			t.Errorf("%s with character_id=0: err = %v, want invalid", typ, err)
		}
	}
}

func TestApplyEventCreatedUpsertError(t *testing.T) {
	boom := errors.New("shard offline")
	f := &failingCharacters{CharacterStore: memory.New(), failUpsert: boom}
	svc := New(f)
	_, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1", CharacterID: 7,
	})
	if !errors.Is(err, boom) {
		t.Errorf("create upsert error should propagate, got %v", err)
	}
}

func TestApplyEventUpdatedStoreError(t *testing.T) {
	boom := errors.New("shard offline")
	f := &failingCharacters{CharacterStore: memory.New(), failUpdate: boom}
	svc := New(f)
	if _, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1", CharacterID: 7,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterUpdated, AccountID: 1, ServerID: "s1", CharacterID: 7, Level: ptrInt(9),
	})
	if !errors.Is(err, boom) {
		t.Errorf("update error should propagate, got %v", err)
	}
}

func TestApplyEventDeletedNoOps(t *testing.T) {
	t.Run("unknown character resolves to missing row", func(t *testing.T) {
		f := &failingCharacters{CharacterStore: memory.New(), failGetByID: store.ErrNotFound}
		svc := New(f)
		if err := applyDelete(svc, &event.Event{Type: event.EventCharacterDeleted, CharacterID: 5}); err != nil {
			t.Errorf("delete of unknown character must be a no-op, got %v", err)
		}
	})
	t.Run("row already gone at delete time", func(t *testing.T) {
		f := &failingCharacters{CharacterStore: memory.New(), failDelete: store.ErrNotFound}
		svc := New(f)
		if _, err := svc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1", CharacterID: 6,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := applyDelete(svc, &event.Event{Type: event.EventCharacterDeleted, CharacterID: 6}); err != nil {
			t.Errorf("re-delivered delete must be a no-op, got %v", err)
		}
	})
	t.Run("real store error still propagates", func(t *testing.T) {
		boom := errors.New("disk gone")
		f := &failingCharacters{CharacterStore: memory.New(), failDelete: boom}
		svc := New(f)
		if _, err := svc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1", CharacterID: 6,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Composite key present: no character-ID resolution between the
		// failure and the caller.
		if _, err := svc.ApplyEvent(context.Background(), &event.Event{
			Type: event.EventCharacterDeleted, AccountID: 1, ServerID: "s1", CharacterID: 6,
		}); !errors.Is(err, boom) {
			t.Errorf("real delete error should propagate, got %v", err)
		}
	})
}

func applyDelete(svc *Service, e *event.Event) error {
	_, err := svc.ApplyEvent(context.Background(), e)
	return err
}

func ptrInt(v int) *int { return &v }

func ptrString(v string) *string { return &v }

func TestApplyEventCreatedFullOptionals(t *testing.T) {
	// A create carrying every optional exercises the populated side of the
	// deref helpers, so both nil and non-nil branches stay pinned.
	svc, mem := newTestService()
	ctx := context.Background()
	meta := map[string]string{"guild": "storm"}
	ch, err := svc.ApplyEvent(ctx, &event.Event{
		Type: event.EventCharacterCreated, AccountID: 5, ServerID: "s1", CharacterID: 12,
		Level: ptrInt(42), ClassID: ptrInt(3), Avatar: ptrString("a.png"), Metadata: &meta,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ch.Level != 42 || ch.ClassID != 3 || ch.Avatar != "a.png" || ch.Metadata["guild"] != "storm" {
		t.Errorf("optionals lost: %+v", ch)
	}
	got, err := mem.GetCharacter(ctx, 5, "s1", 12)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Level != 42 || got.Metadata["guild"] != "storm" {
		t.Errorf("stored row lost optionals: %+v", got)
	}
}

func TestApplyEventCreatedEventTimestampWins(t *testing.T) {
	// A created event carrying a timestamp pins last_login_at to it instead
	// of wall clock (projection is idempotent for re-delivery).
	svc, mem := newTestService()
	at := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	ch, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterCreated, AccountID: 6, ServerID: "s1", CharacterID: 13, Timestamp: at,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ch.LastLoginAt == nil || !ch.LastLoginAt.Equal(at) {
		t.Errorf("last_login_at = %v, want event timestamp %v", ch.LastLoginAt, at)
	}
	got, err := mem.GetCharacter(context.Background(), 6, "s1", 13)
	if err != nil || got.LastLoginAt == nil || !got.LastLoginAt.Equal(at) {
		t.Errorf("stored: %v, %v", got.LastLoginAt, err)
	}
}

func TestApplyEventLoginStoreError(t *testing.T) {
	boom := errors.New("shard offline")
	f := &failingCharacters{CharacterStore: memory.New(), failUpdate: boom}
	svc := New(f)
	if _, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1", CharacterID: 7,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterLogin, AccountID: 1, ServerID: "s1", CharacterID: 7,
	}); !errors.Is(err, boom) {
		t.Errorf("login update error should propagate, got %v", err)
	}
}

func TestApplyEventMovedInvalidCharacterID(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.ApplyEvent(context.Background(), &event.Event{
		Type: event.EventCharacterMoved, CharacterID: 0, TargetServerID: "s2",
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Errorf("moved without character_id should be invalid, got %v", err)
	}
}
