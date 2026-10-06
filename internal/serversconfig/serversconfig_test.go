package serversconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadFlatFleet(t *testing.T) {
	path := writeConfig(t, `{
	  "servers": [
	    {"server_id": "game-1", "region": "cn-east", "endpoint": {"host": "10.0.0.1", "port": 30001}, "capacity": 500},
	    {"server_id": "game-2", "region": "cn-east", "realm_id": "realm-cn", "endpoint": {"host": "10.0.0.2", "port": 30001}}
	  ]
	}`)

	prof, name, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if name != "" {
		t.Errorf("flat fleet name = %q, want empty", name)
	}
	if len(prof.Servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(prof.Servers))
	}
	if prof.Servers[1].RealmID == nil || *prof.Servers[1].RealmID != "realm-cn" {
		t.Errorf("realm_id pointer roundtrip = %v", prof.Servers[1].RealmID)
	}
}

func TestLoadProfileSelection(t *testing.T) {
	const file = `{
	  "profile": "staging",
	  "profiles": {
	    "default":  {"servers": [{"server_id": "s-default",  "region": "r", "endpoint": {"host": "h", "port": 1}}]},
	    "staging":  {"servers": [{"server_id": "s-staging",  "region": "r", "endpoint": {"host": "h", "port": 1}}]},
	    "whatever": {"servers": [{"server_id": "s-whatever", "region": "r", "endpoint": {"host": "h", "port": 1}}]}
	  }
	}`

	cases := []struct {
		name      string
		override  string
		wantName  string
		wantFirst string
	}{
		{"file field wins when no override", "", "staging", "s-staging"},
		{"env override beats file field", "whatever", "whatever", "s-whatever"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prof, name, err := Load(writeConfig(t, file), tc.override)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if name != tc.wantName || prof.Servers[0].ID != tc.wantFirst {
				t.Errorf("selected %q/%s, want %q/%s", name, prof.Servers[0].ID, tc.wantName, tc.wantFirst)
			}
		})
	}

	t.Run("default profile fallback", func(t *testing.T) {
		noField := `{"profiles": {"default": {"servers": [{"server_id": "s", "region": "r", "endpoint": {"host": "h", "port": 1}}]}}}`
		_, name, err := Load(writeConfig(t, noField), "")
		if err != nil || name != "default" {
			t.Errorf("fallback = %q, %v; want default", name, err)
		}
	})
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		override string
		wantErr  string
	}{
		{"both fleets", `{"servers": [{"server_id": "s", "region": "r", "endpoint": {"host": "h", "port": 1}}], "profiles": {"p": {"servers": []}}}`, "", `not both`},
		{"no fleet", `{}`, "", `no "servers" and no "profiles"`},
		{"typo field", `{"servers": [{"server_id": "s", "region": "r", "endpoint": {"host": "h", "port": 1}, "capcity": 5}]}`, "", `unknown field`},
		{"malformed json", `{"servers": [`, "", `parse`},
		{"empty profiles", `{"profiles": {}}`, "", `not selected`},
		{"override without profiles", `{"servers": [{"server_id": "s", "region": "r", "endpoint": {"host": "h", "port": 1}}]}`, "prod", `nothing to select`},
		{"unknown profile picked", `{"profile": "nope", "profiles": {"alpha": {"servers": [{"server_id": "s", "region": "r", "endpoint": {"host": "h", "port": 1}}]}}}`, "", `not found`},
		{"duplicate id", `{"servers": [{"server_id": "dup", "region": "r", "endpoint": {"host": "h", "port": 1}}, {"server_id": "dup", "region": "r", "endpoint": {"host": "h", "port": 1}}]}`, "", `duplicates`},
		{"invalid entry", `{"servers": [{"server_id": "s", "region": "r", "endpoint": {"host": "h", "port": 99999}}]}`, "", `port`},
		{"missing region", `{"servers": [{"server_id": "s", "endpoint": {"host": "h", "port": 1}}]}`, "", `region`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(writeConfig(t, tc.content), tc.override)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}

	if _, _, err := Load(filepath.Join(t.TempDir(), "missing.json"), ""); err == nil ||
		!strings.Contains(err.Error(), "read servers config") {
		t.Errorf("missing file err = %v", err)
	}
}

