package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Regression: SearchCharacters must skip the cursor with the SAME ordering it
// sorts by (server ID lexicographically, character ID numerically). The
// historical bug compared the composite cursor key as one string, so a page
// boundary crossing a digit-length change ("9" → "10") dropped every row
// whose ID string sorted below the cursor.
func TestSearchCharacters_CursorSurvivesDigitLengthChange(t *testing.T) {
	mem := New()
	ctx := context.Background()

	// 25 characters on one server: IDs 1..25 cross the 9→10 digit boundary
	// several times at small page sizes.
	for i := int64(1); i <= 25; i++ {
		ch := &model.Character{
			AccountID:   i,
			ServerID:    "srv-1",
			CharacterID: i,
			Name:        fmt.Sprintf("c%02d", i),
			Level:       int(i),
			ClassID:     1,
		}
		if err := mem.UpsertCharacter(ctx, ch); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	seen := map[int64]bool{}
	cursor := ""
	pages := 0
	for {
		page, next, err := mem.SearchCharacters(ctx, store.CharacterSearchFilter{Limit: 4, Cursor: cursor})
		if err != nil {
			t.Fatalf("search (cursor %q): %v", cursor, err)
		}
		if len(page) == 0 {
			break
		}
		pages++
		for _, ch := range page {
			if seen[ch.CharacterID] {
				t.Fatalf("character %d returned twice", ch.CharacterID)
			}
			seen[ch.CharacterID] = true
		}
		if next == "" {
			break
		}
		cursor = next
		if pages > 20 {
			t.Fatal("cursor never drained after 20 pages")
		}
	}

	if pages != 7 {
		t.Errorf("expected 7 non-empty pages of 4 over 25 rows, got %d", pages)
	}
	if len(seen) != 25 {
		t.Errorf("expected all 25 characters across pages, got %d", len(seen))
	}
	for i := int64(1); i <= 25; i++ {
		if !seen[i] {
			t.Errorf("character %d missing from pagination", i)
		}
	}
}

// Same regression for ListCharactersByServer, whose cursor is a bare
// character ID (numeric ordering by construction).
func TestListCharactersByServer_CursorPagination(t *testing.T) {
	mem := New()
	ctx := context.Background()

	for i := int64(1); i <= 12; i++ {
		ch := &model.Character{AccountID: i, ServerID: "srv-1", CharacterID: i, Name: fmt.Sprintf("c%d", i), ClassID: 1}
		if err := mem.UpsertCharacter(ctx, ch); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	seen := map[int64]bool{}
	cursor := ""
	for {
		page, err := mem.ListCharactersByServer(ctx, "srv-1", 5, cursor)
		if err != nil {
			t.Fatalf("list (cursor %q): %v", cursor, err)
		}
		if len(page) == 0 {
			break
		}
		for _, ch := range page {
			if seen[ch.CharacterID] {
				t.Fatalf("character %d returned twice", ch.CharacterID)
			}
			seen[ch.CharacterID] = true
		}
		cursor = fmt.Sprint(page[len(page)-1].CharacterID)
		if len(seen) > 12 {
			t.Fatal("pagination overshoot")
		}
	}

	if len(seen) != 12 {
		t.Errorf("expected 12 characters, got %d", len(seen))
	}
}
