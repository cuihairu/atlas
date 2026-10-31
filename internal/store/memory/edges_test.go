// Edge coverage for the memory store: the belt-and-braces filter mismatch
// arms (server region/realm/shard/version/platform/tag, character
// server/account), the account+server index arm, cursor parse fallback, the
// list clamps and comparators (characters-by-server, migrations, shards,
// maintenance windows), the character patch arms, queue kind labels for the
// rare verbs, the batching return arms, both bounded rings, and replay of a
// character patch.
package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

func edgeServer(id string) *model.Server {
	realm, shard := "realm-1", "shard-1"
	return &model.Server{
		ID: id, Name: id, Type: "game", Region: "cn", Version: "9.9.9", Platform: "gamebox",
		RealmID: &realm, ShardID: &shard, Status: model.StatusOnline,
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 10,
		Tags: []model.ServerTag{{Code: "pve", Public: true}},
	}
}

// TestServerFilterMismatchArms walks every belt-and-braces rejection: the
// index picks the smallest bucket, so each filter pairs a dimension whose
// bucket holds only the odd server with a second dimension that the odd
// server fails — the candidate survives index selection and matchServer
// must drop it.
func TestServerFilterMismatchArms(t *testing.T) {
	ctx := context.Background()
	s := New()
	// Two identical base servers (every base bucket has length 2) plus one
	// odd server that matches only the odd dimension's bucket.
	for _, id := range []string{"f-a", "f-b"} {
		if err := s.RegisterServer(ctx, edgeServer(id)); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	odd := edgeServer("f-odd")
	realm, shard := "realm-9", "shard-9"
	odd.Region, odd.RealmID, odd.ShardID = "us", &realm, &shard
	odd.Version, odd.Platform, odd.Tags = "0.0.1", "pc", nil
	if err := s.RegisterServer(ctx, odd); err != nil {
		t.Fatalf("register odd: %v", err)
	}

	cases := []store.ServerFilter{
		{Region: "cn", Realm: "realm-9"},      // odd fails region
		{Realm: "realm-1", Region: "us"},      // odd fails realm
		{Shard: "shard-1", Region: "us"},      // odd fails shard
		{Version: "9.9.9", Region: "us"},      // odd fails version
		{Platform: "gamebox", Region: "us"},   // odd fails platform
		{Tags: []string{"pve"}, Region: "us"}, // odd carries no tag
	}
	for _, f := range cases {
		out, err := s.ListServers(ctx, f)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		if len(out) != 0 {
			t.Errorf("filter %+v matched %d servers, want none", f, len(out))
		}
	}
	// Sanity: the base servers match every dimension positively.
	pos := store.ServerFilter{Region: "cn", Realm: "realm-1", Shard: "shard-1", Version: "9.9.9", Platform: "gamebox", Tags: []string{"pve"}}
	if out, _ := s.ListServers(ctx, pos); len(out) != 2 {
		t.Errorf("positive filter matched %d, want 2", len(out))
	}
}

// TestSearchCharactersIndexAndMismatchArms pins the three-way index switch
// (account+server compound arm included) and the two mismatch rejections.
func TestSearchCharactersIndexAndMismatchArms(t *testing.T) {
	ctx := context.Background()
	s := New()
	if err := s.UpsertCharacter(ctx, &model.Character{AccountID: 7, ServerID: "s1", CharacterID: 1, Name: "a"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Compound arm: both dimensions set resolves through byAccountServer.
	got, _, err := s.SearchCharacters(ctx, store.CharacterSearchFilter{AccountID: 7, ServerID: "s1"})
	if err != nil || len(got) != 1 {
		t.Fatalf("compound search = %v/%v, want 1 hit", got, err)
	}

	// Mismatch arms: the index yields the candidate, the other dimension
	// rejects it.
	if got, _, _ := s.SearchCharacters(ctx, store.CharacterSearchFilter{AccountID: 7, ServerID: "s2"}); len(got) != 0 {
		t.Errorf("server mismatch returned %d, want 0", len(got))
	}
	if got, _, _ := s.SearchCharacters(ctx, store.CharacterSearchFilter{AccountID: 9, ServerID: "s1"}); len(got) != 0 {
		t.Errorf("account mismatch returned %d, want 0", len(got))
	}

	// An unparseable cursor falls back to the scan start (no skip).
	if got, _, _ := s.SearchCharacters(ctx, store.CharacterSearchFilter{AccountID: 7, ServerID: "s1", Cursor: "junk"}); len(got) != 1 {
		t.Errorf("junk cursor returned %d, want 1", len(got))
	}
}

// TestListClampsAndComparators pins the list paths the contract suite never
// forces: the default/200 clamps, the migration ordering and truncation, the
// shard not-found wrap, and the shard/window sort comparators.
func TestListClampsAndComparators(t *testing.T) {
	ctx := context.Background()
	s := New()

	for i := 1; i <= 2; i++ {
		if err := s.UpsertCharacter(ctx, &model.Character{AccountID: 1, ServerID: "s1", CharacterID: int64(i), Name: fmt.Sprintf("c%d", i)}); err != nil {
			t.Fatalf("upsert char: %v", err)
		}
	}
	chars, err := s.ListCharactersByServer(ctx, "s1", 0, "")
	if err != nil {
		t.Fatalf("list characters (default limit): %v", err)
	}
	if len(chars) != 2 {
		t.Errorf("default limit returned %d, want 2", len(chars))
	}
	chars, err = s.ListCharactersByServer(ctx, "s1", 500, "")
	if err != nil {
		t.Fatalf("list characters (clamped limit): %v", err)
	}
	if len(chars) != 2 {
		t.Errorf("clamped limit returned %d, want 2", len(chars))
	}

	base := time.Now()
	for i, id := range []string{"m-old", "m-mid", "m-new"} {
		if err := s.CreateMigration(ctx, &model.Migration{ID: id, TargetServer: "t", Status: model.MigrationPending, StartedAt: base.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatalf("create migration: %v", err)
		}
	}
	all, err := s.ListMigrations(ctx, 500)
	if err != nil {
		t.Fatalf("list migrations (clamped): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("clamped migration list returned %d, want 3", len(all))
	}
	top, err := s.ListMigrations(ctx, 2)
	if err != nil {
		t.Fatalf("list migrations (limited): %v", err)
	}
	if len(top) != 2 || top[0].ID != "m-new" || top[1].ID != "m-mid" {
		t.Errorf("limited migrations = %v, want newest first truncated to 2", ids12(top))
	}

	if err := s.CreateRealm(ctx, &model.Realm{ID: "r1"}); err != nil {
		t.Fatalf("create realm: %v", err)
	}
	for _, id := range []string{"sh-a", "sh-b"} {
		if err := s.CreateShard(ctx, &model.Shard{ID: id, RealmID: "r1"}); err != nil {
			t.Fatalf("create shard: %v", err)
		}
	}
	if shards, _ := s.ListShards(ctx, "r1", 0); len(shards) != 2 {
		t.Errorf("shards = %d, want 2", len(shards))
	}
	if top, _ := s.ListShards(ctx, "r1", 1); len(top) != 1 {
		t.Errorf("limited shards = %d, want 1", len(top))
	}
	if _, err := s.GetShard(ctx, "ghost"); !store.IsNotFound(err) {
		t.Errorf("get missing shard = %v, want ErrNotFound", err)
	}

	for i, id := range []string{"w-a", "w-b"} {
		if err := s.CreateMaintenanceWindow(ctx, &model.MaintenanceWindow{ID: id, ServerID: "f-srv", StartAt: base.Add(time.Duration(i) * time.Minute), EndAt: base.Add(time.Hour)}); err != nil {
			t.Fatalf("create window: %v", err)
		}
	}
	if wins, _ := s.ListMaintenanceWindows(ctx, "f-srv", 0); len(wins) != 2 {
		t.Errorf("windows = %d, want 2", len(wins))
	}
	if top, _ := s.ListMaintenanceWindows(ctx, "f-srv", 1); len(top) != 1 || !top[0].StartAt.After(base) {
		t.Errorf("limited windows = %+v, want the later one", top)
	}
}

func ids12(ms []*model.Migration) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// TestCharacterPatchArms pins the class and avatar patch fields.
func TestCharacterPatchArms(t *testing.T) {
	ctx := context.Background()
	s := New()
	if err := s.UpsertCharacter(ctx, &model.Character{AccountID: 1, ServerID: "s1", CharacterID: 1, Name: "a"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	class, avatar := 3, "https://cdn/x.png"
	if err := s.UpdateCharacter(ctx, 1, "s1", 1, store.CharacterPatch{ClassID: &class, Avatar: &avatar}); err != nil {
		t.Fatalf("update: %v", err)
	}
	ch, err := s.GetCharacter(ctx, 1, "s1", 1)
	if err != nil || ch.ClassID != class || ch.Avatar != avatar {
		t.Fatalf("patched character = %+v (%v), want class 3 and avatar set", ch, err)
	}
}

// TestQueueKindLabelsForRareVerbs pins the applied-by-kind labels of the
// verbs the rest of the suite never runs through the queue: the CAS status
// transition, the weight write, and the delete.
func TestQueueKindLabelsForRareVerbs(t *testing.T) {
	ctx := context.Background()
	s := New()
	if err := s.RegisterServer(ctx, edgeServer("k-srv")); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := s.UpdateServerStatusIf(ctx, "k-srv", model.StatusOnline, model.StatusMaintenance); err != nil {
		t.Fatalf("cas: %v", err)
	}
	if err := s.UpdateServerWeight(ctx, "k-srv", 5); err != nil {
		t.Fatalf("weight: %v", err)
	}
	if err := s.DeleteServer(ctx, "k-srv"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	byKind := s.QueueStats().AppliedByKind
	for _, want := range []string{"update_server_status_if", "update_server_weight", "delete_server"} {
		if byKind[want] == 0 {
			t.Errorf("applied-by-kind %q missing from %v", want, byKind)
		}
	}
}

// TestDrainBatchReturnArms drives drainAvailable directly for the two exits
// the live lanes rarely hit deterministically: the channel closing while a
// batch is half-built, and the batch cap.
func TestDrainBatchReturnArms(t *testing.T) {
	capped := make(chan *instruction, batchCap+50)
	for i := 0; i < batchCap+50; i++ {
		capped <- &instruction{}
	}
	batch := drainAvailable(capped, &instruction{})
	if len(batch) != batchCap {
		t.Fatalf("capped batch = %d, want %d", len(batch), batchCap)
	}

	closing := make(chan *instruction, 2)
	closing <- &instruction{}
	closing <- &instruction{}
	close(closing)
	batch = drainAvailable(closing, &instruction{})
	if len(batch) != 3 {
		t.Fatalf("batch across close = %d, want 3", len(batch))
	}
}

// TestBoundedRingsEvictOldest pins both replay-related bounds: the
// idempotency-key LRU drops the oldest key past its cap, and the capture
// ring stops growing past its cap.
func TestBoundedRingsEvictOldest(t *testing.T) {
	s := New()
	for i := 0; i < idemCap+5; i++ {
		s.rememberIdempotent(fmt.Sprintf("k-%d", i), uint64(i))
	}
	if len(s.idem) != idemCap {
		t.Fatalf("idempotency map = %d entries, want capped at %d", len(s.idem), idemCap)
	}
	if _, seen := s.idempotentSeen("k-0"); seen {
		t.Error("oldest idempotency key survived eviction")
	}

	ctx := context.Background()
	mem := New()
	if err := mem.RegisterServer(ctx, edgeServer("r-srv")); err != nil {
		t.Fatalf("register: %v", err)
	}
	for i := 0; i < captureCap+5; i++ {
		if err := mem.RecordHeartbeat(ctx, "r-srv", model.Heartbeat{Players: i, Load: 0.1}); err != nil {
			t.Fatalf("heartbeat %d: %v", i, err)
		}
	}
	if got := mem.QueueStats().CaptureLen; got != captureCap {
		t.Fatalf("capture ring = %d, want capped at %d", got, captureCap)
	}
}

// TestReplayCarriesCharacterPatch pins the replay of an update-character
// instruction: the patch rides the capture entry into the sandbox.
func TestReplayCarriesCharacterPatch(t *testing.T) {
	ctx := context.Background()
	src := New()
	if err := src.UpsertCharacter(ctx, &model.Character{AccountID: 1, ServerID: "s1", CharacterID: 1, Name: "before", Level: 1}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	name := "after"
	if err := src.UpdateCharacter(ctx, 1, "s1", 1, store.CharacterPatch{Name: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}

	dst := New()
	if err := ReplayInto(dst, src); err != nil {
		t.Fatalf("replay: %v", err)
	}
	ch, err := dst.GetCharacter(ctx, 1, "s1", 1)
	if err != nil || ch.Name != name || ch.Level != 1 {
		t.Fatalf("replayed character = %+v (%v), want patched name with level kept", ch, err)
	}
}
