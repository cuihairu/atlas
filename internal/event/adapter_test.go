package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventJSONRoundTrip(t *testing.T) {
	level := 42
	e := Event{
		Type:        EventCharacterCreated,
		AccountID:   10001,
		ServerID:    "game-1001",
		CharacterID: 823712,
		Name:        "剑无尘",
		Level:       &level,
		Timestamp:   time.Unix(1700000000, 0).UTC(),
	}

	data, err := json.Marshal(&e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(data); !strings.Contains(got, `"event":"character.created"`) {
		t.Errorf("expected wire field `event`, got %s", got)
	}

	var back Event
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Type != e.Type || back.AccountID != e.AccountID ||
		back.ServerID != e.ServerID || back.CharacterID != e.CharacterID ||
		back.Name != e.Name || back.Level == nil || *back.Level != level {
		t.Errorf("round trip mismatch: %+v vs %+v", back, e)
	}
}

func TestEventTypeConstants(t *testing.T) {
	// Wire values are a public contract (docs/sync.md §2).
	cases := map[EventType]string{
		EventCharacterCreated: "character.created",
		EventCharacterUpdated: "character.updated",
		EventCharacterDeleted: "character.deleted",
		EventCharacterMoved:   "character.moved",
		EventCharacterLogin:   "character.login",
	}
	for evt, want := range cases {
		if string(evt) != want {
			t.Errorf("expected %q, got %q", want, string(evt))
		}
	}
}
