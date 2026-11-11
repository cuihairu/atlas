// Error-path coverage for the postgres store: every verb's query-failure
// wrap is driven with a canceled context (the operational reality of a
// request-scoped deadline expiring mid-query), plus the not-found mappings,
// the empty-patch no-op, the clamp edges, and the runtime-merge failure path.
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// TestPingAndClose pins the lifecycle verbs against the real pool.
func TestPingAndClose(t *testing.T) {
	s, _ := openTestStore(t)

	if err := s.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Ping(ctx); err == nil {
		t.Error("ping with canceled context: expected error")
	}
	if err := s.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	if err := s.Ping(context.Background()); err == nil {
		t.Error("ping after close: expected error")
	}
}

// TestCanceledContextSweep drives every store verb with an already-canceled
// context: the wrapped "query failed" branches are the 5xx surface callers
// see when a deadline expires mid-operation.
func TestCanceledContextSweep(t *testing.T) {
	s, _ := openTestStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	now := time.Now()
	srv := &model.Server{ID: "sweep-srv", Name: "Sweep", Type: "game",
		Region: "cn", Version: "1.0.0", Platform: "android", Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 10}
	calls := map[string]func() error{
		"register server":      func() error { return s.RegisterServer(ctx, srv) },
		"get server":           func() error { _, err := s.GetServer(ctx, "sweep-srv"); return err },
		"list servers":         func() error { _, err := s.ListServers(ctx, store.ServerFilter{}); return err },
		"update server status": func() error { return s.UpdateServerStatus(ctx, "sweep-srv", model.StatusOffline) },
		"cas server status": func() error {
			_, err := s.UpdateServerStatusIf(ctx, "sweep-srv", model.StatusOnline, model.StatusOffline)
			return err
		},
		"update server tags": func() error {
			return s.UpdateServerTags(ctx, "sweep-srv", []model.ServerTag{{Code: "beta", Label: "Beta", Tier: model.TierNew}})
		},
		"update server weight": func() error { return s.UpdateServerWeight(ctx, "sweep-srv", 5) },
		"delete server":        func() error { return s.DeleteServer(ctx, "sweep-srv") },
		"upsert character": func() error {
			return s.UpsertCharacter(ctx, &model.Character{AccountID: 1, ServerID: "sweep-srv", CharacterID: 1, Name: "H", Level: 1, ClassID: 1, CreatedAt: now, UpdatedAt: now})
		},
		"get character":       func() error { _, err := s.GetCharacter(ctx, 1, "sweep-srv", 1); return err },
		"get character by id": func() error { _, err := s.GetCharacterByCharacterID(ctx, 1); return err },
		"update character": func() error {
			return s.UpdateCharacter(ctx, 1, "sweep-srv", 1, store.CharacterPatch{Name: strptr("x")})
		},
		"delete character": func() error { return s.DeleteCharacter(ctx, 1, "sweep-srv", 1) },
		"list by account":  func() error { _, err := s.ListCharactersByAccount(ctx, 1); return err },
		"list by server":   func() error { _, err := s.ListCharactersByServer(ctx, "sweep-srv", 10, ""); return err },
		"search characters": func() error {
			_, _, err := s.SearchCharacters(ctx, store.CharacterSearchFilter{AccountID: 1})
			return err
		},
		"create migration": func() error {
			return s.CreateMigration(ctx, &model.Migration{ID: "m-1", SourceServers: []string{"a"}, TargetServer: "b", Status: model.MigrationPending, StartedAt: now})
		},
		"get migration":    func() error { _, err := s.GetMigration(ctx, "m-1"); return err },
		"update migration": func() error { return s.UpdateMigrationStatus(ctx, "m-1", model.MigrationCompleted, &now) },
		"list migrations":  func() error { _, err := s.ListMigrations(ctx, 10); return err },
		"get stats":        func() error { _, err := s.GetStats(ctx); return err },
		"create realm": func() error {
			return s.CreateRealm(ctx, &model.Realm{ID: "r-1", Name: "R", Region: "cn", Status: "active", CreatedAt: now})
		},
		"get realm":   func() error { _, err := s.GetRealm(ctx, "r-1"); return err },
		"list realms": func() error { _, err := s.ListRealms(ctx, 10); return err },
		"create shard": func() error {
			return s.CreateShard(ctx, &model.Shard{ID: "sh-1", RealmID: "r-1", Name: "S", Status: "active", CreatedAt: now})
		},
		"get shard":   func() error { _, err := s.GetShard(ctx, "sh-1"); return err },
		"list shards": func() error { _, err := s.ListShards(ctx, "r-1", 10); return err },
		"create window": func() error {
			return s.CreateMaintenanceWindow(ctx, &model.MaintenanceWindow{ID: "mw-1", ServerID: "sweep-srv", StartAt: now, EndAt: now.Add(time.Hour), CreatedAt: now})
		},
		"get window":          func() error { _, err := s.GetMaintenanceWindow(ctx, "mw-1"); return err },
		"delete window":       func() error { return s.DeleteMaintenanceWindow(ctx, "mw-1") },
		"list windows":        func() error { _, err := s.ListMaintenanceWindows(ctx, "sweep-srv", 10); return err },
		"mark window applied": func() error { return s.MarkMaintenanceWindowApplied(ctx, "mw-1", model.StatusMaintenance) },
		"create announcement": func() error {
			return s.CreateAnnouncement(ctx, &model.Announcement{ID: "a-1", Title: "T", Body: "B", Level: model.AnnouncementInfo, StartsAt: now, EndsAt: now.Add(time.Hour), CreatedAt: now})
		},
		"get announcement":    func() error { _, err := s.GetAnnouncement(ctx, "a-1"); return err },
		"delete announcement": func() error { return s.DeleteAnnouncement(ctx, "a-1") },
		"list announcements": func() error {
			_, err := s.ListAnnouncements(ctx, store.AnnouncementFilter{ServerID: "sweep-srv"})
			return err
		},
		"save crossserver cfg": func() error {
			_, err := s.SaveCrossServerConfig(ctx, &model.CrossServerConfig{Version: 1, Hash: "h"})
			return err
		},
		"get crossserver cfg": func() error { _, err := s.GetCrossServerConfig(ctx); return err },
	}

	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s with canceled context: expected error, got nil", name)
		}
	}
}

