// Error-path coverage for the directory service: the character-id guard,
// the three write wraps (create/update/delete), and the event-ordering
// pre-reads — a store failure other than not-found must surface, never be
// mistaken for "no newer projection".
package directory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// faultChars fails the configured verbs; everything else delegates to the
// backing memory store.
type faultChars struct {
	store.CharacterStore
	upsertErr error
	updateErr error
	deleteErr error
	getErr    error
}

func (f *faultChars) UpsertCharacter(ctx context.Context, ch *model.Character) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	return f.CharacterStore.UpsertCharacter(ctx, ch)
}

func (f *faultChars) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	return f.CharacterStore.UpdateCharacter(ctx, accountID, serverID, characterID, patch)
}

func (f *faultChars) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.CharacterStore.DeleteCharacter(ctx, accountID, serverID, characterID)
}

func (f *faultChars) GetCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.CharacterStore.GetCharacter(ctx, accountID, serverID, characterID)
}

func TestDirectoryWriteFaults(t *testing.T) {
	ctx := context.Background()
	fc := &faultChars{CharacterStore: memory.New()}
	svc := New(fc)

	// Guard: character_id must be positive.
	if _, err := svc.CreateCharacter(ctx, 1, "s1", 0, "name", 1, 1); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("create with zero character id = %v, want ErrInvalid", err)
	}

	fc.upsertErr = errFault
	if _, err := svc.CreateCharacter(ctx, 1, "s1", 1, "name", 1, 1); !errors.Is(err, errFault) {
		t.Fatalf("create wrap = %v, want errFault", err)
	}
	fc.upsertErr = nil

	fc.updateErr = errFault
	if _, err := svc.UpdateCharacter(ctx, 1, "s1", 1, store.CharacterPatch{}); !errors.Is(err, errFault) {
		t.Fatalf("update wrap = %v, want errFault", err)
	}
	fc.updateErr = nil

	fc.deleteErr = errFault
	if err := svc.DeleteCharacter(ctx, 1, "s1", 1); !errors.Is(err, errFault) {
		t.Fatalf("delete wrap = %v, want errFault", err)
	}
}

// TestApplyEventOrderingReadFailure pins the replay guard: when the
// ordering pre-read fails with anything but not-found, the event apply
// aborts with the wrapped error instead of blindly upserting over a
// possibly newer projection.
func TestApplyEventOrderingReadFailure(t *testing.T) {
	ctx := context.Background()
	fc := &faultChars{CharacterStore: memory.New(), getErr: errFault}
	svc := New(fc)
	ts := time.Unix(1700000000, 0)

	created := &event.Event{
		Type: event.EventCharacterCreated, AccountID: 1, ServerID: "s1",
		CharacterID: 1, Timestamp: ts,
	}
	if _, err := svc.ApplyEvent(ctx, created); err == nil || !strings.Contains(err.Error(), "check created event ordering") {
		t.Fatalf("created ordering read failure = %v, want the ordering wrap", err)
	}

	login := &event.Event{
		Type: event.EventCharacterLogin, AccountID: 1, ServerID: "s1",
		CharacterID: 1, Timestamp: ts,
	}
	if _, err := svc.ApplyEvent(ctx, login); err == nil || !strings.Contains(err.Error(), "check login event ordering") {
		t.Fatalf("login ordering read failure = %v, want the ordering wrap", err)
	}
}
