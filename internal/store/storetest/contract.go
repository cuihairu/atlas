// Package storetest holds the behavioral contract that every Atlas store
// implementation (memory, PostgreSQL, MySQL) must satisfy. The same suite
// runs against all three so the implementations cannot silently drift
// apart — the failure mode that has been caught twice in manual reviews
// (see the project memory: store contract parity).
//
// The suite assumes an empty store when it starts: the SQL harnesses wipe
// their schema first, the memory harness starts from memory.New(). State
// created by one subtest is scoped by unique IDs so subtests stay
// order-independent.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Core is the interface set every persistent store must satisfy. Runtime
// (heartbeat) state is deliberately excluded: it lives in Redis or memory
// and is covered by RunRuntime.
type Core interface {
	store.ServerStore
	store.CharacterStore
	store.MigrationStore
	store.StatsStore
	store.RealmStore
	store.ShardStore
	store.MaintenanceWindowStore
	store.AnnouncementStore
	store.CrossServerConfigStore
}

var seq atomic.Int64

// id returns an identifier unique within this process; the SQL harnesses
// additionally wipe their schema per run, so IDs never collide.
func id(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), seq.Add(1))
}

// sameTime compares two timestamps with a 1µs tolerance: the SQL backends
// store microsecond precision (nanoseconds are truncated or rounded on
// write), while the memory store keeps full nanosecond precision.
func sameTime(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	d := got.Sub(want)
	if d < 0 {
		d = -d
	}
	if d > time.Microsecond {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Run executes the full persistent-store contract against s. rt is the
// heartbeat view the deployment wires into s for stats aggregation — the
// memory harness passes s itself, the SQL harnesses pass the runtime store
// they attached via WithRuntime (the postgres deployment wires Redis).
func Run(t *testing.T, s Core, rt store.RuntimeStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("RealmsShards", func(t *testing.T) { realmsShards(t, ctx, s) })
	t.Run("Servers", func(t *testing.T) { servers(t, ctx, s) })
	t.Run("Characters", func(t *testing.T) { characters(t, ctx, s) })
	t.Run("Migrations", func(t *testing.T) { migrations(t, ctx, s) })
	t.Run("Maintenance", func(t *testing.T) { maintenance(t, ctx, s) })
	t.Run("Announcements", func(t *testing.T) { announcements(t, ctx, s) })
	t.Run("Stats", func(t *testing.T) { stats(t, ctx, s, rt) })
	t.Run("CrossServerConfig", func(t *testing.T) { crossServerConfig(t, ctx, s) })
}

// createRealmShard provisions the realm + shard pair that server
// registration references (PostgreSQL enforces both foreign keys).
func createRealmShard(t *testing.T, ctx context.Context, s Core, prefix string) (realmID, shardID string) {
	t.Helper()

	realmID = prefix + "-realm"
	if err := s.CreateRealm(ctx, &model.Realm{ID: realmID, Name: "realm", Region: "cn-east"}); err != nil {
		t.Fatalf("create realm: %v", err)
	}
	shardID = prefix + "-shard"
	if err := s.CreateShard(ctx, &model.Shard{ID: shardID, RealmID: realmID, Name: "shard"}); err != nil {
		t.Fatalf("create shard: %v", err)
	}
	return realmID, shardID
}

// realmsShards covers realm/shard CRUD, conflict mapping, timestamp
// backfill on the caller, and the realm-scoped shard listing.
func realmsShards(t *testing.T, ctx context.Context, s Core) {
	realmID := id("realm")
	shardID := id("shard")
	otherShardID := id("shard")

	realm := &model.Realm{ID: realmID, Name: "青龙", Region: "cn-east"}
	if err := s.CreateRealm(ctx, realm); err != nil {
		t.Fatalf("create realm: %v", err)
	}
	if realm.CreatedAt.IsZero() {
		t.Error("CreateRealm did not backfill created_at on the caller")
	}

	got, err := s.GetRealm(ctx, realmID)
	if err != nil {
		t.Fatalf("get realm: %v", err)
	}
	if got.Name != "青龙" || got.Region != "cn-east" {
		t.Errorf("realm roundtrip = %+v", got)
	}
	if got.Status != "active" {
		t.Errorf("realm status = %q, want active (defaulted)", got.Status)
	}
	if got.CreatedAt.IsZero() {
		t.Error("stored realm created_at is zero")
	}
	sameTime(t, "realm created_at", got.CreatedAt, realm.CreatedAt)

	if err := s.CreateRealm(ctx, &model.Realm{ID: realmID, Name: "dup"}); err == nil {
		t.Error("duplicate realm accepted, want ErrConflict")
	} else if !isConflict(err) {
		t.Errorf("duplicate realm = %v, want ErrConflict", err)
	}
	if _, err := s.GetRealm(ctx, id("realm")); !isNotFound(err) {
		t.Errorf("missing realm = %v, want ErrNotFound", err)
	}
	if err := s.CreateShard(ctx, &model.Shard{ID: id("shard"), RealmID: id("realm"), Name: "orphan"}); !isNotFound(err) {
		// PostgreSQL enforces the realm FK; the memory store mirrors it with
		// an existence check — no orphan shards anywhere.
		t.Errorf("shard with unknown realm = %v, want ErrNotFound", err)
	}

	// Shards: create, conflict, realm-scoped listing.
	otherRealmID := id("realm")
	if err := s.CreateRealm(ctx, &model.Realm{ID: otherRealmID, Name: "other"}); err != nil {
		t.Fatalf("create other realm: %v", err)
	}
	if err := s.CreateShard(ctx, &model.Shard{ID: shardID, RealmID: realmID, Name: "s1"}); err != nil {
		t.Fatalf("create shard: %v", err)
	}
	if err := s.CreateShard(ctx, &model.Shard{ID: otherShardID, RealmID: otherRealmID, Name: "s2"}); err != nil {
		t.Fatalf("create other shard: %v", err)
	}
	shard, err := s.GetShard(ctx, shardID)
	if err != nil {
		t.Fatalf("get shard: %v", err)
	}
	if shard.RealmID != realmID || shard.Name != "s1" {
		t.Errorf("shard roundtrip = %+v", shard)
	}
	if shard.Status != "active" {
		t.Errorf("shard status = %q, want active (defaulted)", shard.Status)
	}
	if shard.CreatedAt.IsZero() {
		t.Error("CreateShard did not backfill created_at on the caller")
	}
	if err := s.CreateShard(ctx, &model.Shard{ID: shardID, RealmID: realmID, Name: "dup"}); !isConflict(err) {
		t.Errorf("duplicate shard = %v, want ErrConflict", err)
	}
	byRealm, err := s.ListShards(ctx, realmID, 10)
	if err != nil {
		t.Fatalf("list shards by realm: %v", err)
	}
	if len(byRealm) != 1 || byRealm[0].ID != shardID {
		t.Errorf("list shards by realm = %d entries, want only %s", len(byRealm), shardID)
	}
	if all, err := s.ListShards(ctx, "", 10); err != nil || len(all) != 2 {
		t.Errorf("list all shards = %v, %v; want 2", all, err)
	}
}

// servers covers registration defaults and backfill, idempotent
// re-registration (profile overwrite vs creation-time preservation), the
// dead-lifecycle status reset, missing-server error mapping, filtering
// and cursor pagination.
func servers(t *testing.T, ctx context.Context, s Core) {
	region := id("region")
	realmID, shardID := createRealmShard(t, ctx, s, id("srv"))

	// Register: empty status becomes starting, timestamps backfilled.
	srv := &model.Server{
		ID: id("srv"), Name: "srv-a", Type: "game", Region: region,
		RealmID: &realmID, ShardID: &shardID,
		Version: "1.0.0", Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 1000,
		Metadata: map[string]string{"cluster": "c1", "zone": "pvp"},
	}
	if err := s.RegisterServer(ctx, srv); err != nil {
		t.Fatalf("register: %v", err)
	}
	if srv.Status != model.StatusStarting {
		t.Errorf("caller status after register = %q, want starting", srv.Status)
	}
	if srv.CreatedAt.IsZero() || srv.UpdatedAt.IsZero() {
		t.Error("register did not backfill created_at/updated_at on the caller")
	}

	got, err := s.GetServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "srv-a" || got.Type != "game" || got.Region != region ||
		got.Version != "1.0.0" || got.Platform != "android" ||
		got.Endpoint.Host != "10.0.0.1" || got.Endpoint.Port != 30001 ||
		got.Capacity != 1000 || got.Status != model.StatusStarting {
		t.Errorf("register roundtrip = %+v", got)
	}
	if got.RealmID == nil || *got.RealmID != realmID || got.ShardID == nil || *got.ShardID != shardID {
		t.Errorf("realm/shard roundtrip = %v / %v", got.RealmID, got.ShardID)
	}
	if got.Source != "" {
		t.Errorf("source = %q, want empty (API-owned)", got.Source)
	}
	if got.Metadata["cluster"] != "c1" || got.Metadata["zone"] != "pvp" {
		t.Errorf("metadata roundtrip = %v, want cluster=c1 zone=pvp", got.Metadata)
	}
	sameTime(t, "created_at", got.CreatedAt, srv.CreatedAt)

	// Re-register overwrites profile fields but never the creation time,
	// and keeps a live status untouched (online is not dead-ish).
	upd := &model.Server{
		ID: srv.ID, Name: "srv-a-renamed", Type: "game", Region: region,
		RealmID: &realmID, ShardID: &shardID,
		Version: "1.1.0", Platform: "ios",
		Endpoint: model.Endpoint{Host: "10.0.0.2", Port: 30002},
		Capacity: 2000, Status: model.StatusOnline,
	}
	if err := s.RegisterServer(ctx, upd); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	firstCreatedAt := got.CreatedAt
	got, err = s.GetServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("get after re-register: %v", err)
	}
	if got.Name != "srv-a-renamed" || got.Version != "1.1.0" || got.Platform != "ios" ||
		got.Capacity != 2000 || got.Endpoint.Host != "10.0.0.2" || got.Endpoint.Port != 30002 {
		t.Errorf("re-register did not overwrite profile fields: %+v", got)
	}
	sameTime(t, "created_at after re-register", got.CreatedAt, firstCreatedAt)
	if got.Status != model.StatusStarting {
		t.Errorf("status after re-register = %q, want starting (live status kept)", got.Status)
	}
	if len(got.Metadata) != 0 {
		t.Errorf("metadata after undeclared re-register = %v, want cleared (whole-map replace)", got.Metadata)
	}

	// Dead-lifecycle reset: suspect and offline hand the server back to the
	// incoming status; operator-set statuses are preserved.
	statuses := []struct {
		set    model.ServerStatus
		want   model.ServerStatus
		reason string
	}{
		{model.StatusSuspect, model.StatusOnline, "suspect resets on re-register"},
		{model.StatusMaintenance, model.StatusMaintenance, "maintenance preserved"},
		{model.StatusOffline, model.StatusOnline, "offline resets on re-register"},
	}
	for _, tc := range statuses {
		if err := s.UpdateServerStatus(ctx, srv.ID, tc.set); err != nil {
			t.Fatalf("update status %s: %v", tc.set, err)
		}
		if err := s.RegisterServer(ctx, &model.Server{
			ID: srv.ID, Name: upd.Name, Region: region, Version: upd.Version,
			Platform: upd.Platform, Endpoint: upd.Endpoint, Capacity: upd.Capacity,
			Status: model.StatusOnline,
		}); err != nil {
			t.Fatalf("re-register from %s: %v", tc.set, err)
		}
		cur, err := s.GetServer(ctx, srv.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if cur.Status != tc.want {
			t.Errorf("status after %s re-register = %q, want %q (%s)", tc.set, cur.Status, tc.want, tc.reason)
		}
	}

	// Tags are admin-owned configuration: they round-trip, and a
	// re-registering game server can never wipe them.
	tags := []model.ServerTag{
		{Code: model.TagHot, Label: "火热", Tier: model.TierHot, Public: true},
		{Code: "ops_note", Label: "内部", Tier: model.TierNeutral},
	}
	if err := s.UpdateServerTags(ctx, srv.ID, tags); err != nil {
		t.Fatalf("update tags: %v", err)
	}
	if err := s.RegisterServer(ctx, &model.Server{
		ID: srv.ID, Name: upd.Name, Region: region, RealmID: &realmID, ShardID: &shardID,
		Version: upd.Version, Platform: upd.Platform, Endpoint: upd.Endpoint, Capacity: upd.Capacity,
	}); err != nil {
		t.Fatalf("re-register with tags: %v", err)
	}
	got, err = s.GetServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("get after tagging: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("tags after re-register = %+v, want both preserved", got.Tags)
	}
	if !model.HasTag(got.Tags, model.TagHot) {
		t.Errorf("tags after re-register = %+v, want hot", got.Tags)
	}
	for _, tag := range got.Tags {
		if tag.Code == model.TagHot && !tag.Public {
			t.Errorf("hot tag lost public flag: %+v", tag)
		}
		if tag.Code == "ops_note" && tag.Public {
			t.Errorf("internal tag became public: %+v", tag)
		}
	}
	// Clearing is an empty list.
	if err := s.UpdateServerTags(ctx, srv.ID, nil); err != nil {
		t.Fatalf("clear tags: %v", err)
	}
	got, err = s.GetServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if len(got.Tags) != 0 {
		t.Errorf("tags after clear = %+v, want empty", got.Tags)
	}

	// Register is a full profile upsert: a re-register without realm/shard
	// clears the topology reference (unlike tags, which are admin-owned
	// configuration and survive). Documented so the stores cannot drift.
	if err := s.RegisterServer(ctx, &model.Server{
		ID: srv.ID, Name: upd.Name, Region: region, Version: upd.Version,
		Platform: upd.Platform, Endpoint: upd.Endpoint, Capacity: upd.Capacity,
	}); err != nil {
		t.Fatalf("bare re-register: %v", err)
	}
	if bare, err := s.GetServer(ctx, srv.ID); err != nil {
		t.Fatalf("get after bare re-register: %v", err)
	} else if bare.RealmID != nil || bare.ShardID != nil {
		t.Errorf("re-register without realm/shard must clear them: %+v %+v", bare.RealmID, bare.ShardID)
	}

	// Re-establish topology and demote to starting, so the filter block
	// below sees the same shape regardless of the lifecycle probes above.
	if err := s.RegisterServer(ctx, &model.Server{
		ID: srv.ID, Name: upd.Name, Region: region, RealmID: &realmID, ShardID: &shardID,
		Version: upd.Version, Platform: upd.Platform, Endpoint: upd.Endpoint, Capacity: upd.Capacity,
	}); err != nil {
		t.Fatalf("re-register with topology: %v", err)
	}
	if err := s.UpdateServerStatus(ctx, srv.ID, model.StatusStarting); err != nil {
		t.Fatalf("demote to starting: %v", err)
	}

	// Missing-server paths all map to ErrNotFound.
	ghost := id("ghost")
	if _, err := s.GetServer(ctx, ghost); !isNotFound(err) {
		t.Errorf("get missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdateServerStatus(ctx, ghost, model.StatusOnline); !isNotFound(err) {
		t.Errorf("update missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdateServerTags(ctx, ghost, nil); !isNotFound(err) {
		t.Errorf("update tags on missing = %v, want ErrNotFound", err)
	}
	if err := s.DeleteServer(ctx, ghost); !isNotFound(err) {
		t.Errorf("delete missing = %v, want ErrNotFound", err)
	}

	// Filters + pagination, scoped to this subtest's region.
	srv2 := &model.Server{
		ID: id("srv"), Name: "srv-b", Region: region, Version: "2.0.0", Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.3", Port: 30003}, Capacity: 500,
		Status: model.StatusOnline,
	}
	if err := s.RegisterServer(ctx, srv2); err != nil {
		t.Fatalf("register srv-b: %v", err)
	}
	srv3 := &model.Server{
		ID: id("srv"), Name: "srv-c", Region: region, Version: "3.0.0", Platform: "windows",
		Endpoint: model.Endpoint{Host: "10.0.0.4", Port: 30004}, Capacity: 250,
		Status: model.StatusOnline,
	}
	if err := s.RegisterServer(ctx, srv3); err != nil {
		t.Fatalf("register srv-c: %v", err)
	}

	listRegion := func(f store.ServerFilter) []*model.Server {
		t.Helper()
		f.Region = region
		got, err := s.ListServers(ctx, f)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return got
	}
	if got := listRegion(store.ServerFilter{}); len(got) != 3 {
		t.Errorf("region list = %d servers, want 3", len(got))
	}
	if got := listRegion(store.ServerFilter{Status: model.StatusOnline}); len(got) != 2 {
		t.Errorf("region+status list = %d servers, want 2", len(got))
	}
	if got := listRegion(store.ServerFilter{Version: "2.0.0"}); len(got) != 1 || got[0].ID != srv2.ID {
		t.Errorf("region+version list = %+v, want only srv-b", got)
	}
	if got := listRegion(store.ServerFilter{Platform: "ios"}); len(got) != 1 || got[0].ID != srv.ID {
		t.Errorf("region+platform list = %+v, want only srv-a", got)
	}
	if got := listRegion(store.ServerFilter{Realm: realmID}); len(got) != 1 || got[0].ID != srv.ID {
		t.Errorf("region+realm list = %+v, want only srv-a", got)
	}
	if got := listRegion(store.ServerFilter{Shard: shardID}); len(got) != 1 || got[0].ID != srv.ID {
		t.Errorf("region+shard list = %+v, want only srv-a", got)
	}

	// Cursor pagination: pages are ascending by ID, disjoint, complete.
	var collected []string
	cursor := ""
	for i := 0; i < 10; i++ {
		page := listRegion(store.ServerFilter{Limit: 2, Cursor: cursor})
		if len(page) == 0 {
			break
		}
		if len(page) > 2 {
			t.Fatalf("page has %d servers, limit 2", len(page))
		}
		for _, p := range page {
			collected = append(collected, p.ID)
		}
		cursor = page[len(page)-1].ID
	}
	if len(collected) != 3 {
		t.Errorf("cursor walk collected %d servers, want 3 (%v)", len(collected), collected)
	}
	for i := 1; i < len(collected); i++ {
		if collected[i] <= collected[i-1] {
			t.Errorf("cursor walk not ascending: %v", collected)
			break
		}
	}

	// ListServers clamps limit to 200 (exercise with >200 servers in a
	// region of its own so the assertions above stay untouched).
	capRegion := id("region")
	for i := 0; i < 201; i++ {
		if err := s.RegisterServer(ctx, &model.Server{
			ID: fmt.Sprintf("%s-%03d", id("bulk"), i), Region: capRegion, Version: "1.0.0",
			Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30009},
		}); err != nil {
			t.Fatalf("bulk register %d: %v", i, err)
		}
	}
	bulk, err := s.ListServers(ctx, store.ServerFilter{Region: capRegion, Limit: 500})
	if err != nil {
		t.Fatalf("list with limit 500: %v", err)
	}
	if len(bulk) != 200 {
		t.Errorf("limit 500 returned %d servers, want the 200 cap", len(bulk))
	}

	// Limit 0 falls back to the 50-row default page, not "unlimited":
	// callers that need the complete set must cursor-paginate at the cap.
	def, err := s.ListServers(ctx, store.ServerFilter{Region: capRegion})
	if err != nil {
		t.Fatalf("list with default limit: %v", err)
	}
	if len(def) != 50 {
		t.Errorf("default list returned %d servers, want the 50-row default page", len(def))
	}

	// Delete is idempotent-failing: the second delete maps to ErrNotFound.
	if err := s.DeleteServer(ctx, srv3.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetServer(ctx, srv3.ID); !isNotFound(err) {
		t.Errorf("get deleted = %v, want ErrNotFound", err)
	}
	if err := s.DeleteServer(ctx, srv3.ID); !isNotFound(err) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

// characters covers the index projection: upsert roundtrip and backfill,
// overwrite-on-upsert, patch semantics, not-found mapping, the two list
// orders, and both pagination cursors.
func characters(t *testing.T, ctx context.Context, s Core) {
	serverA := id("char-srv")
	serverB := id("char-srv")
	account := time.Now().UnixNano()

	mk := func(serverID string, charID int64, name string, level int) *model.Character {
		return &model.Character{
			AccountID: account, ServerID: serverID, CharacterID: charID,
			Name: name, Level: level, ClassID: 1, Avatar: "a.png",
		}
	}

	// Upsert roundtrip incl. timestamp backfill on the caller.
	ch := mk(serverA, 101, "galadriel", 60)
	if err := s.UpsertCharacter(ctx, ch); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if ch.CreatedAt.IsZero() || ch.UpdatedAt.IsZero() {
		t.Error("upsert did not backfill created_at/updated_at on the caller")
	}
	got, err := s.GetCharacter(ctx, account, serverA, 101)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "galadriel" || got.Level != 60 || got.ClassID != 1 || got.Avatar != "a.png" {
		t.Errorf("upsert roundtrip = %+v", got)
	}
	if got.LastLoginAt != nil {
		t.Errorf("last_login_at = %v, want nil", *got.LastLoginAt)
	}
	sameTime(t, "created_at", got.CreatedAt, ch.CreatedAt)

	// Overwrite-on-upsert keeps creation time, refreshes the rest.
	firstCreatedAt := got.CreatedAt
	ch2 := mk(serverA, 101, "galadriel-the-white", 65)
	ch2.ClassID = 2
	if err := s.UpsertCharacter(ctx, ch2); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	got, err = s.GetCharacter(ctx, account, serverA, 101)
	if err != nil {
		t.Fatalf("get after overwrite: %v", err)
	}
	if got.Name != "galadriel-the-white" || got.Level != 65 || got.ClassID != 2 {
		t.Errorf("overwrite not applied: %+v", got)
	}
	sameTime(t, "created_at after overwrite", got.CreatedAt, firstCreatedAt)

	// LastLoginAt roundtrip (non-nil).
	login := time.Now().UTC().Truncate(time.Microsecond)
	ch3 := mk(serverA, 102, "legolas", 40)
	ch3.LastLoginAt = &login
	if err := s.UpsertCharacter(ctx, ch3); err != nil {
		t.Fatalf("upsert with login: %v", err)
	}
	got, err = s.GetCharacter(ctx, account, serverA, 102)
	if err != nil {
		t.Fatalf("get legolas: %v", err)
	}
	if got.LastLoginAt == nil {
		t.Fatal("last_login_at lost on roundtrip")
	}
	sameTime(t, "last_login_at", *got.LastLoginAt, login)

	// Cross-account lookup and not-found mapping.
	if _, err := s.GetCharacterByCharacterID(ctx, 101); err != nil {
		t.Errorf("get by character id: %v", err)
	}
	missing := time.Now().UnixNano()
	if _, err := s.GetCharacter(ctx, account, serverA, missing); !isNotFound(err) {
		t.Errorf("get missing = %v, want ErrNotFound", err)
	}
	if _, err := s.GetCharacterByCharacterID(ctx, missing); !isNotFound(err) {
		t.Errorf("get by char id missing = %v, want ErrNotFound", err)
	}

	// Patch: only provided fields change; missing rows map to ErrNotFound;
	// an empty patch is a no-op.
	level := 61
	name := "gil-sun-strider"
	meta := map[string]string{"class": "wizard", "vip_level": "6"}
	if err := s.UpdateCharacter(ctx, account, serverA, 101, store.CharacterPatch{Name: &name, Level: &level, Metadata: &meta}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	got, _ = s.GetCharacter(ctx, account, serverA, 101)
	if got.Name != name || got.Level != 61 || got.ClassID != 2 {
		t.Errorf("patch not applied: %+v", got)
	}
	if got.Metadata["class"] != "wizard" || got.Metadata["vip_level"] != "6" {
		t.Errorf("metadata patch roundtrip = %v, want class=wizard vip_level=6", got.Metadata)
	}
	if err := s.UpdateCharacter(ctx, account, serverA, missing, store.CharacterPatch{Level: &level}); !isNotFound(err) {
		t.Errorf("patch missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdateCharacter(ctx, account, serverA, 101, store.CharacterPatch{}); err != nil {
		t.Errorf("empty patch = %v, want nil", err)
	}

	// List by account: ordered by (server_id, character_id).
	if err := s.UpsertCharacter(ctx, mk(serverB, 1, "aragorn", 30)); err != nil {
		t.Fatalf("upsert serverB: %v", err)
	}
	byAccount, err := s.ListCharactersByAccount(ctx, account)
	if err != nil {
		t.Fatalf("list by account: %v", err)
	}
	if len(byAccount) != 3 {
		t.Fatalf("list by account = %d, want 3", len(byAccount))
	}
	wantOrder := []int64{101, 102, 1} // serverA sorts before serverB
	for i, want := range wantOrder {
		if byAccount[i].CharacterID != want {
			t.Errorf("list by account[%d] = char %d, want %d (ordered by server, char)", i, byAccount[i].CharacterID, want)
			break
		}
	}
	empty, err := s.ListCharactersByAccount(ctx, time.Now().UnixNano()+1)
	if err != nil || len(empty) != 0 {
		t.Errorf("list by unknown account = %v, %v; want empty", empty, err)
	}

	// List by server: ordered by character ID, cursor = plain char ID.
	byServer, err := s.ListCharactersByServer(ctx, serverA, 10, "")
	if err != nil {
		t.Fatalf("list by server: %v", err)
	}
	if len(byServer) != 2 {
		t.Fatalf("list by server = %d, want 2", len(byServer))
	}
	page1, err := s.ListCharactersByServer(ctx, serverA, 1, "")
	if err != nil || len(page1) != 1 || page1[0].CharacterID != 101 {
		t.Fatalf("page1 = %v, %v; want char 101", page1, err)
	}
	page2, err := s.ListCharactersByServer(ctx, serverA, 1, "101")
	if err != nil || len(page2) != 1 || page2[0].CharacterID != 102 {
		t.Fatalf("page2 = %v, %v; want char 102", page2, err)
	}
	page3, err := s.ListCharactersByServer(ctx, serverA, 1, "102")
	if err != nil || len(page3) != 0 {
		t.Fatalf("page3 = %v, %v; want empty (cursor marks the end)", page3, err)
	}

	// Search: name (case-insensitive contains), server, account, metadata
	// pair, level range.
	if err := s.UpsertCharacter(ctx, mk(serverB, 2, "frodo", 10)); err != nil {
		t.Fatalf("upsert frodo: %v", err)
	}
	minLvl, maxLvl := 30, 61
	noMax := 1000

	// mind: account chars are gil-sun-strider(61, class2 — patched above)
	// legolas(40, class1) aragorn(30, class1) frodo(10, class1).
	find := func(f store.CharacterSearchFilter) ([]*model.Character, string) {
		t.Helper()
		got, cur, err := s.SearchCharacters(ctx, f)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		return got, cur
	}

	if got, _ := find(store.CharacterSearchFilter{Name: "GIL"}); len(got) != 1 || got[0].Name != "gil-sun-strider" {
		t.Errorf("name search = %+v, want gil-sun-strider (case-insensitive, post-patch name)", got)
	}
	if got, _ := find(store.CharacterSearchFilter{ServerID: serverB}); len(got) != 2 {
		t.Errorf("server search = %d, want 2", len(got))
	}
	// class_id is retired as a built-in filter (platform de-hardening):
	// 职业 filters through metadata; account filters by the opaque 玩家 ID.
	if got, _ := find(store.CharacterSearchFilter{MetadataKey: "class", MetadataValue: "wizard", MinLevel: &minLvl, MaxLevel: &maxLvl}); len(got) != 1 || got[0].Name != "gil-sun-strider" {
		t.Errorf("metadata+level search = %+v, want gil-sun-strider", got)
	}
	if got, _ := find(store.CharacterSearchFilter{MetadataKey: "class", MetadataValue: "archer"}); len(got) != 0 {
		t.Errorf("metadata no-match search = %d, want 0", len(got))
	}
	if got, _ := find(store.CharacterSearchFilter{AccountID: account, MaxLevel: &noMax}); len(got) != 4 {
		t.Errorf("account search = %d, want 4 (every character of the account)", len(got))
	}
	if got, _ := find(store.CharacterSearchFilter{MaxLevel: &noMax}); len(got) != 4 {
		t.Errorf("max-level search = %d, want 4 (covers every character)", len(got))
	}
	if got, _ := find(store.CharacterSearchFilter{Name: "gandalf"}); len(got) != 0 {
		t.Errorf("no-match search = %d, want 0", len(got))
	}

	// Search pagination: cursor is "serverID:characterID", pages are
	// disjoint and complete.
	var seen []string
	cursor := ""
	for i := 0; i < 10; i++ {
		page, next := find(store.CharacterSearchFilter{Limit: 1, Cursor: cursor})
		if len(page) == 0 {
			break
		}
		seen = append(seen, fmt.Sprintf("%s:%d", page[0].ServerID, page[0].CharacterID))
		if next == "" {
			t.Fatal("non-empty page returned an empty next cursor")
		}
		cursor = next
	}
	if len(seen) != 4 {
		t.Errorf("search cursor walk saw %d chars, want 4 (%v)", len(seen), seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Errorf("search cursor walk not ascending: %v", seen)
			break
		}
	}

	// Delete + not-found.
	if err := s.DeleteCharacter(ctx, account, serverA, 102); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetCharacter(ctx, account, serverA, 102); !isNotFound(err) {
		t.Errorf("get deleted = %v, want ErrNotFound", err)
	}
	if err := s.DeleteCharacter(ctx, account, serverA, 102); !isNotFound(err) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

// migrations covers migration records: JSON array roundtrip without
// aliasing the caller's slice, status transitions, not-found mapping and
// the newest-first listing.
func migrations(t *testing.T, ctx context.Context, s Core) {
	migID := id("mig")
	started := time.Now().UTC().Truncate(time.Microsecond)

	mig := &model.Migration{
		ID:            migID,
		SourceServers: []string{"src-a", "src-b"},
		TargetServer:  "dst",
		Status:        model.MigrationPending,
		StartedAt:     started,
	}
	if err := s.CreateMigration(ctx, mig); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetMigration(ctx, migID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TargetServer != "dst" || got.Status != model.MigrationPending {
		t.Errorf("migration roundtrip = %+v", got)
	}
	if len(got.SourceServers) != 2 || got.SourceServers[0] != "src-a" || got.SourceServers[1] != "src-b" {
		t.Errorf("source_servers roundtrip = %v", got.SourceServers)
	}
	sameTime(t, "started_at", got.StartedAt, started)
	if len(got.SourceServers) > 0 {
		got.SourceServers[0] = "mutated"
	}
	again, err := s.GetMigration(ctx, migID)
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if again.SourceServers[0] != "src-a" {
		t.Errorf("source_servers aliased the caller's slice: %v", again.SourceServers)
	}

	if _, err := s.GetMigration(ctx, id("mig")); !isNotFound(err) {
		t.Errorf("get missing = %v, want ErrNotFound", err)
	}

	completed := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.UpdateMigrationStatus(ctx, migID, model.MigrationCompleted, &completed); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, _ = s.GetMigration(ctx, migID)
	if got.Status != model.MigrationCompleted {
		t.Errorf("status = %q, want completed", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatal("completed_at lost on status update")
	}
	sameTime(t, "completed_at", *got.CompletedAt, completed)
	if err := s.UpdateMigrationStatus(ctx, id("mig"), model.MigrationFailed, nil); !isNotFound(err) {
		t.Errorf("update missing = %v, want ErrNotFound", err)
	}

	list, err := s.ListMigrations(ctx, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for i, m := range list {
		if m.ID == migID {
			found = true
		}
		if i > 0 && list[i-1].StartedAt.Before(m.StartedAt) {
			t.Errorf("list not newest-first: %v before %v", list[i-1].StartedAt, m.StartedAt)
		}
	}
	if !found {
		t.Errorf("created migration missing from list of %d", len(list))
	}
}

// maintenance covers the scheduled-window record: roundtrip with and
// without a linked announcement, conflict and not-found mapping, the
// server-scoped listing and the applied-marker transition.
func maintenance(t *testing.T, ctx context.Context, s Core) {
	serverA := id("mw-srv")
	serverB := id("mw-srv")
	winID := id("mw")
	otherID := id("mw")

	start := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	end := start.Add(30 * time.Minute)

	w := &model.MaintenanceWindow{ID: winID, ServerID: serverA, StartAt: start, EndAt: end}
	if err := s.CreateMaintenanceWindow(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.CreatedAt.IsZero() {
		t.Error("CreateMaintenanceWindow did not backfill created_at on the caller")
	}

	got, err := s.GetMaintenanceWindow(ctx, winID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ServerID != serverA {
		t.Errorf("server_id = %q, want %q", got.ServerID, serverA)
	}
	sameTime(t, "start_at", got.StartAt, start)
	sameTime(t, "end_at", got.EndAt, end)
	if got.PreviousStatus != "" {
		t.Errorf("previous_status = %q, want empty before apply", got.PreviousStatus)
	}
	if got.AnnouncementID != nil {
		t.Errorf("announcement_id = %v, want nil", *got.AnnouncementID)
	}

	// Link an announcement on a second window.
	annID := id("ann")
	if err := s.CreateAnnouncement(ctx, &model.Announcement{
		ID: annID, Title: "t", StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create announcement for link: %v", err)
	}
	linked := &model.MaintenanceWindow{
		ID: otherID, ServerID: serverB, StartAt: start, EndAt: end,
		AnnouncementID: &annID,
	}
	if err := s.CreateMaintenanceWindow(ctx, linked); err != nil {
		t.Fatalf("create linked window: %v", err)
	}
	gotOther, err := s.GetMaintenanceWindow(ctx, otherID)
	if err != nil {
		t.Fatalf("get linked: %v", err)
	}
	if gotOther.AnnouncementID == nil || *gotOther.AnnouncementID != annID {
		t.Errorf("announcement_id roundtrip = %v, want %s", gotOther.AnnouncementID, annID)
	}

	// Valid times: the probe targets duplicate-ID conflict mapping, and a
	// zero time would trip MySQL strict mode's zero-date rejection before
	// the duplicate key fires.
	if err := s.CreateMaintenanceWindow(ctx, &model.MaintenanceWindow{ID: winID, ServerID: serverA, StartAt: start, EndAt: end}); !isConflict(err) {
		t.Errorf("duplicate window = %v, want ErrConflict", err)
	}
	if _, err := s.GetMaintenanceWindow(ctx, id("mw")); !isNotFound(err) {
		t.Errorf("get missing = %v, want ErrNotFound", err)
	}

	byServer, err := s.ListMaintenanceWindows(ctx, serverA, 10)
	if err != nil {
		t.Fatalf("list by server: %v", err)
	}
	if len(byServer) != 1 || byServer[0].ID != winID {
		t.Errorf("list by server = %d entries, want only %s", len(byServer), winID)
	}
	if got, err := s.ListMaintenanceWindows(ctx, id("mw-srv"), 10); err != nil || len(got) != 0 {
		t.Errorf("list for unknown server = %v, %v; want empty", got, err)
	}

	// Applying the window records where to return to.
	if err := s.MarkMaintenanceWindowApplied(ctx, winID, model.StatusOnline); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	got, _ = s.GetMaintenanceWindow(ctx, winID)
	if got.PreviousStatus != model.StatusOnline {
		t.Errorf("previous_status = %q, want online", got.PreviousStatus)
	}
	if err := s.MarkMaintenanceWindowApplied(ctx, id("mw"), model.StatusOnline); !isNotFound(err) {
		t.Errorf("mark missing = %v, want ErrNotFound", err)
	}

	// Deleting is unconditional; a second delete must not error.
	if err := s.DeleteMaintenanceWindow(ctx, winID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetMaintenanceWindow(ctx, winID); !isNotFound(err) {
		t.Errorf("get deleted = %v, want ErrNotFound", err)
	}
	if err := s.DeleteMaintenanceWindow(ctx, winID); err != nil {
		t.Errorf("second delete = %v, want nil", err)
	}
}

// announcements covers global and server-scoped announcements, the active
// window filter, conflict/not-found mapping and deletion.
func announcements(t *testing.T, ctx context.Context, s Core) {
	serverA := id("ann-srv")
	serverB := id("ann-srv")
	now := time.Now()

	globalID := id("ann")
	scopedAID := id("ann")
	scopedBID := id("ann")

	create := func(ann *model.Announcement) {
		t.Helper()
		if ann.StartsAt.IsZero() {
			ann.StartsAt = now.Add(-time.Minute)
		}
		if ann.EndsAt.IsZero() {
			ann.EndsAt = now.Add(time.Hour)
		}
		if err := s.CreateAnnouncement(ctx, ann); err != nil {
			t.Fatalf("create announcement %s: %v", ann.ID, err)
		}
		if ann.CreatedAt.IsZero() {
			t.Errorf("announcement %s: created_at not backfilled", ann.ID)
		}
	}

	create(&model.Announcement{ID: globalID, Title: "global", Level: "info"})
	create(&model.Announcement{ID: scopedAID, ServerID: &serverA, Title: "for A", Level: "warning"})
	create(&model.Announcement{
		ID: scopedBID, ServerID: &serverB, Title: "expired", Level: "critical",
		StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(-time.Hour),
	})

	got, err := s.GetAnnouncement(ctx, globalID)
	if err != nil {
		t.Fatalf("get global: %v", err)
	}
	if got.ServerID != nil {
		t.Errorf("global announcement server_id = %v, want nil", *got.ServerID)
	}
	if got.Title != "global" || got.Level != "info" {
		t.Errorf("global roundtrip = %+v", got)
	}
	scoped, err := s.GetAnnouncement(ctx, scopedAID)
	if err != nil {
		t.Fatalf("get scoped: %v", err)
	}
	if scoped.ServerID == nil || *scoped.ServerID != serverA {
		t.Errorf("scoped server_id = %v, want %s", scoped.ServerID, serverA)
	}

	// Valid times for the same reason as the maintenance dup probe above.
	if err := s.CreateAnnouncement(ctx, &model.Announcement{ID: globalID, Title: "dup", StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour)}); !isConflict(err) {
		t.Errorf("duplicate announcement = %v, want ErrConflict", err)
	}
	if _, err := s.GetAnnouncement(ctx, id("ann")); !isNotFound(err) {
		t.Errorf("get missing = %v, want ErrNotFound", err)
	}

	// Filters: server scope sees global + its own; active-only drops the
	// expired one.
	list := func(f store.AnnouncementFilter) []*model.Announcement {
		t.Helper()
		got, err := s.ListAnnouncements(ctx, f)
		if err != nil {
			t.Fatalf("list announcements: %v", err)
		}
		return got
	}
	if got := list(store.AnnouncementFilter{}); len(got) < 3 {
		t.Errorf("all announcements = %d, want >= 3", len(got))
	}
	// Membership checks (not counts): earlier subtests may have added their
	// own announcements — the maintenance window probe links a warning — so
	// only the relative filtering matters here.
	idset := func(list []*model.Announcement) map[string]bool {
		out := map[string]bool{}
		for _, a := range list {
			out[a.ID] = true
		}
		return out
	}
	if got := idset(list(store.AnnouncementFilter{ServerID: serverA})); !got[globalID] || !got[scopedAID] || got[scopedBID] {
		t.Errorf("server A filter = %v, want global + A (not B)", got)
	}
	if got := idset(list(store.AnnouncementFilter{ServerID: serverB, ActiveOnly: true})); !got[globalID] || got[scopedBID] {
		t.Errorf("server B active filter = %v, want global only (B expired)", got)
	}
	if got := idset(list(store.AnnouncementFilter{ActiveOnly: true})); !got[globalID] || !got[scopedAID] || got[scopedBID] {
		t.Errorf("active filter = %v, want global + A (not expired B)", got)
	}

	if err := s.DeleteAnnouncement(ctx, scopedAID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetAnnouncement(ctx, scopedAID); !isNotFound(err) {
		t.Errorf("get deleted = %v, want ErrNotFound", err)
	}
	if err := s.DeleteAnnouncement(ctx, scopedAID); err != nil {
		t.Errorf("second delete = %v, want nil", err)
	}
}

// stats asserts aggregate counters as deltas so the test is independent of
// rows created by other subtests. TotalPlayers is deliberately not
// asserted: runtime state lives outside the persistent stores.
func stats(t *testing.T, ctx context.Context, s Core, rt store.RuntimeStore) {
	before, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats before: %v", err)
	}

	region := id("region")
	statSrv := id("stat-srv")
	const capacity = 777
	if err := s.RegisterServer(ctx, &model.Server{
		ID: statSrv, Region: region, Version: "9.9.9", Capacity: capacity,
		Endpoint: model.Endpoint{Host: "10.0.0.5", Port: 30005},
		Status:   model.StatusOnline,
	}); err != nil {
		t.Fatalf("register for stats: %v", err)
	}
	account := time.Now().UnixNano()
	for i, charID := range []int64{1, 2} {
		if err := s.UpsertCharacter(ctx, &model.Character{
			AccountID: account, ServerID: statSrv, CharacterID: charID,
			Name: fmt.Sprintf("c%d", i), Level: 1,
		}); err != nil {
			t.Fatalf("upsert for stats: %v", err)
		}
	}

	after, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats after: %v", err)
	}
	if after.TotalServers != before.TotalServers+1 {
		t.Errorf("TotalServers = %d, want %d", after.TotalServers, before.TotalServers+1)
	}
	if after.TotalCapacity != before.TotalCapacity+capacity {
		t.Errorf("TotalCapacity = %d, want %d", after.TotalCapacity, before.TotalCapacity+capacity)
	}
	if after.TotalCharacters != before.TotalCharacters+2 {
		t.Errorf("TotalCharacters = %d, want %d", after.TotalCharacters, before.TotalCharacters+2)
	}
	if after.ServersByRegion[region] != 1 {
		t.Errorf("ServersByRegion[%s] = %d, want 1", region, after.ServersByRegion[region])
	}
	if after.ServersByStatus[string(model.StatusOnline)] != before.ServersByStatus[string(model.StatusOnline)]+1 {
		t.Errorf("ServersByStatus[online] = %d, want %d",
			after.ServersByStatus[string(model.StatusOnline)],
			before.ServersByStatus[string(model.StatusOnline)]+1)
	}
	if after.ServersByVersion["9.9.9"] != 1 {
		t.Errorf("ServersByVersion[9.9.9] = %d, want 1", after.ServersByVersion["9.9.9"])
	}
	if after.ServersByRegion == nil || after.ServersByStatus == nil || after.ServersByVersion == nil {
		t.Error("stats maps must be non-nil")
	}

	// TotalPlayers aggregates the heartbeat view: SQL deployments merge the
	// wired runtime store (Redis in production), the memory deployment sums
	// its own runtimes. Either way the numbers must land in stats — the
	// historical failure mode is a silent 0.
	const players = 4242
	if err := rt.RecordHeartbeat(ctx, statSrv, model.Heartbeat{Players: players, Load: 0.5}); err != nil {
		t.Fatalf("heartbeat for stats: %v", err)
	}
	merged, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats after heartbeat: %v", err)
	}
	if merged.TotalPlayers != before.TotalPlayers+players {
		t.Errorf("TotalPlayers = %d, want %d (heartbeat players must reach stats)",
			merged.TotalPlayers, before.TotalPlayers+players)
	}
}

// RunRuntime executes the runtime (heartbeat) store contract.
func RunRuntime(t *testing.T, rt store.RuntimeStore) {
	t.Helper()
	ctx := context.Background()

	srvID := id("rt")
	if err := rt.RecordHeartbeat(ctx, srvID, model.Heartbeat{
		Players: 42, Load: 0.75, Status: model.StatusOnline,
	}); err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}
	got, err := rt.GetRuntime(ctx, srvID)
	if err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	if got.Players != 42 || got.Load != 0.75 || got.Status != model.StatusOnline {
		t.Errorf("runtime roundtrip = %+v", got)
	}
	if got.LastSeenAt.IsZero() {
		t.Error("last_seen_at not stamped")
	}
	d := time.Since(got.LastSeenAt)
	if d < -time.Minute || d > 5*time.Minute {
		t.Errorf("last_seen_at = %v, not within a minute of now", got.LastSeenAt)
	}

	if _, err := rt.GetRuntime(ctx, id("rt")); !isNotFound(err) {
		t.Errorf("get missing runtime = %v, want ErrNotFound", err)
	}

	// Batch read (read-path pipelining): present keys keep their values,
	// missing keys are simply absent — no ErrNotFound per key — and an
	// empty id list is a no-op. This is the contract discovery's list
	// path relies on (docs/performance.md §2).
	batch, err := rt.GetRuntimes(ctx, []string{srvID, id("rt")})
	if err != nil {
		t.Fatalf("get runtimes: %v", err)
	}
	if len(batch) != 1 {
		t.Errorf("get runtimes = %d keys, want 1 (missing keys must be absent)", len(batch))
	}
	if got, ok := batch[srvID]; !ok {
		t.Errorf("get runtimes missing %s", srvID)
	} else if got.Players != 42 || got.Load != 0.75 || got.Status != model.StatusOnline {
		t.Errorf("batch runtime roundtrip = %+v, want players=42 load=0.75 online", got)
	} else if got.LastSeenAt.IsZero() {
		t.Error("batch runtime last_seen_at not stamped")
	}
	empty, err := rt.GetRuntimes(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("get runtimes(nil) = %v, %v; want empty map, nil error", empty, err)
	}

	if err := rt.DeleteRuntime(ctx, id("rt")); !isNotFound(err) {
		t.Errorf("delete missing runtime = %v, want ErrNotFound", err)
	}
	if err := rt.DeleteRuntime(ctx, srvID); err != nil {
		t.Fatalf("delete runtime: %v", err)
	}
	if _, err := rt.GetRuntime(ctx, srvID); !isNotFound(err) {
		t.Errorf("runtime after delete = %v, want ErrNotFound", err)
	}
}

// crossServerConfig pins the config-center storage contract: the config is
// a single document, an unwritten center reports ErrNotFound, every save
// bumps the version monotonically and rewrites hash/spec/updated_at, and
// saving the same content twice must still be a distinct version (version
// monotonicity is the storage layer's job, idempotency lives in the
// service above it).
func crossServerConfig(t *testing.T, ctx context.Context, s Core) {
	spec := model.CrossServerSpec{
		Topology: model.CrossServerTopology{Clusters: []model.CrossServerCluster{
			{ID: "cluster-a", Name: "A", Region: "cn-east", Servers: []string{"srv-1", "srv-2"}},
		}},
		Groups:       []model.CrossServerGroup{{ID: "g1", Name: "season-1", Servers: []string{"srv-1"}}},
		Features:     map[string]bool{"world-boss": true, "arena": false},
		MatchDomains: []model.CrossServerMatchDomain{{ID: "md-1", Servers: []string{"srv-1", "srv-2"}, Params: map[string]string{"mmr_range": "500"}}},
		CrossPlayTypes: []model.CrossPlayType{
			{ID: "battlefield", Name: "跨服战场", Lifecycle: model.CrossPlayLifecycleSeasonal, Matchmaking: true, Ranking: true, IDPrefix: "xb"},
		},
	}
	spec = model.NormalizeCrossServerSpec(spec)

	if _, err := s.GetCrossServerConfig(ctx); !isNotFound(err) {
		t.Errorf("get unwritten config = %v, want ErrNotFound", err)
	}

	first, err := s.SaveCrossServerConfig(ctx, &model.CrossServerConfig{
		Hash: model.HashCrossServerSpec(spec),
		Spec: spec,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if first.Version < 1 {
		t.Errorf("first save version = %d, want >= 1", first.Version)
	}
	if first.UpdatedAt.IsZero() {
		t.Error("save did not stamp updated_at")
	}

	got, err := s.GetCrossServerConfig(ctx)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if got.Version != first.Version || got.Hash != first.Hash {
		t.Errorf("roundtrip = version %d hash %q, want %d/%q", got.Version, got.Hash, first.Version, first.Hash)
	}
	if len(got.Spec.Topology.Clusters) != 1 || got.Spec.Topology.Clusters[0].ID != "cluster-a" {
		t.Errorf("topology roundtrip = %+v", got.Spec.Topology.Clusters)
	}
	if !got.Spec.Features["world-boss"] || got.Spec.Features["arena"] {
		t.Errorf("features roundtrip = %+v", got.Spec.Features)
	}
	if len(got.Spec.MatchDomains) != 1 || got.Spec.MatchDomains[0].Params["mmr_range"] != "500" {
		t.Errorf("match domain roundtrip = %+v", got.Spec.MatchDomains)
	}
	if len(got.Spec.CrossPlayTypes) != 1 || got.Spec.CrossPlayTypes[0].ID != "battlefield" ||
		got.Spec.CrossPlayTypes[0].IDPrefix != "xb" || !got.Spec.CrossPlayTypes[0].Matchmaking {
		t.Errorf("crossplay type roundtrip = %+v", got.Spec.CrossPlayTypes)
	}

	// The stored document must not alias the caller's spec: mutating the
	// object that was handed to Save in place afterwards must leave the
	// store untouched (the service diffs the stored copy against the
	// incoming one — a shared backing array would corrupt that diff).
	spec.Topology.Clusters[0].Servers = append(spec.Topology.Clusters[0].Servers, "srv-late")
	spec.Features["world-boss"] = !spec.Features["world-boss"]
	got, err = s.GetCrossServerConfig(ctx)
	if err != nil {
		t.Fatalf("get config after caller mutation: %v", err)
	}
	if len(got.Spec.Topology.Clusters[0].Servers) != 2 || !got.Spec.Features["world-boss"] {
		t.Errorf("caller-side mutation leaked into the store: %+v", got.Spec)
	}
	spec.Topology.Clusters[0].Servers = spec.Topology.Clusters[0].Servers[:2]
	spec.Features["world-boss"] = !spec.Features["world-boss"]

	// A second, different save must move the version forward and replace
	// the document.
	updated := model.NormalizeCrossServerSpec(model.CrossServerSpec{
		Features: map[string]bool{"world-boss": false},
	})
	second, err := s.SaveCrossServerConfig(ctx, &model.CrossServerConfig{
		Hash: model.HashCrossServerSpec(updated),
		Spec: updated,
	})
	if err != nil {
		t.Fatalf("save updated config: %v", err)
	}
	if second.Version <= first.Version {
		t.Errorf("version did not advance: %d then %d", first.Version, second.Version)
	}
	got, err = s.GetCrossServerConfig(ctx)
	if err != nil {
		t.Fatalf("get config after update: %v", err)
	}
	if len(got.Spec.Topology.Clusters) != 0 {
		t.Errorf("update did not replace the document: %+v", got.Spec.Topology)
	}
	if got.Hash != model.HashCrossServerSpec(updated) {
		t.Errorf("hash = %q, want %q", got.Hash, model.HashCrossServerSpec(updated))
	}

	// Saving identical content is still a write: the storage layer does not
	// dedupe (the service's idempotency check happens before it), and the
	// version must keep climbing so a receiver can trust it as a clock.
	third, err := s.SaveCrossServerConfig(ctx, &model.CrossServerConfig{
		Hash: model.HashCrossServerSpec(updated),
		Spec: updated,
	})
	if err != nil {
		t.Fatalf("save identical config: %v", err)
	}
	if third.Version <= second.Version {
		t.Errorf("version regressed on identical save: %d then %d", second.Version, third.Version)
	}
}

func isNotFound(err error) bool {
	return err != nil && errors.Is(err, store.ErrNotFound)
}

func isConflict(err error) bool {
	return err != nil && errors.Is(err, store.ErrConflict)
}