func strptr(v string) *string { return &v }
func intptr(v int) *int       { return &v }

// TestPatchEveryField pins the full dynamic UPDATE and its zero-row mapping:
// every CharacterPatch arm must land in its column, an empty patch is a
// no-op, and patching a character that does not exist is ErrNotFound.
func TestPatchEveryField(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mustOK := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	mustOK(s.RegisterServer(ctx, &model.Server{ID: "patch-srv", Name: "P", Type: "game",
		Region: "cn", Version: "1.0.0", Platform: "android", Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10}), "register")
	mustOK(s.UpsertCharacter(ctx, &model.Character{AccountID: 7, ServerID: "patch-srv", CharacterID: 8,
		Name: "Old", Level: 1, ClassID: 2, Avatar: "a.png", Metadata: map[string]string{"old": "1"},
		CreatedAt: now, UpdatedAt: now}), "upsert")

	login := now.Add(time.Hour)
	meta := map[string]string{"new": "2"}
	patch := store.CharacterPatch{
		Name:        strptr("New"),
		Level:       intptr(50),
		ClassID:     intptr(3),
		Avatar:      strptr("b.png"),
		Metadata:    &meta,
		LastLoginAt: &login,
	}
	mustOK(s.UpdateCharacter(ctx, 7, "patch-srv", 8, patch), "patch")

	ch, err := s.GetCharacter(ctx, 7, "patch-srv", 8)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ch.Name != "New" || ch.Level != 50 || ch.ClassID != 3 || ch.Avatar != "b.png" {
		t.Errorf("patched scalar fields wrong: %+v", ch)
	}
	if ch.Metadata["new"] != "2" || len(ch.Metadata) != 1 {
		t.Errorf("patched metadata wrong: %+v", ch.Metadata)
	}
	if !ch.LastLoginAt.Equal(login) {
		t.Errorf("patched last_login_at wrong: %v want %v", ch.LastLoginAt, login)
	}

	// Empty patch: no-op, no error, no query.
	mustOK(s.UpdateCharacter(ctx, 7, "patch-srv", 8, store.CharacterPatch{}), "empty patch")

	// Missing character: zero rows map to ErrNotFound.
	if err := s.UpdateCharacter(ctx, 99, "patch-srv", 8, patch); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("patch missing character: expected ErrNotFound, got %v", err)
	}
}

