// Error-path coverage for the mysql store: every method's query-failure
// wrap is driven with a canceled context (the operational reality of a
// request-scoped deadline expiring mid-query), plus the JSON-path and
// duplicate-detection helpers that guard injection and conflict mapping.
package mysql

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
	// Close is the shutdown path; every later query must fail, not hang.
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

// TestPatchEveryField pins the full dynamic UPDATE: every CharacterPatch
// arm must land in its column (the store contract asserts partial patches,
// not the all-fields shape).
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
}

func intptr(v int) *int { return &v }

// TestListLimitClamp pins the listing clamps (limit <= 0 → 50, > 200 → 200):
// both edges must stay error-free with real rows behind them.
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

// TestJSONPathKey pins the JSON-path guard: only identifier-safe keys may be
// embedded in a path (paths cannot be parameterized), anything else filters
// to zero rows instead of risking injection.
func TestJSONPathKey(t *testing.T) {
	for key, want := range map[string]string{
		"plain":       `."plain"`,
		"dot.key":     `."dot.key"`,
		"under_score": `."under_score"`,
		"a-b.c9":      `."a-b.c9"`,
	} {
		got, ok := jsonPathKey(key)
		if !ok || got != "$"+want {
			t.Errorf("jsonPathKey(%q) = %q, %v; want %q, true", key, got, ok, "$"+want)
		}
	}
	for _, key := range []string{"", "has space", "semi;colon", "quo\"te", "bra`ck", "unié"} {
		if got, ok := jsonPathKey(key); ok {
			t.Errorf("jsonPathKey(%q) = %q, true; want rejected", key, got)
		}
	}
}

// TestConflictIfExists pins duplicate detection: a failed INSERT whose row
// is found on follow-up lookup maps to ErrConflict; otherwise the original
// error survives.
func TestConflictIfExists(t *testing.T) {
	execErr := errors.New("insert failed")
	lookupErr := errors.New("lookup failed")

	err := conflictIfExists(execErr, nil, "create shard", "sh-1")
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("existing row should map to ErrConflict, got %v", err)
	}

	err = conflictIfExists(execErr, lookupErr, "create shard", "sh-1")
	if !errors.Is(err, execErr) || errors.Is(err, store.ErrConflict) {
		t.Errorf("missing row should keep the original error, got %v", err)
	}
}

// TestCorruptTagsColumn pins decode resilience for the one TEXT-stored JSON
// column: damaged tags JSON surfaces as a wrapped error on every read path,
// never as a panic or silently dropped rows. (The other JSON columns are
// MySQL JSON type — the server itself rejects invalid text on write, so
// their decode branches guard a TEXT-schema future only.)
func TestCorruptTagsColumn(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()

	if err := s.RegisterServer(ctx, &model.Server{ID: "corrupt-srv", Name: "C", Type: "game",
		Region: "cn", Version: "1.0.0", Platform: "android", Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE servers SET tags = ? WHERE id = ?", "{not-json", "corrupt-srv"); err != nil {
		t.Fatalf("corrupt tags: %v", err)
	}
	if _, err := s.GetServer(ctx, "corrupt-srv"); err == nil {
		t.Error("get server with corrupt tags: expected error, got nil")
	}
	if _, err := s.ListServers(ctx, store.ServerFilter{}); err == nil {
		t.Error("list servers with corrupt tags: expected error, got nil")
	}
}

// TestGetShardMissing pins the not-found mapping of the single-shard read.
func TestGetShardMissing(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.GetShard(context.Background(), "no-such-shard"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestSearchFilterEdges pins the search-side clamps and the unsafe-metadata
// key path: a key that cannot appear in a JSON path filters to zero rows
// instead of failing or matching everything.
func TestSearchFilterEdges(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := s.RegisterServer(ctx, &model.Server{ID: "edge-srv", Name: "E", Type: "game",
		Region: "cn", Version: "1.0.0", Platform: "android", Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 1}, Capacity: 10}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.UpsertCharacter(ctx, &model.Character{AccountID: 2, ServerID: "edge-srv",
		CharacterID: 2, Name: "E", Level: 1, ClassID: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	chars, cursor, err := s.SearchCharacters(ctx, store.CharacterSearchFilter{
		MetadataKey:   "bad key!",
		MetadataValue: "v",
		Limit:         300,
	})
	if err != nil {
		t.Fatalf("unsafe metadata key: %v", err)
	}
	if len(chars) != 0 || cursor != "" {
		t.Errorf("unsafe metadata key should match nothing, got %d rows", len(chars))
	}
}

// TestListByServerGarbageCursor pins the cursor decode fallback: an opaque
// cursor degrades to the first page instead of erroring.
func TestListByServerGarbageCursor(t *testing.T) {
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

	chars, err := s.ListCharactersByServer(ctx, "cursor-srv", 10, "garbage-cursor")
	if err != nil {
		t.Fatalf("garbage cursor: %v", err)
	}
	if len(chars) != 1 {
		t.Errorf("garbage cursor should fall back to the first page, got %d rows", len(chars))
	}
	if joinStrings(nil, ",") != "" {
		t.Error("joinStrings(nil) should be empty")
	}
}

// TestGetStatsSchemaDrift pins the stats aggregation against schema drift:
// a missing character table surfaces as an error, not as zero counts.
func TestGetStatsSchemaDrift(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx, "DROP TABLE character_index"); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := s.GetStats(ctx); err == nil {
		t.Error("get stats with missing character table: expected error, got nil")
	}
}
