package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// recordingAdmin serves the realm/shard endpoints and remembers the order in
// which they were called, so the test can assert realms exist before shards.
type recordingAdmin struct {
	mu     sync.Mutex
	paths  []string
	bodies map[string]map[string]any
	// failOn makes one path answer with failCode instead of 201.
	failOn   string
	failCode int
}

func newRecordingAdmin() *recordingAdmin {
	return &recordingAdmin{bodies: map[string]map[string]any{}}
}

func (a *recordingAdmin) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/admin/realms", a.record)
	mux.HandleFunc("POST /v1/admin/shards", a.record)
	return mux
}

func (a *recordingAdmin) record(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	id, _ := body["id"].(string)

	a.mu.Lock()
	a.paths = append(a.paths, r.URL.Path)
	a.bodies[r.URL.Path+"/"+id] = body
	failing := a.failOn != "" && r.URL.Path == a.failOn
	code := a.failCode
	a.mu.Unlock()

	if failing {
		w.WriteHeader(code)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func testFleet() []fleetEntry {
	return []fleetEntry{
		{ServerID: "game-1001", Region: "cn-east", RealmID: "realm-cn", ShardID: "shard-east"},
		{ServerID: "game-1002", Region: "cn-north", RealmID: "realm-cn", ShardID: "shard-north"},
		{ServerID: "game-9001", Region: "cn-east"},
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEnsureTopologyCreatesRealmsBeforeShards(t *testing.T) {
	rec := newRecordingAdmin()
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()
	admin := &adminClient{base: srv.URL, http: srv.Client()}

	if err := ensureTopology(context.Background(), quietLogger(), admin, testFleet()); err != nil {
		t.Fatalf("ensureTopology: %v", err)
	}

	// The fleet references one realm and two shards. Shards must not precede
	// their realm — Atlas enforces that with a foreign key.
	realmAt, shardAt := -1, -1
	for i, p := range rec.paths {
		switch p {
		case "/v1/admin/realms":
			if realmAt < 0 {
				realmAt = i
			}
		case "/v1/admin/shards":
			if shardAt < 0 {
				shardAt = i
			}
		}
	}
	if realmAt < 0 || shardAt < 0 {
		t.Fatalf("paths = %v, want a realm and a shard call", rec.paths)
	}
	if realmAt > shardAt {
		t.Errorf("realm created after shard: %v", rec.paths)
	}
	if got := rec.bodies["/v1/admin/realms/realm-cn"]["region"]; got != "cn-east" {
		t.Errorf("realm region = %v, want cn-east", got)
	}
	if got := rec.bodies["/v1/admin/shards/shard-east"]["realm_id"]; got != "realm-cn" {
		t.Errorf("shard realm_id = %v, want realm-cn", got)
	}
}

func TestEnsureTopologyTreatsConflictAsSeeded(t *testing.T) {
	rec := newRecordingAdmin()
	rec.failOn, rec.failCode = "/v1/admin/realms", http.StatusConflict
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()
	admin := &adminClient{base: srv.URL, http: srv.Client()}

	// 409 = the row is already there: a container restart must not read its
	// own earlier seed as a failure.
	if err := ensureTopology(context.Background(), quietLogger(), admin, testFleet()); err != nil {
		t.Fatalf("409 should be tolerated: %v", err)
	}
}

func TestEnsureTopologyPropagatesRealFailure(t *testing.T) {
	rec := newRecordingAdmin()
	rec.failOn, rec.failCode = "/v1/admin/shards", http.StatusInternalServerError
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()
	admin := &adminClient{base: srv.URL, http: srv.Client()}

	if err := ensureTopology(context.Background(), quietLogger(), admin, testFleet()); err == nil {
		t.Fatal("a 500 from the admin API must not read as a successful seed")
	}
}

func TestEnsureTopologySkipsWhenFleetHasNoTopology(t *testing.T) {
	rec := newRecordingAdmin()
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()
	admin := &adminClient{base: srv.URL, http: srv.Client()}

	fleet := []fleetEntry{{ServerID: "game-1", Region: "cn-east", Capacity: 10}}
	if err := ensureTopology(context.Background(), quietLogger(), admin, fleet); err != nil {
		t.Fatalf("ensureTopology: %v", err)
	}
	if len(rec.paths) != 0 {
		t.Errorf("paths = %v, want no admin calls", rec.paths)
	}
}

// The built-in fleet is what the deploy stack runs; keep it self-consistent.
func TestDefaultFleetEntriesAreValid(t *testing.T) {
	fleet := defaultFleet()
	for i := range fleet {
		if err := fleet[i].validate(); err != nil {
			t.Errorf("built-in fleet entry %d: %v", i, err)
		}
	}
}
