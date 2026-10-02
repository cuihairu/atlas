// Package mysql tests: the shared store contract runs against a real MySQL
// when ATLAS_TEST_MYSQL_DSN is set (CI wires a service container; locally any
// throwaway mysql works). Without the variable the suite skips — `go test
// ./...` stays dependency-free.
package mysql

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
	"github.com/cuihairu/atlas/internal/store/storetest"
)

// openTestStore drops every table in the test database and applies the real
// *_mysql.sql migration files, then returns a Store wired to an in-memory
// runtime view — mirroring the deployment's stats merge.
func openTestStore(t *testing.T) (*Store, *memory.Store) {
	t.Helper()

	dsn := os.Getenv("ATLAS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("ATLAS_TEST_MYSQL_DSN not set; skipping mysql contract")
	}
	// Migration scripts are multi-statement and the store scans DATETIME
	// into time.Time; the driver needs both flags.
	for _, param := range []string{"multiStatements=true", "parseTime=true"} {
		key := param[:strings.IndexByte(param, '=')]
		if !strings.Contains(dsn, key+"=") {
			if strings.Contains(dsn, "?") {
				dsn += "&" + param
			} else {
				dsn += "?" + param
			}
		}
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping mysql: %v", err)
	}

	ctx := context.Background()

	// Wipe: drop whatever tables exist in this database (FK order does not
	// matter with checks disabled).
	rows, err := db.QueryContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if _, err := db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
		t.Fatalf("disable fk checks: %v", err)
	}
	for _, name := range tables {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS `"+name+"`"); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	if _, err := db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=1"); err != nil {
		t.Fatalf("enable fk checks: %v", err)
	}

	files, err := filepath.Glob("../../../migrations/0*_mysql.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no mysql migration files found")
	}
	for _, f := range files {
		script, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := db.ExecContext(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}

	rt := memory.New()
	return New(db).WithRuntime(rt), rt
}

func TestContract(t *testing.T) {
	s, rt := openTestStore(t)
	storetest.Run(t, s, rt)
}

// TestStatsRuntimeMerge pins the WithRuntime wiring: heartbeat players reach
// stats (the postgres contract asserts the same from its own side).
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
	if err := rt.RecordHeartbeat(ctx, srv.ID, model.Heartbeat{Players: 3711, Load: 0.1}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	merged, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if merged.TotalPlayers != 3711 {
		t.Fatalf("TotalPlayers = %d, want 3711", merged.TotalPlayers)
	}
}
