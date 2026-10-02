// Package postgres tests: the shared store contract runs against a real
// PostgreSQL when ATLAS_TEST_DATABASE_URL is set (CI wires a service
// container; locally any throwaway postgres works). Without the variable the
// suite skips — `go test ./...` stays dependency-free.
package postgres

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
	"github.com/cuihairu/atlas/internal/store/storetest"
)

// openTestStore provisions an empty schema on the test database and applies
// the real migration files (the same ones the compose "migrate" service
// runs), then returns a Store wired to an in-memory runtime view — mirroring
// the deployment, where GetStats merges Redis heartbeat state.
func openTestStore(t *testing.T) (*Store, *memory.Store) {
	t.Helper()

	url := os.Getenv("ATLAS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ATLAS_TEST_DATABASE_URL not set; skipping postgres contract")
	}

	ctx := context.Background()

	// DDL pool: simple protocol so a whole migration file (BEGIN; …; COMMIT;)
	// runs as one multi-statement script, exactly like psql -f.
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	ddlPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(ddlPool.Close)

	if _, err := ddlPool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("wipe schema: %v", err)
	}

	files, err := filepath.Glob("../../../migrations/0*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no migration files found")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_mysql.sql") {
			continue
		}
		script, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := ddlPool.Exec(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}

	// Store under test on a normal (extended-protocol) pool.
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect store pool: %v", err)
	}
	t.Cleanup(pool.Close)

	rt := memory.New()
	return New(pool).WithRuntime(rt), rt
}

func TestContract(t *testing.T) {
	s, rt := openTestStore(t)
	storetest.Run(t, s, rt)
}

// TestStatsRuntimeMerge pins the postgres-specific WithRuntime wiring: with a
// runtime view attached, heartbeat players reach stats. The deployment wires
// Redis here (main.go); the contract suite asserts the same from the other
// side.
func TestStatsRuntimeMerge(t *testing.T) {
	s, rt := openTestStore(t)
	ctx := context.Background()

	srv := &model.Server{
		ID: "merge-srv", Region: "cn-test", Version: "1.0.0",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001}, Status: model.StatusOnline,
	}
	if err := s.RegisterServer(ctx, srv); err != nil {
		t.Fatalf("register: %v", err)
	}
	bare, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats bare: %v", err)
	}
	if bare.TotalPlayers != 0 {
		t.Fatalf("TotalPlayers without heartbeats = %d, want 0", bare.TotalPlayers)
	}

	if err := rt.RecordHeartbeat(ctx, srv.ID, model.Heartbeat{Players: 3711, Load: 0.1}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	merged, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats merged: %v", err)
	}
	if merged.TotalPlayers != 3711 {
		t.Fatalf("TotalPlayers with runtime = %d, want 3711", merged.TotalPlayers)
	}
}
