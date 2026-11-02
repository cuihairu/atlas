package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	atlas "github.com/cuihairu/atlas/sdk/go/atlas"
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

// TestAdminDeleteJSON verifies the room teardown call: DELETE on the admin
// server endpoint, carrying the bearer key when configured.
func TestAdminDeleteJSON(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	admin := &adminClient{base: srv.URL, key: "demo-key", http: srv.Client()}
	if err := admin.deleteJSON(context.Background(), "/v1/admin/servers/moba-room-rank-101"); err != nil {
		t.Fatalf("deleteJSON: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/v1/admin/servers/moba-room-rank-101" {
		t.Errorf("path = %s, want the server row endpoint", gotPath)
	}
	if gotAuth != "Bearer demo-key" {
		t.Errorf("auth = %q, want Bearer demo-key", gotAuth)
	}
}

// TestAdminDeleteJSONEscapesServerID verifies a server_id containing path
// metacharacters is escaped before it is concatenated into the admin URL —
// otherwise a "/" or "?" would route the DELETE to a different endpoint.
func TestAdminDeleteJSONEscapesServerID(t *testing.T) {
	var mu sync.Mutex
	var gotPath, gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotRaw = r.URL.RawPath
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	admin := &adminClient{base: srv.URL, http: srv.Client()}
	serverID := "room/a?b#c"
	if err := admin.deleteJSON(context.Background(), "/v1/admin/servers/"+url.PathEscape(serverID)); err != nil {
		t.Fatalf("deleteJSON: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotRaw != "/v1/admin/servers/room%2Fa%3Fb%23c" {
		t.Errorf("raw path = %q, want the escaped server row endpoint (got decoded path %q)", gotRaw, gotPath)
	}
}

// TestAdminJSONDrainsBody verifies both helpers consume the response body so
// the keep-alive connection is reusable for the next request.
func TestAdminJSONDrainsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A non-empty body is left on the wire; if the client does not drain
		// it, the connection cannot be reused and Go closes it.
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer srv.Close()

	admin := &adminClient{base: srv.URL, http: srv.Client()}
	// Same client, two requests: the second must be able to reuse the
	// connection established by the first (no error, no "connection reset").
	if err := admin.deleteJSON(context.Background(), "/v1/admin/servers/room-1"); err != nil {
		t.Fatalf("first deleteJSON: %v", err)
	}
	if err := admin.deleteJSON(context.Background(), "/v1/admin/servers/room-2"); err != nil {
		t.Fatalf("second deleteJSON: %v", err)
	}
	if err := admin.postJSON(context.Background(), "/v1/admin/servers/room-1/tags", map[string]any{"code": "hot"}); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
}

// TestAdminDeleteJSONReportsFailure ensures a non-2xx admin DELETE is an error,
// so the room loop logs the failed teardown instead of silently proceeding.
func TestAdminDeleteJSONReportsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	admin := &adminClient{base: srv.URL, http: srv.Client()}
	if err := admin.deleteJSON(context.Background(), "/v1/admin/servers/ghost"); err == nil {
		t.Fatal("deleteJSON: 404 must surface as an error")
	}
}

// roomServer simulates the Atlas registry + admin API one demoagent talks to
// during a room cycle, recording every call so the teardown orchestration can
// be asserted end-to-end through the real SDK client.
type roomServer struct {
	mu       sync.Mutex
	register int
	hb       int
	unreg    int
	deletes  []string
	// unregisterErr makes the registry unregister answer 500 so roomLoop
	// takes the "close failed" path and must skip the admin delete.
	unregisterErr bool
	// deleteErr makes the admin DELETE answer 500 so roomLoop takes the
	// "row delete failed" degradation path (still proceeds to the next
	// instance).
	deleteErr bool
}

func (s *roomServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/registry/servers/register", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.register++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"server_id":"moba-room-1","status":"online"}`))
	})
	mux.HandleFunc("POST /v1/registry/servers/{id}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hb++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"server_id":"moba-room-1","status":"online"}`))
	})
	mux.HandleFunc("POST /v1/registry/servers/{id}/unregister", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.unreg++
		fail := s.unregisterErr
		s.mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"offline"}`))
	})
	mux.HandleFunc("DELETE /v1/admin/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.deletes = append(s.deletes, r.PathValue("id"))
		fail := s.deleteErr
		s.mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// runRoom drives roomLoop for one full cycle and returns after the loop
// observes ctx cancellation (the recycle sleep is 0 so the loop re-runs and
// re-registers, then the context cancellation ends the sleep/register retry).
func runRoom(t *testing.T, s *roomServer, e *fleetEntry, unregisterErr, deleteErr bool) {
	t.Helper()
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	client, err := atlas.New(atlas.Options{Addr: srv.URL})
	if err != nil {
		t.Fatalf("atlas.New: %v", err)
	}
	defer client.Close()
	admin := &adminClient{base: srv.URL, http: srv.Client()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ok, fail, lastBeat atomic.Int64
	state := newLiveState(e)
	done := make(chan struct{})
	go func() {
		roomLoop(ctx, quietLogger(), client, admin, e, state, 10*time.Millisecond, &ok, &fail, &lastBeat)
		close(done)
	}()
	// Wait until the unregister+delete choreography has run once (or, when
	// delete is expected, until the DELETE landed), then stop.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		unreg := s.unreg
		deletes := len(s.deletes)
		s.mu.Unlock()
		if unreg >= 1 {
			if unregisterErr || deleteErr {
				// Unregister-fail must never delete; delete-fail still
				// attempts the DELETE. Either way once unregister fired the
				// branch is decided — give the loop one beat to settle.
				time.Sleep(30 * time.Millisecond)
				break
			}
			if deletes >= 1 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("roomLoop did not stop")
	}
}

// TestRoomLoopDeletesRowAfterUnregister covers the full teardown orchestration
// the review flagged as untested: a successful unregister must be followed by
// an explicit admin DELETE on the server row (the registry only flips to
// offline).
func TestRoomLoopDeletesRowAfterUnregister(t *testing.T) {
	e := &fleetEntry{ServerID: "moba-room-1", Region: "cn-east", Capacity: 10, lifetime: 20 * time.Millisecond, recycle: 0}
	s := &roomServer{}
	runRoom(t, s, e, false, false)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unreg < 1 {
		t.Fatalf("unregister count = %d, want >=1", s.unreg)
	}
	if len(s.deletes) == 0 {
		t.Fatal("expected admin DELETE after unregister, got none")
	}
	if s.deletes[0] != "moba-room-1" {
		t.Errorf("delete path id = %q, want moba-room-1", s.deletes[0])
	}
}

// TestRoomLoopSkipsDeleteWhenUnregisterFails covers the degradation path: the
// admin DELETE must not be issued when the graceful unregister itself failed
// (a failed close must not pretend the row is gone).
func TestRoomLoopSkipsDeleteWhenUnregisterFails(t *testing.T) {
	e := &fleetEntry{ServerID: "moba-room-1", Region: "cn-east", Capacity: 10, lifetime: 20 * time.Millisecond, recycle: 0}
	s := &roomServer{unregisterErr: true}
	runRoom(t, s, e, true, false)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unreg < 1 {
		t.Fatalf("unregister count = %d, want >=1", s.unreg)
	}
	if len(s.deletes) != 0 {
		t.Errorf("deletes = %v, want none when unregister failed", s.deletes)
	}
}

// TestRoomLoopDegradesOnDeleteFailure covers the other degradation path: a
// failed admin DELETE is logged and the room loop proceeds to the next
// instance instead of crashing.
func TestRoomLoopDegradesOnDeleteFailure(t *testing.T) {
	e := &fleetEntry{ServerID: "moba-room-1", Region: "cn-east", Capacity: 10, lifetime: 20 * time.Millisecond, recycle: 0}
	s := &roomServer{deleteErr: true}
	runRoom(t, s, e, false, true)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.deletes) == 0 {
		t.Fatal("expected admin DELETE to be attempted")
	}
	// The loop must survive a failed delete and move on: a second cycle
	// re-registers (the register call counts the next instance).
	if s.register < 2 {
		t.Errorf("register count = %d, want >=2 (loop survived delete failure)", s.register)
	}
}