// TestListLimitClamp pins the listing clamps: limits <= 0 and > 200 are
// normalized by the store and stay error-free with real rows behind them.
func TestListLimitClamp(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := s.RegisterServer(ctx, &model.Server{ID: "clamp-srv", Name: "C", Type: "game",
		Region: "cn", Version: "1.0.0", Platform: "android", Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10}); err != nil {
		t.Fatalf("register: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.UpsertCharacter(ctx, &model.Character{AccountID: 9, ServerID: "clamp-srv",
			CharacterID: int64(100 + i), Name: "C", Level: 1, ClassID: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	for _, limit := range []int{0, -1, 300} {
		chars, err := s.ListCharactersByServer(ctx, "clamp-srv", limit, "")
		if err != nil {
			t.Fatalf("list by server limit=%d: %v", limit, err)
		}
		if len(chars) != 3 {
			t.Errorf("list by server limit=%d: expected 3 rows, got %d", limit, len(chars))
		}
	}
	for _, limit := range []int{0, 300} {
		if _, err := s.ListMigrations(ctx, limit); err != nil {
			t.Errorf("list migrations limit=%d: %v", limit, err)
		}
		if _, err := s.ListRealms(ctx, limit); err != nil {
			t.Errorf("list realms limit=%d: %v", limit, err)
		}
		if _, err := s.ListShards(ctx, "", limit); err != nil {
			t.Errorf("list shards limit=%d: %v", limit, err)
		}
		if _, err := s.ListMaintenanceWindows(ctx, "", limit); err != nil {
			t.Errorf("list windows limit=%d: %v", limit, err)
		}
		if _, err := s.ListAnnouncements(ctx, store.AnnouncementFilter{Limit: limit}); err != nil {
			t.Errorf("list announcements limit=%d: %v", limit, err)
		}
	}
}

// TestMissingRowsPinNotFound pins the not-found mapping of the single-row
// reads: a query that succeeds with zero rows must surface as store.ErrNotFound.
func TestMissingRowsPinNotFound(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()

	if _, err := s.GetServer(ctx, "no-such"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get server missing: expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetShard(ctx, "no-such-shard"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get shard missing: expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetRealm(ctx, "no-such-realm"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get realm missing: expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetCrossServerConfig(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get crossserver config missing: expected ErrNotFound, got %v", err)
	}
}

// TestCursorHelpers pins the cursor decode fallback of the search path and
// the pure helpers: an opaque cursor degrades to a zero page (no error), and
// the empty-input edges stay stable.
func TestCursorHelpers(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := s.RegisterServer(ctx, &model.Server{ID: "cursor-srv", Name: "K", Type: "game",
		Region: "cn", Version: "1.0.0", Platform: "android", Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.UpsertCharacter(ctx, &model.Character{AccountID: 3, ServerID: "cursor-srv",
		CharacterID: 3, Name: "K", Level: 1, ClassID: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// SearchCharacters routes the cursor through splitCursor; a garbage
	// cursor (no colon) decodes to ("", 0), which resumes at the very start
	// of the order rather than erroring — the row set is the first page.
	chars, _, err := s.SearchCharacters(ctx, store.CharacterSearchFilter{ServerID: "cursor-srv", Cursor: "garbage-cursor"})
	if err != nil {
		t.Fatalf("search with garbage cursor: %v", err)
	}
	if len(chars) != 1 {
		t.Errorf("garbage search cursor should fall back to the first page, got %d rows", len(chars))
	}

	if joinStrings(nil, ",") != "" {
		t.Error("joinStrings(nil) should be empty")
	}
	if parts := splitCursor("no-colon"); len(parts) != 2 || parts[0] != "" || parts[1] != int64(0) {
		t.Errorf("splitCursor(no-colon) = %v, want {\"\", 0}", parts)
	}
	if parts := splitCursor("srv:not-a-number"); parts[1] != int64(0) {
		t.Errorf("splitCursor(bad number) = %v, want second part 0", parts)
	}

	// Limit clamp: a search limit over the cap is normalized, not an error.
	if _, _, err := s.SearchCharacters(ctx, store.CharacterSearchFilter{ServerID: "cursor-srv", Limit: 300}); err != nil {
		t.Fatalf("search with over-cap limit: %v", err)
	}
}

// TestGetStatsSchemaDrift pins the stats aggregation against schema drift:
// a missing character table (and a dropped servers column) surfaces as an
// error, not as zero counts or a silently-skipped scan.
func TestGetStatsSchemaDrift(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()

	if _, err := s.pool.Exec(ctx, `DROP TABLE character_index`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := s.GetStats(ctx); err == nil {
		t.Error("get stats with missing character table: expected error, got nil")
	}

	// A column gone from the servers row set fails the scan, not the count.
	s2, _ := openTestStore(t)
	if _, err := s2.pool.Exec(ctx, `ALTER TABLE servers DROP COLUMN capacity`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if _, err := s2.GetStats(ctx); err == nil {
		t.Error("get stats with dropped servers column: expected error, got nil")
	}
}

// failingRuntime is a RuntimeStore whose ListRuntimes always fails, to pin
// the stats merge error path (the deployment wires Redis; a broken Redis
// must surface as a stats error, not silently zero players).
type failingRuntime struct {
	store.RuntimeStore
	err error
}

func (f *failingRuntime) ListRuntimes(ctx context.Context) (map[string]model.Runtime, error) {
	_ = ctx
	return nil, f.err
}

// TestGetStatsRuntimeFailure pins the runtime-merge failure branch: with a
// failing runtime attached, GetStats reports the merge error.
func TestGetStatsRuntimeFailure(t *testing.T) {
	s, _ := openTestStore(t)
	s.WithRuntime(&failingRuntime{err: errors.New("redis gone")})

	if _, err := s.GetStats(context.Background()); err == nil {
		t.Fatal("get stats with failing runtime: expected error, got nil")
	}
}