// TestApplyLifecycle pins the ownership rules: declared servers are created
// (or refreshed without disturbing live state), undeclared config servers are
// released with their records intact, and API-owned servers are never
// touched.
func TestApplyLifecycle(t *testing.T) {
	ctx := context.Background()
	s := memory.New()

	// Pre-existing API-owned server, online with uptime.
	started := time.Now().Add(-time.Hour)
	apiOwned := &model.Server{
		ID: "api-srv", Region: "cn", Version: "1.0.0",
		Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30001},
		Status:   model.StatusOnline, StartedAt: &started, Source: "api",
	}
	if err := s.RegisterServer(ctx, apiOwned); err != nil {
		t.Fatalf("register api server: %v", err)
	}
	if err := s.UpdateServerStatus(ctx, apiOwned.ID, model.StatusOnline); err != nil {
		t.Fatalf("promote api server: %v", err)
	}

	declared := &Profile{Servers: []Server{
		{ID: "cfg-new", Region: "cn", Endpoint: Endpoint{Host: "10.0.0.1", Port: 30001}, Capacity: 100},
		{ID: "api-srv", Region: "cn", Endpoint: Endpoint{Host: "10.0.0.9", Port: 30002}, Capacity: 200},
	}}

	res, err := Apply(ctx, s, declared)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Created != 1 || res.Updated != 1 || res.Released != 0 {
		t.Fatalf("result = %+v, want 1/1/0", res)
	}

	created, err := s.GetServer(ctx, "cfg-new")
	if err != nil {
		t.Fatalf("get created: %v", err)
	}
	if created.Status != model.StatusStarting || created.Source != "config" {
		t.Errorf("created = status %q source %q, want starting/config", created.Status, created.Source)
	}

	refreshed, err := s.GetServer(ctx, "api-srv")
	if err != nil {
		t.Fatalf("get refreshed: %v", err)
	}
	if refreshed.Status != model.StatusOnline {
		t.Errorf("refreshed status = %q, want online preserved", refreshed.Status)
	}
	if refreshed.StartedAt == nil || !refreshed.StartedAt.Equal(started) {
		t.Errorf("refreshed started_at moved: %v -> %v", started, refreshed.StartedAt)
	}
	if refreshed.Source != "config" || refreshed.Capacity != 200 || refreshed.Endpoint.Port != 30002 {
		t.Errorf("refreshed profile = source %q cap %d port %d, want config/200/30002",
			refreshed.Source, refreshed.Capacity, refreshed.Endpoint.Port)
	}

	// Drop cfg-new from the declaration: it must be released, not deleted.
	res, err = Apply(ctx, s, &Profile{Servers: declared.Servers[1:2]})
	if err != nil {
		t.Fatalf("Apply release: %v", err)
	}
	if res.Released != 1 {
		t.Fatalf("released = %d, want 1", res.Released)
	}
	released, err := s.GetServer(ctx, "cfg-new")
	if err != nil {
		t.Fatalf("get released: %v", err)
	}
	if released.Source != "" || released.Status != model.StatusStarting {
		t.Errorf("released = source %q status %q, want \"\"/starting preserved", released.Source, released.Status)
	}
}

// TestApplyPaginationWalk exercises the cursor walk in the reconcile loop:
// with more servers than one page, release must visit every page. Apply
// requests pages of 200 (the store's cap), so declare 205.
func TestApplyPaginationWalk(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	const total, kept = 205, 5

	var all []Server
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("cfg-%04d", i)
		all = append(all, Server{ID: id, Region: "cn", Endpoint: Endpoint{Host: "10.0.0.1", Port: 30001}})
	}
	if _, err := Apply(ctx, s, &Profile{Servers: all}); err != nil {
		t.Fatalf("apply full: %v", err)
	}
	if got, err := s.ListServers(ctx, store.ServerFilter{Limit: 200, Cursor: "cfg-0199"}); err != nil || len(got) != 5 {
		t.Fatalf("after apply, tail page = %d servers err=%v, want 5", len(got), err)
	}

	// Keep only the last five declared; the rest get released page by page.
	res, err := Apply(ctx, s, &Profile{Servers: all[total-kept:]})
	if err != nil {
		t.Fatalf("apply shrink: %v", err)
	}
	if res.Released != total-kept {
		t.Fatalf("released = %d, want %d", res.Released, total-kept)
	}
}

// failingStore wraps a ServerStore to force errors out of Apply's paths.
type failingStore struct {
	store.ServerStore
	failGet  bool
	failReg  bool
	failList bool
}

func (f *failingStore) GetServer(ctx context.Context, id string) (*model.Server, error) {
	if f.failGet {
		return nil, fmt.Errorf("boom-get")
	}
	return f.ServerStore.GetServer(ctx, id)
}

func (f *failingStore) RegisterServer(ctx context.Context, srv *model.Server) error {
	if f.failReg {
		return fmt.Errorf("boom-register")
	}
	return f.ServerStore.RegisterServer(ctx, srv)
}

func (f *failingStore) ListServers(ctx context.Context, fl store.ServerFilter) ([]*model.Server, error) {
	if f.failList {
		return nil, fmt.Errorf("boom-list")
	}
	return f.ServerStore.ListServers(ctx, fl)
}

func TestApplyErrors(t *testing.T) {
	ctx := context.Background()
	prof := &Profile{Servers: []Server{{ID: "s1", Region: "cn", Endpoint: Endpoint{Host: "h", Port: 1}}}}

	cases := []struct {
		name string
		stub func(*failingStore)
		want string
	}{
		{"lookup failure", func(f *failingStore) { f.failGet = true }, "lookup s1"},
		{"create failure", func(f *failingStore) { f.failReg = true }, "create s1"},
		{"reconcile failure", func(f *failingStore) { f.failList = true }, "reconcile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &failingStore{ServerStore: memory.New()}
			tc.stub(f)
			if _, err := Apply(ctx, f, prof); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}
