package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// TestMatchesFieldByField drives the shared predicate filter by filter so
// the index List and the store-backed fallback can never disagree.
func TestMatchesFieldByField(t *testing.T) {
	srv := mkServer("game-1", "cn-east", "online", "1.0.0", 100)
	realm, shard := "realm-1", "shard-1"
	srv.RealmID = &realm
	srv.ShardID = &shard
	srv.Tags = []model.ServerTag{{Code: "hot", Public: true}}
	srv.Metadata = map[string]string{"zone": "pvp"}
	bare := mkServer("game-9", "cn-east", "online", "1.0.0", 100)

	cases := []struct {
		name string
		f    ListFilter
		srv  *model.Server
		want bool
	}{
		{"empty filter matches everything", ListFilter{}, srv, true},
		{"id substring hit", ListFilter{ID: "ame-"}, srv, true},
		{"id miss", ListFilter{ID: "game-2"}, srv, false},
		{"status match", ListFilter{Status: model.StatusOnline}, srv, true},
		{"status mismatch", ListFilter{Status: model.StatusOffline}, srv, false},
		{"region match", ListFilter{Region: "cn-east"}, srv, true},
		{"region mismatch", ListFilter{Region: "us"}, srv, false},
		{"realm match", ListFilter{Realm: "realm-1"}, srv, true},
		{"realm absent on server", ListFilter{Realm: "realm-1"}, bare, false},
		{"realm mismatch", ListFilter{Realm: "realm-2"}, srv, false},
		{"shard match", ListFilter{Shard: "shard-1"}, srv, true},
		{"shard absent on server", ListFilter{Shard: "shard-1"}, bare, false},
		{"version match", ListFilter{Version: "1.0.0"}, srv, true},
		{"version mismatch", ListFilter{Version: "2.0"}, srv, false},
		{"type match", ListFilter{Type: "game"}, srv, true},
		{"type mismatch", ListFilter{Type: "cross"}, srv, false},
		{"platform match", ListFilter{Platform: "pc"}, srv, true},
		{"platform mismatch", ListFilter{Platform: "mobile"}, srv, false},
		{"tag present", ListFilter{Tag: "hot"}, srv, true},
		{"tag absent", ListFilter{Tag: "cold"}, srv, false},
		{"metadata hit", ListFilter{MetadataKey: "zone", MetadataValue: "pvp"}, srv, true},
		{"metadata value miss", ListFilter{MetadataKey: "zone", MetadataValue: "pve"}, srv, false},
		// A missing key reads as the zero value: an empty wanted value
		// matches a server without the key at all (documented predicate
		// behaviour, shared by both List paths).
		{"metadata key miss with empty value matches", ListFilter{MetadataKey: "map"}, srv, true},
		{"metadata key miss with value", ListFilter{MetadataKey: "map", MetadataValue: "lobby"}, srv, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.srv, tc.f); got != tc.want {
				t.Fatalf("Matches = %v, want %v (filter %+v)", got, tc.want, tc.f)
			}
		})
	}
}

// flakyServers fails exactly the tracked writes it is configured to, so
// each decorator's error path can be driven without a real store outage.
type flakyServers struct {
	store.ServerStore
	statusErr error
	tagsErr   error
	deleteErr error
}

func (f *flakyServers) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	return f.statusErr
}

func (f *flakyServers) UpdateServerTags(ctx context.Context, id string, tags []model.ServerTag) error {
	return f.tagsErr
}

func (f *flakyServers) DeleteServer(ctx context.Context, id string) error { return f.deleteErr }

type flakyRuntime struct {
	store.RuntimeStore
	hbErr     error
	deleteErr error
}

func (f *flakyRuntime) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	return f.hbErr
}

func (f *flakyRuntime) DeleteRuntime(ctx context.Context, id string) error { return f.deleteErr }

// TestTrackDecoratorsPropagateStoreErrors pins the decorator contract on
// failure: the store error is returned untouched and the index keeps its
// previous state (no half-applied projections).
func TestTrackDecoratorsPropagateStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	idx := New()
	base := mkServer("game-1", "cn", "online", "1.0.0", 100)
	idx.Upsert(base)
	// Live gauges come from the runtime projection, not the profile upsert.
	idx.UpdateRuntime("game-1", 7, 0.5, time.Now())

	servers := &trackedServers{ServerStore: &flakyServers{statusErr: boom, tagsErr: boom, deleteErr: boom}, idx: idx}
	runtime := &trackedRuntime{RuntimeStore: &flakyRuntime{hbErr: boom, deleteErr: boom}, idx: idx}
	ctx := context.Background()

	if err := servers.UpdateServerStatus(ctx, "game-1", model.StatusDraining); !errors.Is(err, boom) {
		t.Fatalf("UpdateServerStatus error = %v, want boom", err)
	}
	if got, _ := idx.Get("game-1"); got.Status != model.StatusOnline {
		t.Fatalf("status projected into the index despite store error: %s", got.Status)
	}

	if err := servers.UpdateServerTags(ctx, "game-1", []model.ServerTag{{Code: "new"}}); !errors.Is(err, boom) {
		t.Fatalf("UpdateServerTags error = %v, want boom", err)
	}

	if err := servers.DeleteServer(ctx, "game-1"); !errors.Is(err, boom) {
		t.Fatalf("DeleteServer error = %v, want boom", err)
	}
	if _, ok := idx.Get("game-1"); !ok {
		t.Fatal("record removed from the index despite store error")
	}

	if err := runtime.RecordHeartbeat(ctx, "game-1", model.Heartbeat{Players: 99}); !errors.Is(err, boom) {
		t.Fatalf("RecordHeartbeat error = %v, want boom", err)
	}
	if got, _ := idx.Get("game-1"); got.Players != 7 {
		t.Fatalf("players projected into the index despite store error: %d", got.Players)
	}

	if err := runtime.DeleteRuntime(ctx, "game-1"); !errors.Is(err, boom) {
		t.Fatalf("DeleteRuntime error = %v, want boom", err)
	}
	if got, _ := idx.Get("game-1"); got.Players != 7 {
		t.Fatalf("live gauges zeroed despite store error: %d", got.Players)
	}
}
