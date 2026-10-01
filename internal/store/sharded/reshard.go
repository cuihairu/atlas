// Resharding support (TODO v0.1.16): copy the whole character index from one
// store layout into another — single store → sharded, sharded → sharded with
// a different shard count, or sharded → single — using only the public
// CharacterStore surface, so source and destination can be any mix of
// memory / postgres / sharded stores.
package sharded

import (
	"context"
	"fmt"

	"github.com/cuihairu/atlas/internal/store"
)

// Reshard drains src into dst via cursor-paginated SearchCharacters and
// upserts every character into dst, where dst may be a single store or a
// sharded one (or anything else implementing CharacterStore). Upserts make
// the copy idempotent: re-running after an interrupted pass repairs the
// remainder instead of duplicating.
//
// batchSize is the page size for source reads (<= 0 means 500). Reshard does
// not delete src and does not cutover traffic — operators stop writes (or
// accept a final delta pass), run Reshard, verify the returned count, then
// repoint the directory service.
func Reshard(ctx context.Context, src, dst store.CharacterStore, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 500
	}

	cursor := ""
	copied := 0
	for {
		page, next, err := src.SearchCharacters(ctx, store.CharacterSearchFilter{Limit: batchSize, Cursor: cursor})
		if err != nil {
			return copied, fmt.Errorf("reshard read (cursor %q): %w", cursor, err)
		}

		for _, ch := range page {
			if err := dst.UpsertCharacter(ctx, ch); err != nil {
				return copied, fmt.Errorf("reshard write character %d: %w", ch.CharacterID, err)
			}
		}
		copied += len(page)

		// Empty page or a cursor that stops advancing means the source is
		// drained (the latter guards against stores that always echo a
		// non-empty cursor).
		if len(page) == 0 || next == "" || next == cursor {
			return copied, nil
		}
		cursor = next
	}
}
