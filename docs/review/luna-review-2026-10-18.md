[0m
[0m✱ [0mGlob "**/*" [90m300 matches[0m
[0m✱ [0mGrep "TODO|FIXME|XXX|HACK" [90m109 matches[0m
[0m$ [0mgit status --short && git branch --show-current
main
[0m→ [0mRead TODO.md 
[0m→ [0mRead todo.md 
[0m→ [0mRead README.zh.md 
[0m→ [0mRead BUGS.md 
[0m$ [0mgit ls-files | grep -vE '(^docs/node_modules/|(^|/)(bin|obj|target|__pycache__)/|\.pb\.go$|third_party)' | awk 'BEGIN{n=0} {n++; if ($0 ~ /\.(go|py|ts|tsx|js|lua|md|yml|yaml|sql|proto|json|cs|cpp|hpp|sh)$/) s++} END{print "tracked files:",n,"source/docs/config:",s}' && find . -name AGENTS.md -not -path './docs/node_modules/*' -print
tracked files: 428 source/docs/config: 358
[0m→ [0mRead docs/audit-source-review-round2.md 
[0m→ [0mRead docs/audit-doc-consistency.md 
[0m→ [0mRead docs/zh/审计-文档一致性.md [90m[offset=1, limit=210][0m
[0m→ [0mRead docs/zh/审计-源码二轮.md 
[0m$ [0mgit ls-files | grep -vE '(^docs/node_modules/|(^|/)(bin|obj|target|__pycache__)/|third_party)' | awk -F/ '{print $1}' | sort | uniq -c
3 api
      1 BUGS.md
      6 cmd
      1 codecov.yml
     50 dashboard
     13 deploy
      5 deployments
      1 docker-compose.yml
      1 .dockerignore
      2 docs
      2 "docs
     69 docs
      1 .env.example
     11 examples
      7 .github
      1 .gitignore
      1 go.mod
      1 go.sum
    150 internal
      1 LICENSE
      1 Makefile
     16 migrations
      6 plugins
      1 README.md
      1 README.zh.md
     75 sdk
      1 static
      1 todo.md
      1 TODO.md
[0m→ [0mRead Makefile 
正在按源码为准复核；仓库已有审计报告只作为线索，不直接当作当前结论。当前工作区干净，我不会改文件或提交。
[0m→ [0mRead docs/zh/roadmap.md 
[0m→ [0mRead docs/zh/api.md [90m[offset=1, limit=230][0m
[0m→ [0mRead docs/zh/security.md 
[0m→ [0mRead cmd/atlas/main.go [90m[offset=130, limit=175][0m
[0m$ [0mgit ls-files '*.go' | grep -v '_test.go'
api/pb/atlas.pb.go
api/pb/atlas_grpc.pb.go
cmd/atlas-crossagent/main.go
cmd/atlas-demoagents/main.go
cmd/atlas-reshard/main.go
cmd/atlas/main.go
examples/go/main.go
internal/admin/service.go
internal/config/config.go
internal/crossserver/service.go
internal/directory/directory.go
internal/directory/events.go
internal/discovery/discovery.go
internal/event/adapter.go
internal/event/counting.go
internal/event/http/adapter.go
internal/event/kafka/adapter.go
internal/event/nats/adapter.go
internal/event/rabbitmq/adapter.go
internal/event/redis/adapter.go
internal/fleet/index.go
internal/fleet/track.go
internal/grpc/admin.go
internal/grpc/audit.go
internal/grpc/auth.go
internal/grpc/converters.go
internal/grpc/directory.go
internal/grpc/discovery.go
internal/grpc/metrics.go
internal/grpc/ratelimit.go
internal/grpc/registry.go
internal/grpc/routing.go
internal/grpc/server.go
internal/grpc/spans.go
internal/grpc/trace.go
internal/health/alert.go
internal/health/monitor.go
internal/httpapi/audit.go
internal/httpapi/auth.go
internal/httpapi/handlers.go
internal/httpapi/ratelimit.go
internal/httpapi/registry_auth.go
internal/httpapi/spans.go
internal/httpapi/tracing.go
internal/metrics/metrics.go
internal/metrics/queue.go
internal/migration/service.go
internal/model/crossserver.go
internal/model/errors.go
internal/model/model.go
internal/model/tags.go
internal/registry/registry.go
internal/routing/service.go
internal/serversconfig/serversconfig.go
internal/store/memory/indexes.go
internal/store/memory/memory.go
internal/store/memory/queue.go
internal/store/mysql/mysql.go
internal/store/postgres/postgres.go
internal/store/redisstore/redis.go
internal/store/sharded/reshard.go
internal/store/sharded/sharded.go
internal/store/sharding.go
internal/store/store.go
internal/store/storetest/contract.go
internal/telemetry/heatmap.go
internal/telemetry/telemetry.go
internal/tlsutil/tlsutil.go
internal/tracing/tracing.go
internal/tracing/tracingtest/tracingtest.go
internal/version/version.go
sdk/go/atlas/api.go
sdk/go/atlas/client.go
sdk/go/atlas/config.go
sdk/go/atlas/grpc.go
sdk/go/atlas/heartbeat.go
sdk/go/atlas/rest.go
sdk/go/atlas/types.go
[0m$ [0mgit ls-files '*.md' | grep -vE '(^docs/node_modules/|package-lock)'
BUGS.md
README.md
README.zh.md
TODO.md
dashboard/README.md
deploy/k8s/README.md
docs/api-quickstart.md
docs/api.md
docs/apisix.md
docs/architecture.md
docs/audit-doc-consistency.md
docs/audit-source-review-round2.md
docs/benchmarks.md
docs/concepts.md
docs/config-center.md
docs/data-model.md
docs/ha.md
docs/index.md
docs/lifecycle.md
docs/migration.md
docs/operations.md
docs/performance.md
docs/realms-shards.md
docs/roadmap.md
docs/scenarios.md
docs/sdk-cpp.md
docs/sdk-csharp.md
docs/sdk-go.md
docs/sdk-java.md
docs/sdk-js.md
docs/sdk-python.md
docs/security-audit.md
docs/security.md
docs/server-config.md
docs/sync.md
docs/topology.md
docs/zh/api-quickstart.md
docs/zh/api.md
docs/zh/apisix.md
docs/zh/architecture.md
docs/zh/benchmarks.md
docs/zh/concepts.md
docs/zh/config-center.md
docs/zh/data-model.md
docs/zh/ha.md
docs/zh/index.md
docs/zh/lifecycle.md
docs/zh/migration.md
docs/zh/operations.md
docs/zh/performance.md
docs/zh/realms-shards.md
docs/zh/roadmap.md
docs/zh/scenarios.md
docs/zh/sdk-cpp.md
docs/zh/sdk-csharp.md
docs/zh/sdk-go.md
docs/zh/sdk-java.md
docs/zh/sdk-js.md
docs/zh/sdk-python.md
docs/zh/security-audit.md
docs/zh/security.md
docs/zh/server-config.md
docs/zh/sync.md
docs/zh/topology.md
"docs/zh/\345\256\241\350\256\241-\346\226\207\346\241\243\344\270\200\350\207\264\346\200\247.md"
"docs/zh/\345\256\241\350\256\241-\346\272\220\347\240\201\344\272\214\350\275\256.md"
plugins/apisix/README.md
sdk/cpp/README.md
sdk/cpp/third_party/VENDORED.md
sdk/csharp/README.md
sdk/java/README.md
sdk/js/README.md
sdk/python/README.md
todo.md
[0m→ [0mRead internal/health/monitor.go 
[0m→ [0mRead internal/health/alert.go 
[0m→ [0mRead internal/directory/events.go 
[0m→ [0mRead README.md 
[0m→ [0mRead dashboard/package.json 
[0m→ [0mRead docs/package.json 
[0m→ [0mRead internal/event/kafka/adapter.go [90m[offset=180, limit=100][0m
[0m→ [0mRead internal/event/nats/adapter.go [90m[offset=185, limit=80][0m
[0m✱ [0mGrep "func \(.*UpdateServerStatus|UpdateServerStatus\(" [90min internal/store · 12 matches[0m
[0m✱ [0mGrep "MarkMaintenanceWindowApplied|mark.*applied|failed to mark|maintenance window" [90min internal/health · 13 matches[0m
[0m✱ [0mGrep "func \(.*(CreateMigration|RollbackMigration|CreateMaintenanceWindow)" [90min internal/migration · 2 matches[0m
[0m✱ [0mGrep "^\s*rpc " [90min api/proto · 23 matches[0m
[0m$ [0mgit ls-files static/logo.svg docs/public/logo.svg; nl -ba README.zh.md | sed -n '118,130p'; nl -ba README.md | sed -n '118,130p'; nl -ba internal/health/monitor.go | sed -n '297,330p'; nl -ba internal/health/monitor.go | sed -n '262,271p'
docs/public/logo.svg
static/logo.svg
   118	## Quick Start
   119	
   120	```bash
   121	# Run with in-memory store (no dependencies)
   122	
   123	> [English](README.md) | **中文**
   124	make run
   125	
   126	# Run tests
   127	make test
   128	```
   129	
   130	## Docker 快速搭建
   118	## Quick Start
   119	
   120	```bash
   121	# Run with in-memory store (no dependencies)
   122	
   123	> **English** | [中文](README.zh.md)
   124	make run
   125	
   126	# Run tests
   127	make test
   128	```
   129	
   130	## Quick Start with Docker
   297		for _, w := range windows {
   298			srv := byID[w.ServerID]
   299			switch {
   300			case !now.Before(w.EndAt):
   301				// Window over: restore, then drop the window.
   302				if srv != nil && w.PreviousStatus != "" && srv.Status == model.StatusMaintenance {
   303					if err := m.transition(ctx, srv, w.PreviousStatus,
   304						"maintenance window %s ended, restoring status", w.ID); err != nil {
   305						m.logger.Error("failed to exit maintenance window",
   306							"window_id", w.ID, "server_id", w.ServerID, "error", err)
   307						continue // keep the window so the restore can be retried
   308					}
   309				}
   310				if err := m.store.DeleteMaintenanceWindow(ctx, w.ID); err != nil {
   311					m.logger.Warn("failed to delete expired maintenance window",
   312						"window_id", w.ID, "error", err)
   313				}
   314			case w.Active(now) && w.PreviousStatus == "":
   315				// Window open, not applied yet.
   316				previous := model.ServerStatus("")
   317				if srv != nil && srv.Status.AutoManaged() {
   318					previous = srv.Status
   319					if err := m.transition(ctx, srv, model.StatusMaintenance,
   320						"maintenance window %s started", w.ID); err != nil {
   321						m.logger.Error("failed to enter maintenance window",
   322							"window_id", w.ID, "server_id", w.ServerID, "error", err)
   323						continue // unapplied: retry next sweep
   324					}
   325				}
   326				if err := m.store.MarkMaintenanceWindowApplied(ctx, w.ID, previous); err != nil {
   327					m.logger.Error("failed to mark maintenance window applied",
   328						"window_id", w.ID, "error", err)
   329				}
   330			}
   262	func (m *Monitor) transition(ctx context.Context, srv *model.Server, newStatus model.ServerStatus, reasonFmt string, args ...any) error {
   263		m.logger.Warn(fmt.Sprintf(reasonFmt, args...),
   264			"server_id", srv.ID,
   265			"from", srv.Status,
   266			"to", newStatus,
   267		)
   268		m.metrics.CountHealthTransition(srv.Status, newStatus)
   269		srv.Status = newStatus // keep the in-memory copy fresh for alert counting
   270		return m.store.UpdateServerStatus(ctx, srv.ID, newStatus)
   271	}
[0m$ [0mgo test ./...
go: downloading github.com/nats-io/nats-server/v2 v2.15.1
go: downloading github.com/prometheus/client_golang v1.25.0
go: downloading github.com/klauspost/compress v1.20.1
go: downloading github.com/prometheus/common v0.72.0
go: downloading github.com/prometheus/procfs v0.22.0
?   	github.com/cuihairu/atlas/api/pb	[no test files]
ok  	github.com/cuihairu/atlas/cmd/atlas	0.027s
?   	github.com/cuihairu/atlas/cmd/atlas-crossagent	[no test files]
ok  	github.com/cuihairu/atlas/cmd/atlas-demoagents	0.017s
?   	github.com/cuihairu/atlas/cmd/atlas-reshard	[no test files]
?   	github.com/cuihairu/atlas/examples/go	[no test files]
ok  	github.com/cuihairu/atlas/internal/admin	0.020s
ok  	github.com/cuihairu/atlas/internal/config	0.009s
ok  	github.com/cuihairu/atlas/internal/crossserver	0.026s
ok  	github.com/cuihairu/atlas/internal/directory	0.033s
ok  	github.com/cuihairu/atlas/internal/discovery	0.022s
ok  	github.com/cuihairu/atlas/internal/event	0.012s
ok  	github.com/cuihairu/atlas/internal/event/http	0.005s
ok  	github.com/cuihairu/atlas/internal/event/kafka	0.737s
ok  	github.com/cuihairu/atlas/internal/event/nats	20.345s
ok  	github.com/cuihairu/atlas/internal/event/rabbitmq	0.035s
ok  	github.com/cuihairu/atlas/internal/event/redis	3.771s
ok  	github.com/cuihairu/atlas/internal/fleet	0.016s
ok  	github.com/cuihairu/atlas/internal/grpc	0.101s
ok  	github.com/cuihairu/atlas/internal/health	0.111s
ok  	github.com/cuihairu/atlas/internal/httpapi	0.419s
ok  	github.com/cuihairu/atlas/internal/metrics	0.068s
ok  	github.com/cuihairu/atlas/internal/migration	0.009s
ok  	github.com/cuihairu/atlas/internal/model	0.014s
ok  	github.com/cuihairu/atlas/internal/registry	0.035s
ok  	github.com/cuihairu/atlas/internal/routing	0.057s
ok  	github.com/cuihairu/atlas/internal/serversconfig	0.020s
?   	github.com/cuihairu/atlas/internal/store	[no test files]
ok  	github.com/cuihairu/atlas/internal/store/memory	0.056s
ok  	github.com/cuihairu/atlas/internal/store/mysql	0.008s
ok  	github.com/cuihairu/atlas/internal/store/postgres	0.010s
ok  	github.com/cuihairu/atlas/internal/store/redisstore	0.028s
ok  	github.com/cuihairu/atlas/internal/store/sharded	0.045s
?   	github.com/cuihairu/atlas/internal/store/storetest	[no test files]
ok  	github.com/cuihairu/atlas/internal/telemetry	0.037s
ok  	github.com/cuihairu/atlas/internal/tlsutil	0.036s
ok  	github.com/cuihairu/atlas/internal/tracing	0.017s
?   	github.com/cuihairu/atlas/internal/tracing/tracingtest	[no test files]
ok  	github.com/cuihairu/atlas/internal/version	0.022s
ok  	github.com/cuihairu/atlas/sdk/go/atlas	0.211s
[0m→ [0mRead internal/store/store.go [90m[offset=195, limit=50][0m
[0m→ [0mRead docs/zh/lifecycle.md [90m[offset=1, limit=250][0m
[0m→ [0mRead internal/config/config.go [90m[offset=240, limit=115][0m
[0m$ [0mnl -ba internal/health/monitor_test.go | sed -n '1,300p'; nl -ba internal/health/maintenance_test.go | sed -n '1,320p'
1	package health
     2	
     3	import (
     4		"context"
     5		"log/slog"
     6		"os"
     7		"testing"
     8		"time"
     9	
    10		"github.com/cuihairu/atlas/internal/model"
    11		"github.com/cuihairu/atlas/internal/store"
    12		"github.com/cuihairu/atlas/internal/store/memory"
    13	)
    14	
    15	func newTestMonitor(mem store.Store, suspectAfter, offlineAfter time.Duration) *Monitor {
    16		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
    17		return New(mem, suspectAfter, offlineAfter, time.Hour, logger) // interval doesn't matter for sweep tests
    18	}
    19	
    20	func registerServer(t *testing.T, ctx context.Context, mem *memory.Store, id string, status model.ServerStatus) {
    21		t.Helper()
    22		srv := &model.Server{
    23			ID:       id,
    24			Name:     id,
    25			Type:     "game",
    26			Region:   "cn-east",
    27			Version:  "1.0.0",
    28			Platform: "android",
    29			Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
    30			Capacity: 2000,
    31			Status:   status,
    32		}
    33		if err := mem.RegisterServer(ctx, srv); err != nil {
    34			t.Fatalf("register server %s: %v", id, err)
    35		}
    36	}
    37	
    38	func recordHeartbeat(t *testing.T, ctx context.Context, mem *memory.Store, id string, players int) {
    39		t.Helper()
    40		hb := model.Heartbeat{Players: players, Load: 0.5, Status: model.StatusOnline}
    41		if err := mem.RecordHeartbeat(ctx, id, hb); err != nil {
    42			t.Fatalf("record heartbeat for %s: %v", id, err)
    43		}
    44	}
    45	
    46	func setRuntimeAge(mem *memory.Store, id string, age time.Duration) {
    47		// The memory store uses time.Now() internally, so we need to work around
    48		// this. We'll delete and re-add with a synthetic last-seen-at.
    49		// Actually, the memory store sets LastSeenAt = time.Now() on RecordHeartbeat.
    50		// For testing, we can manipulate the runtime by writing directly through the store.
    51		// The cleanest approach: just test with the monitor's now() function.
    52		//
    53		// Since we control `now`, we set it far in the future relative to the
    54		// heartbeat's LastSeenAt (which is time.Now() at record time).
    55	}
    56	
    57	func TestSweep_OnlineToSuspect(t *testing.T) {
    58		mem := memory.New()
    59		ctx := context.Background()
    60		suspectAfter := 30 * time.Second
    61		offlineAfter := 60 * time.Second
    62	
    63		mon := newTestMonitor(mem, suspectAfter, offlineAfter)
    64	
    65		registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
    66		recordHeartbeat(t, ctx, mem, "srv-1", 100)
    67	
    68		// Set now to 35 seconds in the future (past suspect threshold).
    69		heartbeatTime := time.Now()
    70		mon.now = func() time.Time { return heartbeatTime.Add(35 * time.Second) }
    71	
    72		if err := mon.sweep(ctx); err != nil {
    73			t.Fatalf("sweep: %v", err)
    74		}
    75	
    76		srv, err := mem.GetServer(ctx, "srv-1")
    77		if err != nil {
    78			t.Fatalf("GetServer: %v", err)
    79		}
    80		if srv.Status != model.StatusSuspect {
    81			t.Errorf("expected suspect, got %s", srv.Status)
    82		}
    83	}
    84	
    85	func TestSweep_OnlineToOffline(t *testing.T) {
    86		mem := memory.New()
    87		ctx := context.Background()
    88	
    89		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
    90	
    91		registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
    92		recordHeartbeat(t, ctx, mem, "srv-1", 100)
    93	
    94		heartbeatTime := time.Now()
    95		mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
    96	
    97		if err := mon.sweep(ctx); err != nil {
    98			t.Fatalf("sweep: %v", err)
    99		}
   100	
   101		srv, err := mem.GetServer(ctx, "srv-1")
   102		if err != nil {
   103			t.Fatalf("GetServer: %v", err)
   104		}
   105		if srv.Status != model.StatusOffline {
   106			t.Errorf("expected offline, got %s", srv.Status)
   107		}
   108	}
   109	
   110	func TestSweep_SuspectRecovered(t *testing.T) {
   111		mem := memory.New()
   112		ctx := context.Background()
   113	
   114		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
   115	
   116		registerServer(t, ctx, mem, "srv-1", model.StatusSuspect)
   117		// Record a recent heartbeat (only 5 seconds ago).
   118		recordHeartbeat(t, ctx, mem, "srv-1", 100)
   119	
   120		heartbeatTime := time.Now()
   121		mon.now = func() time.Time { return heartbeatTime.Add(5 * time.Second) }
   122	
   123		if err := mon.sweep(ctx); err != nil {
   124			t.Fatalf("sweep: %v", err)
   125		}
   126	
   127		srv, err := mem.GetServer(ctx, "srv-1")
   128		if err != nil {
   129			t.Fatalf("GetServer: %v", err)
   130		}
   131		if srv.Status != model.StatusOnline {
   132			t.Errorf("expected online (recovered), got %s", srv.Status)
   133		}
   134	}
   135	
   136	func TestSweep_SuspectToOffline(t *testing.T) {
   137		mem := memory.New()
   138		ctx := context.Background()
   139	
   140		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
   141	
   142		registerServer(t, ctx, mem, "srv-1", model.StatusSuspect)
   143		recordHeartbeat(t, ctx, mem, "srv-1", 100)
   144	
   145		heartbeatTime := time.Now()
   146		mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
   147	
   148		if err := mon.sweep(ctx); err != nil {
   149			t.Fatalf("sweep: %v", err)
   150		}
   151	
   152		srv, err := mem.GetServer(ctx, "srv-1")
   153		if err != nil {
   154			t.Fatalf("GetServer: %v", err)
   155		}
   156		if srv.Status != model.StatusOffline {
   157			t.Errorf("expected offline, got %s", srv.Status)
   158		}
   159	}
   160	
   161	func TestSweep_MaintenanceNotTouched(t *testing.T) {
   162		mem := memory.New()
   163		ctx := context.Background()
   164	
   165		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
   166	
   167		registerServer(t, ctx, mem, "srv-1", model.StatusMaintenance)
   168		recordHeartbeat(t, ctx, mem, "srv-1", 100)
   169	
   170		heartbeatTime := time.Now()
   171		mon.now = func() time.Time { return heartbeatTime.Add(5 * time.Minute) }
   172	
   173		if err := mon.sweep(ctx); err != nil {
   174			t.Fatalf("sweep: %v", err)
   175		}
   176	
   177		srv, err := mem.GetServer(ctx, "srv-1")
   178		if err != nil {
   179			t.Fatalf("GetServer: %v", err)
   180		}
   181		if srv.Status != model.StatusMaintenance {
   182			t.Errorf("expected maintenance (unchanged), got %s", srv.Status)
   183		}
   184	}
   185	
   186	func TestSweep_DisabledNotTouched(t *testing.T) {
   187		mem := memory.New()
   188		ctx := context.Background()
   189	
   190		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
   191	
   192		registerServer(t, ctx, mem, "srv-1", model.StatusDisabled)
   193		recordHeartbeat(t, ctx, mem, "srv-1", 100)
   194	
   195		heartbeatTime := time.Now()
   196		mon.now = func() time.Time { return heartbeatTime.Add(5 * time.Minute) }
   197	
   198		if err := mon.sweep(ctx); err != nil {
   199			t.Fatalf("sweep: %v", err)
   200		}
   201	
   202		srv, err := mem.GetServer(ctx, "srv-1")
   203		if err != nil {
   204			t.Fatalf("GetServer: %v", err)
   205		}
   206		if srv.Status != model.StatusDisabled {
   207			t.Errorf("expected disabled (unchanged), got %s", srv.Status)
   208		}
   209	}
     1	package health
     2	
     3	import (
     4		"context"
     5		"testing"
     6		"time"
     7	
     8		"github.com/cuihairu/atlas/internal/model"
     9		"github.com/cuihairu/atlas/internal/store/memory"
    10	)
    11	
    12	// applyWindowAt runs one sweep with the monitor clock pinned at now.
    13	func applyWindowAt(t *testing.T, mon *Monitor, ctx context.Context, at time.Time) {
    14		t.Helper()
    15		mon.now = func() time.Time { return at }
    16		if err := mon.sweep(ctx); err != nil {
    17			t.Fatalf("sweep: %v", err)
    18		}
    19	}
    20	
    21	func statusOf(t *testing.T, ctx context.Context, mem *memory.Store, id string) model.ServerStatus {
    22		t.Helper()
    23		srv, err := mem.GetServer(ctx, id)
    24		if err != nil {
    25			t.Fatalf("GetServer %s: %v", id, err)
    26		}
    27		return srv.Status
    28	}
    29	
    30	func TestMaintenanceWindow_AppliesAndRestores(t *testing.T) {
    31		mem := memory.New()
    32		ctx := context.Background()
    33		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
    34	
    35		registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
    36	
    37		base := time.Now()
    38		win := &model.MaintenanceWindow{
    39			ID:       "mwin-1",
    40			ServerID: "srv-1",
    41			StartAt:  base.Add(-time.Minute),
    42			EndAt:    base.Add(time.Hour),
    43		}
    44		if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
    45			t.Fatalf("create window: %v", err)
    46		}
    47	
    48		// Window open → server moves to maintenance, previous status recorded.
    49		applyWindowAt(t, mon, ctx, base)
    50		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
    51			t.Fatalf("in-window status = %s, want maintenance", got)
    52		}
    53		w, _ := mem.GetMaintenanceWindow(ctx, "mwin-1")
    54		if w.PreviousStatus != model.StatusOnline {
    55			t.Fatalf("previous status = %q, want online", w.PreviousStatus)
    56		}
    57	
    58		// Window over → status restored, window deleted.
    59		applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
    60		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
    61			t.Fatalf("post-window status = %s, want online (restored)", got)
    62		}
    63		if _, err := mem.GetMaintenanceWindow(ctx, "mwin-1"); err == nil {
    64			t.Fatal("expired window should be deleted")
    65		}
    66	}
    67	
    68	func TestMaintenanceWindow_ScheduledNotAppliedEarly(t *testing.T) {
    69		mem := memory.New()
    70		ctx := context.Background()
    71		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
    72	
    73		registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
    74	
    75		base := time.Now()
    76		win := &model.MaintenanceWindow{
    77			ID:       "mwin-future",
    78			ServerID: "srv-1",
    79			StartAt:  base.Add(time.Hour),
    80			EndAt:    base.Add(2 * time.Hour),
    81		}
    82		if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
    83			t.Fatalf("create window: %v", err)
    84		}
    85	
    86		// Before the window opens: nothing happens, window untouched.
    87		applyWindowAt(t, mon, ctx, base)
    88		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
    89			t.Fatalf("status = %s, want online before window opens", got)
    90		}
    91		if _, err := mem.GetMaintenanceWindow(ctx, "mwin-future"); err != nil {
    92			t.Fatalf("scheduled window should still exist: %v", err)
    93		}
    94	
    95		// Opening the window applies it.
    96		applyWindowAt(t, mon, ctx, base.Add(time.Hour+time.Minute))
    97		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
    98			t.Fatalf("status = %s, want maintenance once open", got)
    99		}
   100	}
   101	
   102	func TestMaintenanceWindow_OperatorMoveWins(t *testing.T) {
   103		mem := memory.New()
   104		ctx := context.Background()
   105		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
   106	
   107		registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
   108	
   109		base := time.Now()
   110		win := &model.MaintenanceWindow{
   111			ID:       "mwin-1",
   112			ServerID: "srv-1",
   113			StartAt:  base.Add(-time.Minute),
   114			EndAt:    base.Add(time.Hour),
   115		}
   116		if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
   117			t.Fatalf("create window: %v", err)
   118		}
   119	
   120		// Enter the window…
   121		applyWindowAt(t, mon, ctx, base)
   122		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
   123			t.Fatalf("in-window status = %s, want maintenance", got)
   124		}
   125	
   126		// …then an operator takes the server offline mid-window.
   127		if err := mem.UpdateServerStatus(ctx, "srv-1", model.StatusOffline); err != nil {
   128			t.Fatalf("operator disable: %v", err)
   129		}
   130	
   131		// Window end must NOT resurrect the server.
   132		applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
   133		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOffline {
   134			t.Fatalf("post-window status = %s, want offline (operator move wins)", got)
   135		}
   136	}
   137	
   138	func TestMaintenanceWindow_NonAutoManagedSkipped(t *testing.T) {
   139		mem := memory.New()
   140		ctx := context.Background()
   141		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
   142	
   143		// A draining server and a disabled server with windows open.
   144		registerServer(t, ctx, mem, "srv-drain", model.StatusDraining)
   145		registerServer(t, ctx, mem, "srv-disabled", model.StatusDisabled)
   146	
   147		base := time.Now()
   148		for i, id := range []string{"srv-drain", "srv-disabled"} {
   149			win := &model.MaintenanceWindow{
   150				ID:       "mwin-" + id,
   151				ServerID: id,
   152				StartAt:  base.Add(-time.Minute),
   153				EndAt:    base.Add(time.Hour),
   154			}
   155			if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
   156				t.Fatalf("create window %d: %v", i, err)
   157			}
   158		}
   159	
   160		applyWindowAt(t, mon, ctx, base)
   161	
   162		if got := statusOf(t, ctx, mem, "srv-drain"); got != model.StatusDraining {
   163			t.Errorf("draining server = %s, want draining (operator-owned)", got)
   164		}
   165		if got := statusOf(t, ctx, mem, "srv-disabled"); got != model.StatusDisabled {
   166			t.Errorf("disabled server = %s, want disabled (operator-owned)", got)
   167		}
   168		// Windows are consumed either way (no per-sweep retries).
   169		for _, id := range []string{"srv-drain", "srv-disabled"} {
   170			w, err := mem.GetMaintenanceWindow(ctx, "mwin-"+id)
   171			if err != nil {
   172				t.Fatalf("window %s: %v", id, err)
   173			}
   174			if w.PreviousStatus != "" {
   175				t.Errorf("window %s previous = %q, want empty (nothing to restore)", id, w.PreviousStatus)
   176			}
   177		}
   178	}
[0m$ [0mnl -ba internal/health/monitor.go | sed -n '132,180p'; nl -ba internal/health/monitor.go | sed -n '193,200p'; nl -ba internal/health/monitor.go | sed -n '236,271p'; nl -ba docs/zh/roadmap.md | sed -n '45,61p'; nl -ba README.zh.md | sed -n '118,130p'
132		for _, srv := range autoManaged {
   133			rt, ok := rtMap[srv.ID]
   134			if !ok {
   135				// No runtime data; this server has never sent a heartbeat.
   136				// If it's been starting for too long, mark offline.
   137				if srv.Status == model.StatusStarting {
   138					age := now.Sub(srv.CreatedAt)
   139					m.metrics.ObserveHeartbeatLag(age)
   140					if age > m.offlineAfter {
   141						if err := m.transition(ctx, srv, model.StatusOffline, "no heartbeat since creation (age %v)", age); err == nil {
   142							m.alertPermanentOffline(ctx, srv, age)
   143						} else {
   144							m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   145						}
   146					}
   147				}
   148				continue
   149			}
   150	
   151			age := now.Sub(rt.LastSeenAt)
   152			m.metrics.ObserveHeartbeatLag(age)
   153	
   154			switch srv.Status {
   155			case model.StatusStarting, model.StatusOnline:
   156				if age > m.offlineAfter {
   157					if err := m.transition(ctx, srv, model.StatusOffline, "heartbeat age %v exceeds offline threshold %v", age, m.offlineAfter); err == nil {
   158						m.alertPermanentOffline(ctx, srv, age)
   159					} else {
   160						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   161					}
   162				} else if age > m.suspectAfter {
   163					if err := m.transition(ctx, srv, model.StatusSuspect, "heartbeat age %v exceeds suspect threshold %v", age, m.suspectAfter); err != nil {
   164						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   165					}
   166				}
   167	
   168			case model.StatusSuspect:
   169				if age > m.offlineAfter {
   170					if err := m.transition(ctx, srv, model.StatusOffline, "heartbeat age %v exceeds offline threshold %v", age, m.offlineAfter); err == nil {
   171						m.alertPermanentOffline(ctx, srv, age)
   172					} else {
   173						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   174					}
   175				} else if age <= m.suspectAfter {
   176					// Heartbeat recovered.
   177					if err := m.transition(ctx, srv, model.StatusOnline, "heartbeat recovered (age %v)", age); err != nil {
   178						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   179					}
   180				}
   193		// Fleet-level alerting on the post-sweep status distribution.
   194		if m.alerter != nil {
   195			total, suspect, offline := countStatuses(servers)
   196			m.alerter.Evaluate(ctx, total, suspect, offline)
   197		}
   198	
   199		return nil
   200	}
   236	// alertPermanentOffline reports a permanent registration that just left the
   237	// auto-managed rotation for offline (docs/lifecycle.md §3 离线告警). The
   238	// monitor only drives offline transitions for auto-managed statuses
   239	// (starting / online / suspect) — operator-set maintenance never reaches
   240	// this path, which is the 维修状态豁免 the alert contract requires.
   241	func (m *Monitor) alertPermanentOffline(ctx context.Context, srv *model.Server, age time.Duration) {
   242		if m.alerter == nil {
   243			return
   244		}
   245		kind := srv.RegKind
   246		if kind == "" {
   247			kind = model.RegPermanent // legacy rows behave as permanent
   248		}
   249		if kind == model.RegTemporary {
   250			// A temporary registration leaving the fleet is expected lifecycle
   251			// (TTL reclaim) — only permanent registrations alert on offline.
   252			return
   253		}
   254		m.alerter.EmitServerOffline(ctx, ServerOfflineEvent{
   255			ServerID:    srv.ID,
   256			RegKind:     kind,
   257			OfflineFor:  age,
   258			Maintenance: false, // maintenance is operator-set and never transitions here
   259		})
   260	}
   261	
   262	func (m *Monitor) transition(ctx context.Context, srv *model.Server, newStatus model.ServerStatus, reasonFmt string, args ...any) error {
   263		m.logger.Warn(fmt.Sprintf(reasonFmt, args...),
   264			"server_id", srv.ID,
   265			"from", srv.Status,
   266			"to", newStatus,
   267		)
   268		m.metrics.CountHealthTransition(srv.Status, newStatus)
   269		srv.Status = newStatus // keep the in-memory copy fresh for alert counting
   270		return m.store.UpdateServerStatus(ctx, srv.ID, newStatus)
   271	}
    45	| ✅ Admin 生命周期操作 | maintenance / drain / enable / disable | v0.1.x |
    46	| ✅ 事件同步 | EventAdapter 抽象 + HTTP / Redis Streams / Kafka / NATS(JetStream) / RabbitMQ | v0.1.2 / v0.1.12 |
    47	| ✅ Prometheus 指标 | :8082 `/metrics`（Grafana 为外接指引，见 topology.md——仓库内无看板实体） | v0.1.3 |
    48	| ✅ Routing 接入推荐 | `GET /v1/routing/recommended`，同账号角色粘滞 | v0.1.x |
    49	| ✅ gRPC 双传输 | 5 服务 23 RPC，:9090（核心面与 REST 同源） | v0.1.5 |
    50	| ✅ 六语言 SDK | Go(双传输) / C++ / Python(同步异步) / JS/TS / Java / C#，自动心跳内置 | v0.1.6 ~ v0.1.11 |
    51	| ✅ APISIX 插件 | atlas-auth 玩家 token 鉴权注入 + atlas-ratelimit 端点组限流 | v0.1.13 |
    52	| ✅ Realm / Shard 管理 | Admin CRUD + 三存储实现 + 索引迁移 | v0.1.14 |
    53	| ✅ 健康告警 | suspect / offline 占比阈值 + webhook 通知（锁存语义） | v0.1.15 |
    54	| ✅ 角色索引分片 | account-hash 分片 + 跨分片查询 + 重切工具 | v0.1.16 |
    55	| ✅ 安全加固 | Registry mTLS、Admin RBAC + 审计（证书指纹）、令牌桶限流、三端口安全域 | v0.1.17 |
    56	| ✅ Dashboard 增强 | 实时服务器地图、玩家趋势、迁移进度、主题切换、中英 i18n、服务器负载热力图（REST + gRPC + dash 页，2026-10-12 交付） | v0.1.18 |
    57	| ✅ 高可用 | Redis 哨兵/集群、PG 连接池调优、HAProxy 心跳扇入、主从复制指南 | v0.1.19 |
    58	| ✅ 管理面：维护窗口与公告 | 服务器元数据、计划维护窗口（定时自动进出）、公告系统（三级严重度） | v0.1.20 |
    59	| ✅ 质量基建 | service/handler 覆盖率 >80% 目标（CI 报告 + Codecov 上报，未作失败门禁）、性能基准、Dependabot、周期安全审计 CI | v0.1.x |
    60	
    61	能力怎么用、什么场景用，见 [README 使用场景](https://github.com/cuihairu/atlas#使用场景) 与 [公告与计划维护](operations.md)。
   118	## Quick Start
   119	
   120	```bash
   121	# Run with in-memory store (no dependencies)
   122	
   123	> [English](README.md) | **中文**
   124	make run
   125	
   126	# Run tests
   127	make test
   128	```
   129	
   130	## Docker 快速搭建
[0m✱ [0mGrep "TODO v0\.1\.|TODO 观测|FIXME|TODO\(" [90min . · 59 matches[0m
[0m→ [0mRead docs/zh/sdk-go.md [90m[offset=1, limit=145][0m
[0m✱ [0mGrep "DeleteRealm|UpdateRealm|DeleteShard|UpdateShard|CreateRealm|ListRealms" [90min internal · 60 matches[0m
[0m→ [0mRead docs/zh/realms-shards.md [90m[offset=1, limit=180][0m
[0m$ [0mcd dashboard && npm test -- --reporter=dot && npm run build
> dashboard@0.0.0 test
> vitest run --reporter=dot

vite.config.ts (1:304) [33m[UNRESOLVED_IMPORT] [0mCould not resolve '@vitejs/plugin-react' in vite.config.ts
 - Module not found, treating it as an external dependency in vite.config.ts at 304..326

vite.config.ts (2:29) [33m[UNRESOLVED_IMPORT] [0mCould not resolve 'vitest/config' in vite.config.ts
   [38;5;246m╭[0m[38;5;246m─[0m[38;5;246m[[0m vite.config.ts:2:30 [38;5;246m][0m
   [38;5;246m│[0m
 [38;5;246m2 │[0m [38;5;249mi[0m[38;5;249mm[0m[38;5;249mp[0m[38;5;249mo[0m[38;5;249mr[0m[38;5;249mt[0m[38;5;249m [0m[38;5;249m{[0m[38;5;249m [0m[38;5;249md[0m[38;5;249me[0m[38;5;249mf[0m[38;5;249mi[0m[38;5;249mn[0m[38;5;249me[0m[38;5;249mC[0m[38;5;249mo[0m[38;5;249mn[0m[38;5;249mf[0m[38;5;249mi[0m[38;5;249mg[0m[38;5;249m [0m[38;5;249m}[0m[38;5;249m [0m[38;5;249mf[0m[38;5;249mr[0m[38;5;249mo[0m[38;5;249mm[0m[38;5;249m [0m'vitest/config'
 [38;5;240m  │[0m                              ───────┬───────  
 [38;5;240m  │[0m                                     ╰───────── Module not found, treating it as an external dependency
[38;5;246m───╯[0m

failed to load config from /home/cui/workspaces/atlas/dashboard/vite.config.ts

⎯⎯⎯⎯⎯⎯⎯ Startup Error ⎯⎯⎯⎯⎯⎯⎯⎯
Error [ERR_MODULE_NOT_FOUND]: Cannot find package '@vitejs/plugin-react' imported from /home/cui/node_modules/.vite-temp/vite.config.ts.timestamp-1792325548039-d1b756df626328.mjs
    at Object.getPackageJSONURL (node:internal/modules/package_json_reader:331:9)
    at packageResolve (node:internal/modules/esm/resolve:784:25)
    at moduleResolve (node:internal/modules/esm/resolve:873:18)
    at defaultResolve (node:internal/modules/esm/resolve:1006:11)
    at #cachedDefaultResolve (node:internal/modules/esm/loader:705:20)
    at #resolveAndMaybeBlockOnLoaderThread (node:internal/modules/esm/loader:725:38)
    at ModuleLoader.resolveSync (node:internal/modules/esm/loader:763:56)
    at #resolve (node:internal/modules/esm/loader:687:17)
    at ModuleLoader.getOrCreateModuleJob (node:internal/modules/esm/loader:607:35)
    at ModuleJob.syncLink (node:internal/modules/esm/module_job:276:33) {
  code: 'ERR_MODULE_NOT_FOUND'
}
[0m$ [0mcd docs && npm run build
> vitepress build


  vitepress v1.6.4

- building client + server bundles...

(!) Some chunks are larger than 500 kB after minification. Consider:
- Using dynamic import() to code-split the application
- Use build.rollupOptions.output.manualChunks to improve chunking: https://rollupjs.org/configuration-options/#output-manualchunks
- Adjust chunk size limit for this warning via build.chunkSizeWarningLimit.
[32m✓[0m building client + server bundles...
- rendering pages...
TypeError: Cannot read properties of undefined (reading 'length')
    at ComputedRefImpl.fn (file:///home/cui/workspaces/atlas/docs/.vitepress/.temp/app.js:4946:47)
    at refreshComputed (/home/cui/workspaces/atlas/docs/node_modules/@vue/reactivity/dist/reactivity.cjs.prod.js:407:28)
    at get value (/home/cui/workspaces/atlas/docs/node_modules/@vue/reactivity/dist/reactivity.cjs.prod.js:1613:5)
    at file:///home/cui/workspaces/atlas/docs/.vitepress/.temp/app.js:4977:39
    at renderComponentSubTree (/home/cui/workspaces/atlas/docs/node_modules/@vue/server-renderer/dist/server-renderer.cjs.prod.js:479:9)
    at renderComponentVNode (/home/cui/workspaces/atlas/docs/node_modules/@vue/server-renderer/dist/server-renderer.cjs.prod.js:426:14)
    at ssrRenderComponent (/home/cui/workspaces/atlas/docs/node_modules/@vue/server-renderer/dist/server-renderer.cjs.prod.js:104:10)
    at file:///home/cui/workspaces/atlas/docs/.vitepress/.temp/index.md.js:38:13
    at renderComponentSubTree (/home/cui/workspaces/atlas/docs/node_modules/@vue/server-renderer/dist/server-renderer.cjs.prod.js:479:9)
    at renderComponentVNode (/home/cui/workspaces/atlas/docs/node_modules/@vue/server-renderer/dist/server-renderer.cjs.prod.js:426:14)
[32m✓[0m rendering pages...
build complete in 23.28s.
[0m✗ [0m failed
[91m[1mError: [0mInvalid arguments for tool "shell":
- command: Missing key

Arguments provided:
{}

Update the arguments and call the tool again.
[0m→ [0mRead internal/migration/service.go 
[0m$ [0mgit status --short
(no output)
[0m$ [0mnl -ba internal/httpapi/handlers.go | sed -n '184,210p'; nl -ba internal/admin/service.go | sed -n '190,246p'; nl -ba docs/zh/realms-shards.md | sed -n '89,108p'; nl -ba docs/zh/migration.md | sed -n '1,130p'
184		mux.HandleFunc("GET /v1/admin/load-series", h.handleAdminLoadSeries)
   185		mux.HandleFunc("GET /v1/admin/bus-series", h.handleAdminBusSeries)
   186		// 服务器热力图: per-server load grid, one row per live server.
   187		mux.HandleFunc("GET /v1/admin/load-heatmap", h.handleAdminLoadHeatmap)
   188		mux.HandleFunc("POST /v1/admin/migrations", h.handleAdminCreateMigration)
   189		mux.HandleFunc("GET /v1/admin/migrations", h.handleAdminListMigrations)
   190		mux.HandleFunc("GET /v1/admin/migrations/{id}", h.handleAdminGetMigration)
   191		mux.HandleFunc("POST /v1/admin/migrations/{id}/rollback", h.handleAdminRollbackMigration)
   192		mux.HandleFunc("POST /v1/admin/realms", h.handleAdminCreateRealm)
   193		mux.HandleFunc("GET /v1/admin/realms", h.handleAdminListRealms)
   194		mux.HandleFunc("POST /v1/admin/shards", h.handleAdminCreateShard)
   195		mux.HandleFunc("GET /v1/admin/shards", h.handleAdminListShards)
   196		// Server tags (标记体系): GET shows the full list including internal
   197		// tags; POST upserts one tag by code; DELETE removes one by code.
   198		mux.HandleFunc("GET /v1/admin/servers/{id}/tags", h.handleAdminGetServerTags)
   199		mux.HandleFunc("POST /v1/admin/servers/{id}/tags", h.handleAdminAddServerTag)
   200		mux.HandleFunc("DELETE /v1/admin/servers/{id}/tags/{code}", h.handleAdminRemoveServerTag)
   201		// Maintenance windows & announcements (TODO v0.1.20).
   202		mux.HandleFunc("POST /v1/admin/servers/{id}/maintenance-window", h.handleAdminCreateMaintenanceWindow)
   203		// Fleet batch: one template across N servers, staggered rolling
   204		// maintenance, partial-success result, dry-run preview.
   205		mux.HandleFunc("POST /v1/admin/maintenance-windows/batch", h.handleAdminBatchCreateMaintenanceWindow)
   206		mux.HandleFunc("GET /v1/admin/maintenance-windows", h.handleAdminListMaintenanceWindows)
   207		mux.HandleFunc("DELETE /v1/admin/maintenance-windows/{id}", h.handleAdminDeleteMaintenanceWindow)
   208		mux.HandleFunc("POST /v1/admin/announcements", h.handleAdminCreateAnnouncement)
   209		mux.HandleFunc("GET /v1/admin/announcements", h.handleAdminListAnnouncements)
   210		mux.HandleFunc("DELETE /v1/admin/announcements/{id}", h.handleAdminDeleteAnnouncement)
   190	func (s *Service) ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error) {
   191		return s.migration.ListMigrations(ctx, limit)
   192	}
   193	
   194	// ── Realms & Shards (TODO v0.1.14) ──────────────────────────────
   195	
   196	// CreateRealmRequest is the DTO for creating a realm.
   197	type CreateRealmRequest struct {
   198		ID     string `json:"id"`
   199		Name   string `json:"name"`
   200		Region string `json:"region"`
   201		Status string `json:"status"`
   202	}
   203	
   204	// CreateShardRequest is the DTO for creating a shard.
   205	type CreateShardRequest struct {
   206		ID      string `json:"id"`
   207		RealmID string `json:"realm_id"`
   208		Name    string `json:"name"`
   209		Status  string `json:"status"`
   210	}
   211	
   212	// CreateRealm inserts a new realm. Name and ID are required; status defaults
   213	// to "active". Returns model.ErrConflict when the ID already exists.
   214	func (s *Service) CreateRealm(ctx context.Context, req CreateRealmRequest) (*model.Realm, error) {
   215		if req.ID == "" || req.Name == "" {
   216			return nil, fmt.Errorf("%w: realm id and name are required", model.ErrInvalid)
   217		}
   218		status := req.Status
   219		if status == "" {
   220			status = "active"
   221		}
   222		r := &model.Realm{ID: req.ID, Name: req.Name, Region: req.Region, Status: status}
   223		if err := s.store.CreateRealm(ctx, r); err != nil {
   224			return nil, fmt.Errorf("create realm %s: %w", req.ID, err)
   225		}
   226		return r, nil
   227	}
   228	
   229	// ListRealms returns realms ordered by creation time descending.
   230	func (s *Service) ListRealms(ctx context.Context, limit int) ([]*model.Realm, error) {
   231		realms, err := s.store.ListRealms(ctx, limit)
   232		if err != nil {
   233			return nil, fmt.Errorf("list realms: %w", err)
   234		}
   235		return realms, nil
   236	}
   237	
   238	// CreateShard inserts a new shard under an existing realm. Name, ID and
   239	// realm_id are required; the realm must exist (model.ErrNotFound otherwise).
   240	func (s *Service) CreateShard(ctx context.Context, req CreateShardRequest) (*model.Shard, error) {
   241		if req.ID == "" || req.Name == "" || req.RealmID == "" {
   242			return nil, fmt.Errorf("%w: shard id, realm_id and name are required", model.ErrInvalid)
   243		}
   244		if _, err := s.store.GetRealm(ctx, req.RealmID); err != nil {
   245			return nil, fmt.Errorf("create shard %s: %w", req.ID, err)
   246		}
    89	### 分服合并（合服）
    90	
    91	Shard 层面的合并由[合服迁移](/zh/migration)完成：`POST /v1/admin/migrations` 把源服务器角色
    92	索引原子迁到目标，之后源服务器下线、客户端按同一 `shard` 维度筛选即可无感。
    93	
    94	### 玩家跨服查询带大区语境
    95	
    96	`GET /v1/directory/accounts/{id}/characters` 返回的角色自带 `server_id`，配合发现列表的
    97	`realm_id` / `shard_id` 即可在客户端把"角色 → 所在大区/分服"一次渲染出来。
    98	
    99	## API 速查（:8082）
   100	
   101	| Method | Path | 说明 |
   102	| --- | --- | --- |
   103	| `POST` | `/v1/admin/realms` | 建大区；`{id, name, region, status?}`，`status` 缺省 `active`；重 ID → `409 REALM_EXISTS` |
   104	| `GET` | `/v1/admin/realms?limit=` | 大区列表（`created_at` 倒序，默认 50） |
   105	| `POST` | `/v1/admin/shards` | 建分服；`{id, name, realm_id, status?}`；大区不存在 → `404 REALM_NOT_FOUND`，重 ID → `409 SHARD_EXISTS` |
   106	| `GET` | `/v1/admin/shards?realm_id=&limit=` | 分服列表，可按大区过滤 |
   107	
   108	服务器侧只有两个弱引用字段：注册体里的 `realm_id` / `shard_id`（可后补），与发现筛选的
     1	
     2	# 合服 / 转服 / 迁服
     3	
     4	Atlas 的角色目录设计让它天然适合承载服务器之间的角色迁移。这是 Atlas 相对普通 Service Discovery 的第二大差异点。
     5	
     6	---
     7	
     8	## 1. 场景
     9	
    10	| 场景 | 含义 |
    11	| --- | --- |
    12	| 合服 | 多个服务器合并为一个 |
    13	| 转服 | 玩家把角色从一个服务器移到另一个 |
    14	| 迁服 | 服务器整体搬迁（换机房 / 换集群） |
    15	| 跨区 | 跨 Region 迁移 |
    16	
    17	四者在 Atlas 中抽象为同一件事：**角色索引的 `server_id` 从源迁移到目标**。
    18	
    19	---
    20	
    21	## 2. 合服
    22	
    23	```mermaid
    24	flowchart LR
    25	    S1["Server 1001"] -->|merge| T["Server 2001"]
    26	    S2["Server 1002"] -->|merge| T
    27	```
    28	
    29	### 角色目录的变化
    30	
    31	| 字段 | 值 |
    32	| --- | --- |
    33	| `character_id` | 123 |
    34	| `old_server` | 1001 |
    35	| `new_server` | 2001 |
    36	
    37	### 过程
    38	
    39	```mermaid
    40	flowchart TB
    41	    S1["1. 运维创建 migration 记录<br/>POST /v1/admin/migrations → status = pending"]
    42	    S2["2. 目标服务器 2001 进入 starting<br/>加载合并后的角色数据"]
    43	    S3["3. 源服务器 1001 / 1002 进入 draining"]
    44	    S4["4. 存量玩家自然离开或到达超时"]
    45	    S5["5. 源服务器角色数据导出 → 导入目标"]
    46	    S6["6. 目录索引批量变更 server_id<br/>（按 §7 蓝图由调用方执行）"]
    47	    S7["7. 校验角色数量一致<br/>（调用目录接口比对，见下）"]
    48	    S8["8. 源服务器 offline，目标 online"]
    49	    S1 --> S2 --> S3 --> S4 --> S5 --> S6 --> S7 --> S8
    50	```
    51	
    52	> **v0.1 现状**：步骤 1 是 Atlas API（只创建记录，`status = pending`）；步骤 2-5、8
    53	> 是运维与游戏服务器的动作，Atlas 不参与编排，也**不自动推进迁移状态**——
    54	> `migrating` / `verifying` / `completed` 目前没有 API 可达路径（记录会停在
    55	> `pending`，可用的唯一状态推进是 `rollback`）。步骤 6 的索引变更当前没有原子
    56	> 端点（见 §3 现状）；步骤 7 的校验由你调用目录接口完成比对。自动编排是 §7 的
    57	> 设计蓝图，尚未实现。
    58	
    59	### 校验
    60	
    61	合服最容易出问题的是**角色丢失**。Atlas 在第 7 步提供校验能力：
    62	
    63	`GET /v1/directory/servers/{server_id}/characters`（分别拉取源与目标的角色列表）
    64	
    65	比对迁移前后的角色数与 `character_id` 集合，确保：
    66	
    67	| # | 校验条件 |
    68	| --- | --- |
    69	| 1 | source 角色数 == target 中来自 source 的角色数 |
    70	| 2 | 无 `character_id` 遗漏 |
    71	| 3 | 无 `character_id` 重复 |
    72	
    73	---
    74	
    75	## 3. 转服
    76	
    77	玩家主动转移单个角色。
    78	
    79	| 字段 | 值 |
    80	| --- | --- |
    81	| `character_id` | 123 |
    82	| `source_servers` | `["1001"]` |
    83	| `target_server` | 2001 |
    84	
    85	> 说明：API 请求体使用 `source_servers` 数组（`CreateMigrationRequest.SourceServers []string`），合服/转服/迁服共用同一端点。转服时数组仅含单个源服务器。
    86	
    87	与合服的区别：转服是**单角色粒度**，合服是**服务器粒度**。但对 Atlas 而言都是 `character_index` 中 `server_id` 字段的变更。
    88	
    89	> **v0.1 现状**：目录 API 暂无原子变更 `server_id` 的端点——`PATCH
    90	> /v1/directory/characters/{id}` 只接受 `name` / `level` / `class_id` / `avatar` / `metadata`
    91	> （`server_id` 是行定位符，不是可改字段）。当前的落地路径是**建新删旧**两步：
    92	
    93	```mermaid
    94	flowchart LR
    95	    T1["1. 游戏服务器完成角色数据转移"] --> T2["2. 目标侧 POST /v1/directory/characters<br/>{account_id, server_id: 2001, character_id, ...}"] --> T3["3. 源侧 DELETE /v1/directory/characters/{id}<br/>删除旧索引行"]
    96	```
    97	
    98	两步**不是事务**：目标建行受注册门禁约束（目标处于维护中/禁止注册会被拒），建删之间同一 `character_id` 可能短暂在两侧并存。原子切换端点是规划项；数据转移本身始终由游戏服务器负责，Atlas 保证的是目录与运行时状态的一致视图。
    99	
   100	---
   101	
   102	## 4. 迁服
   103	
   104	服务器整体搬迁，`server_id` 不变，`endpoint` 变化。
   105	
   106	| 服务器 | 阶段 | endpoint |
   107	| --- | --- | --- |
   108	| `game-1001` | before | `10.0.1.21:30001` |
   109	| `game-1001` | after | `10.0.5.88:30001` |
   110	
   111	对 Atlas 只是 `servers.endpoint` 的更新，角色索引无需变更。
   112	
   113	```mermaid
   114	flowchart TB
   115	    M1["1. 目标实例以相同 server_id 注册，新 endpoint"]
   116	    M2["2. Atlas 更新 endpoint，状态置为 starting"]
   117	    M3["3. 旧实例 draining"]
   118	    M4["4. 新实例 online"]
   119	    M1 --> M2 --> M3 --> M4
   120	```
   121	
   122	---
   123	
   124	## 5. 数据模型
   125	
   126	### server_migrations
   127	
   128	```sql
   129	CREATE TABLE server_migrations (
   130	    id             TEXT PRIMARY KEY,
[0m→ [0mRead internal/store/store.go [90m[offset=125, limit=50][0m
[0m→ [0mRead internal/store/memory/memory.go [90m[offset=180, limit=42][0m
[0m→ [0mRead docs/performance.md 
[0m→ [0mRead docs/realms-shards.md 
[0m→ [0mRead docs/zh/performance.md [90m[offset=1, limit=140][0m
[0m✱ [0mGrep "DLQ|dead.?letter|dead letter|重试|retry" [90min docs/zh/sync.md · 3 matches[0m
[0m$ [0mnl -ba internal/registry/registry.go | sed -n '145,245p'; nl -ba internal/httpapi/audit.go | sed -n '1,115p'; nl -ba cmd/atlas/main.go | sed -n '520,570p'; nl -ba internal/event/nats/adapter.go | sed -n '207,235p'; nl -ba internal/event/kafka/adapter.go | sed -n '206,225p'
145		code, message := srv.RegistrationBlocked(s.maintenanceEnforce == MaintenanceEnforceWarn)
   146		return RegistrationVerdict{
   147			Code:    code,
   148			Message: message,
   149			Warn:    code == "" && s.maintenanceEnforce == MaintenanceEnforceWarn && srv.InMaintenance(),
   150		}, nil
   151	}
   152	
   153	// Register registers a game server. It is idempotent: re-registering the same
   154	// server ID updates its fields rather than returning ErrConflict.
   155	func (s *Service) Register(ctx context.Context, req RegisterRequest) (*model.Server, error) {
   156		t0 := time.Now()
   157		defer func() { s.metrics.ObserveRegistryWrite("register", time.Since(t0)) }()
   158		ctx, span := atlastracing.Start(ctx, "registry.register")
   159		defer span.End()
   160	
   161		startedAt := time.Now()
   162		if req.StartedAt != nil {
   163			startedAt = *req.StartedAt
   164		}
   165		srv := &model.Server{
   166			ID:                req.ID,
   167			Name:              req.Name,
   168			Type:              req.Type,
   169			Region:            req.Region,
   170			RealmID:           req.RealmID,
   171			ShardID:           req.ShardID,
   172			Version:           req.Version,
   173			Platform:          req.Platform,
   174			Endpoint:          req.Endpoint,
   175			Capacity:          req.Capacity,
   176			NotifyMode:        req.NotifyMode,
   177			NotifyCallbackURL: req.NotifyCallbackURL,
   178			Metadata:          req.Metadata,
   179			// An omitted marker is materialized as permanent rather than left
   180			// unset: every record written from here carries its lifecycle class
   181			// explicitly, so the expiry path never has to infer it.
   182			RegKind:       regKindOf(req),
   183			RegTTLSeconds: req.TTLSeconds,
   184			StartedAt:     &startedAt,
   185			Status:        model.StatusStarting,
   186		}
   187	
   188		if err := srv.Validate(); err != nil {
   189			return nil, err
   190		}
   191	
   192		// Config-managed servers (ATLAS_SERVERS_CONFIG) reject API registration:
   193		// their profile fields are owned by the config file, so an API update
   194		// would be silently reverted at the next Atlas restart. Heartbeats and
   195		// lifecycle operations remain the way a declared server participates.
   196		if existing, err := s.servers.GetServer(ctx, req.ID); err == nil {
   197			if existing.Source == "config" {
   198				return nil, fmt.Errorf("%w: server %s is managed by the servers config; update the declaration there (heartbeat is still accepted)", model.ErrConflict, req.ID)
   199			}
   200		} else if !errors.Is(err, model.ErrNotFound) {
   201			return nil, fmt.Errorf("lookup server %s: %w", req.ID, err)
   202		}
   203	
   204		if err := s.servers.RegisterServer(ctx, srv); err != nil {
   205			return nil, fmt.Errorf("register server: %w", err)
   206		}
   207	
   208		// Record an initial heartbeat so runtime data is available immediately.
   209		hb := model.Heartbeat{
   210			Players: req.Players,
   211			Load:    0,
   212			Status:  model.StatusStarting,
   213		}
   214		if err := s.runtime.RecordHeartbeat(ctx, srv.ID, hb); err != nil {
   215			s.logger.Warn("failed to record initial heartbeat", "server_id", srv.ID, "error", err)
   216		}
   217	
   218		s.logger.Info("server registered", "server_id", srv.ID, "region", srv.Region, "status", srv.Status)
   219		return srv, nil
   220	}
   221	
   222	// Heartbeat processes a heartbeat from a game server.
   223	// Heartbeat records a heartbeat and returns the server's effective status
   224	// afterwards, so callers can tell online from suspect / offline (an offline
   225	// server must re-register to re-enter rotation).
   226	func (s *Service) Heartbeat(ctx context.Context, serverID string, hb model.Heartbeat) (model.ServerStatus, error) {
   227		t0 := time.Now()
   228		defer func() { s.metrics.ObserveRegistryWrite("heartbeat", time.Since(t0)) }()
   229		ctx, span := atlastracing.Start(ctx, "registry.heartbeat")
   230		defer span.End()
   231	
   232		if err := hb.Validate(); err != nil {
   233			return "", err
   234		}
   235	
   236		// Verify the server exists.
   237		srv, err := s.servers.GetServer(ctx, serverID)
   238		if err != nil {
   239			return "", fmt.Errorf("heartbeat for unknown server %s: %w", serverID, err)
   240		}
   241	
   242		// Determine the status to record.
   243		status := hb.Status
   244		if status == "" {
   245			status = srv.Status
     1	// Audit logging for Admin API operations (TODO v0.1.17): every request is
     2	// recorded with actor, timestamp, method, path, response status and — for
     3	// mutating methods — the request payload as the change record. Entries go to
     4	// the structured log (slog) and to a bounded in-memory ring exposed via
     5	// GET /v1/admin/audit for ops tooling.
     6	package httpapi
     7	
     8	import (
     9		"bytes"
    10		"encoding/json"
    11		"io"
    12		"log/slog"
    13		"net/http"
    14		"sync"
    15		"time"
    16	)
    17	
    18	// AuditEntry is one recorded Admin API operation.
    19	type AuditEntry struct {
    20		Time   time.Time       `json:"time"`
    21		Actor  string          `json:"actor"` // "role:fingerprint" (see Actor.String)
    22		Method string          `json:"method"`
    23		Path   string          `json:"path"`
    24		Status int             `json:"status"`
    25		Body   json.RawMessage `json:"body,omitempty"` // mutating methods only, truncated
    26		Query  string          `json:"query,omitempty"`
    27	}
    28	
    29	// AuditLog records Admin API operations.
    30	type AuditLog struct {
    31		logger *slog.Logger
    32		max    int // ring capacity
    33	
    34		mu      sync.Mutex
    35		ring    []AuditEntry
    36		maxBody int
    37	}
    38	
    39	// NewAuditLog creates an audit log holding the most recent max entries
    40	// (<= 0 means 1000). maxBody bounds the captured request payload per entry
    41	// (<= 0 means 4096 bytes).
    42	func NewAuditLog(max, maxBody int, logger *slog.Logger) *AuditLog {
    43		if max <= 0 {
    44			max = 1000
    45		}
    46		if maxBody <= 0 {
    47			maxBody = 4096
    48		}
    49		return &AuditLog{logger: logger, max: max, maxBody: maxBody}
    50	}
    51	
    52	// auditStatusWriter captures the response status for the audit entry.
    53	type auditStatusWriter struct {
    54		http.ResponseWriter
    55		status int
    56	}
    57	
    58	func (w *auditStatusWriter) WriteHeader(code int) {
    59		w.status = code
    60		w.ResponseWriter.WriteHeader(code)
    61	}
    62	
    63	func (a *AuditLog) Middleware(next http.Handler) http.Handler {
    64		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    65			// Capture the payload of mutating requests (the change record);
    66			// reads have no diff to record.
    67			var body []byte
    68			if isMutating(r.Method) && r.Body != nil {
    69				buf, err := io.ReadAll(io.LimitReader(r.Body, int64(a.maxBody)+1))
    70				if err == nil {
    71					if len(buf) > a.maxBody {
    72						buf = buf[:a.maxBody]
    73					}
    74					body = buf
    75					r.Body = io.NopCloser(bytes.NewReader(buf))
    76				}
    77			}
    78	
    79			sw := &auditStatusWriter{ResponseWriter: w, status: http.StatusOK}
    80			next.ServeHTTP(sw, r)
    81	
    82			actor := ActorFrom(r.Context())
    83			entry := AuditEntry{
    84				Time:   time.Now().UTC(),
    85				Actor:  actor.String(),
    86				Method: r.Method,
    87				Path:   r.URL.Path,
    88				Status: sw.status,
    89				Query:  r.URL.RawQuery,
    90			}
    91			if len(body) > 0 {
    92				// Keep valid JSON as-is; anything else (broken payloads) is
    93				// wrapped as a string so the audit entry itself always marshals.
    94				if json.Valid(body) {
    95					entry.Body = json.RawMessage(body)
    96				} else {
    97					if raw, err := json.Marshal(string(body)); err == nil {
    98						entry.Body = raw
    99					}
   100				}
   101			}
   102	
   103			a.Record(entry)
   104		})
   105	}
   106	
   107	// Record appends an entry to the ring and mirrors it to the structured log.
   108	// Transport-neutral: the REST middleware and the gRPC UnaryAudit
   109	// interceptor both funnel through here, so ops tooling sees one ring and
   110	// one log vocabulary regardless of how the operation arrived.
   111	func (a *AuditLog) Record(e AuditEntry) {
   112		a.mu.Lock()
   113		a.ring = append(a.ring, e)
   114		if len(a.ring) > a.max {
   115			a.ring = a.ring[len(a.ring)-a.max:]
   520	
   521		// ── Registry API (internal network) ────────────────────
   522		// No CORS middleware here: this port serves game servers (machine
   523		// traffic), never browsers.
   524		regMux := http.NewServeMux()
   525		handler.RegisterRegistryRoutes(regMux)
   526		regMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
   527			w.WriteHeader(http.StatusOK)
   528			w.Write([]byte(`{"status":"ok"}`))
   529		})
   530		var regHandler http.Handler = http.Handler(regMux)
   531		if len(regTokens) > 0 || len(regIPs) > 0 {
   532			regHandler = httpapi.RegistryAuth(httpapi.RegistryAuthConfig{
   533				Tokens:      regTokens,
   534				IPWhitelist: regIPs,
   535				Logger:      logger,
   536			})(regHandler)
   537		}
   538		if limiter != nil {
   539			regHandler = limiter.Middleware(regHandler)
   540		}
   541		// Tracing outermost: register/heartbeat calls rejected by auth or the
   542		// rate limiter are traced too.
   543		regHandler = httpapi.Tracing(logger)(httpapi.Spans()(regHandler))
   544		regSrv := &http.Server{
   545			Addr:         cfg.RegistryAddr,
   546			Handler:      regHandler,
   547			ReadTimeout:  10 * time.Second,
   548			WriteTimeout: 10 * time.Second,
   549			IdleTimeout:  60 * time.Second,
   550		}
   551	
   552		// ── Admin API (management network) ─────────────────────
   553		adminMux := http.NewServeMux()
   554		handler.RegisterAdminRoutes(adminMux)
   555		adminMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
   556			if err := composite.Ping(r.Context()); err != nil {
   557				w.WriteHeader(http.StatusServiceUnavailable)
   558				w.Write([]byte(`{"status":"not ready","error":"` + err.Error() + `"}`))
   559				return
   560			}
   561			w.WriteHeader(http.StatusOK)
   562			w.Write([]byte(`{"status":"ok"}`))
   563		})
   564		adminMux.Handle("GET /metrics", prom.Handler())
   565	
   566		var adminHandler http.Handler = http.Handler(adminMux)
   567		// Audit inside auth (records the resolved actor), rate limit outside
   568		// (cheap per-IP rejection before any key check).
   569		if auditLog != nil {
   570			adminHandler = auditLog.Middleware(adminHandler)
   207	func (a *Adapter) handle(ctx context.Context, h event.Handler, msg *nats.Msg) {
   208		// Stream sequence doubles as the event ID (0 when metadata is absent).
   209		seq := uint64(0)
   210		if meta, err := msg.Metadata(); err == nil {
   211			seq = meta.Sequence.Stream
   212		}
   213	
   214		var e event.Event
   215		if err := json.Unmarshal(msg.Data, &e); err != nil {
   216			// Poison payload: it can never succeed, so ack it and move on.
   217			a.logger.Error("event: undecodable payload, dropping",
   218				"subject", msg.Subject, "stream_seq", seq, "error", err)
   219			_ = msg.Ack()
   220			return
   221		}
   222		e.ID = fmt.Sprintf("%d", seq)
   223	
   224		if err := h(ctx, &e); err != nil {
   225			a.logger.Error("event: handler failed, left unacked",
   226				"subject", msg.Subject, "stream_seq", seq,
   227				"type", string(e.Type), "error", err)
   228			return
   229		}
   230		if err := msg.Ack(); err != nil {
   231			a.logger.Error("event: ack failed", "subject", msg.Subject, "stream_seq", seq, "error", err)
   232		}
   233	}
   206	func (a *Adapter) handle(ctx context.Context, h event.Handler, msg kafka.Message) {
   207		var e event.Event
   208		if err := json.Unmarshal(msg.Value, &e); err != nil {
   209			// Poison payload: it can never succeed, so commit it and move on.
   210			a.logger.Error("event: undecodable payload, dropping",
   211				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
   212			a.commit(ctx, msg)
   213			return
   214		}
   215		e.ID = fmt.Sprintf("%d@%d", msg.Partition, msg.Offset)
   216	
   217		if err := h(ctx, &e); err != nil {
   218			a.logger.Error("event: handler failed, offset not committed",
   219				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset,
   220				"type", string(e.Type), "error", err)
   221			return
   222		}
   223		a.commit(ctx, msg)
   224	}
   225
[0m→ [0mRead internal/httpapi/audit.go 
[0m✱ [0mGrep "MaxDeliver|AckWait|RequeueBackoff|MaxRetry" [90min internal/event · 8 matches[0m
[0m→ [0mRead internal/registry/registry.go [90m[offset=236, limit=90][0m
[0m$ [0mnl -ba internal/health/monitor.go | sed -n '79,96p;132,164p;193,197p;262,271p;297,330p'; nl -ba internal/registry/registry.go | sed -n '195,216p;253,280p'; nl -ba internal/directory/events.go | sed -n '57,89p'; nl -ba internal/migration/service.go | sed -n '68,114p;125,141p'
79	func (m *Monitor) sweep(ctx context.Context) error {
    80		// List all servers regardless of status, cursor-paginated at the
    81		// store's list cap. A bare call takes the store's 50-row default
    82		// page: servers past it would never be checked, so they would never
    83		// transition to suspect/offline no matter how long they stay silent.
    84		var servers []*model.Server
    85		cursor := ""
    86		for {
    87			page, err := m.store.ListServers(ctx, store.ServerFilter{Limit: store.ListServersMaxLimit, Cursor: cursor})
    88			if err != nil {
    89				return fmt.Errorf("list servers: %w", err)
    90			}
    91			servers = append(servers, page...)
    92			if len(page) < store.ListServersMaxLimit {
    93				break
    94			}
    95			cursor = page[len(page)-1].ID
    96		}
   132		for _, srv := range autoManaged {
   133			rt, ok := rtMap[srv.ID]
   134			if !ok {
   135				// No runtime data; this server has never sent a heartbeat.
   136				// If it's been starting for too long, mark offline.
   137				if srv.Status == model.StatusStarting {
   138					age := now.Sub(srv.CreatedAt)
   139					m.metrics.ObserveHeartbeatLag(age)
   140					if age > m.offlineAfter {
   141						if err := m.transition(ctx, srv, model.StatusOffline, "no heartbeat since creation (age %v)", age); err == nil {
   142							m.alertPermanentOffline(ctx, srv, age)
   143						} else {
   144							m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   145						}
   146					}
   147				}
   148				continue
   149			}
   150	
   151			age := now.Sub(rt.LastSeenAt)
   152			m.metrics.ObserveHeartbeatLag(age)
   153	
   154			switch srv.Status {
   155			case model.StatusStarting, model.StatusOnline:
   156				if age > m.offlineAfter {
   157					if err := m.transition(ctx, srv, model.StatusOffline, "heartbeat age %v exceeds offline threshold %v", age, m.offlineAfter); err == nil {
   158						m.alertPermanentOffline(ctx, srv, age)
   159					} else {
   160						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   161					}
   162				} else if age > m.suspectAfter {
   163					if err := m.transition(ctx, srv, model.StatusSuspect, "heartbeat age %v exceeds suspect threshold %v", age, m.suspectAfter); err != nil {
   164						m.logger.Error("transition failed", "server_id", srv.ID, "error", err)
   193		// Fleet-level alerting on the post-sweep status distribution.
   194		if m.alerter != nil {
   195			total, suspect, offline := countStatuses(servers)
   196			m.alerter.Evaluate(ctx, total, suspect, offline)
   197		}
   262	func (m *Monitor) transition(ctx context.Context, srv *model.Server, newStatus model.ServerStatus, reasonFmt string, args ...any) error {
   263		m.logger.Warn(fmt.Sprintf(reasonFmt, args...),
   264			"server_id", srv.ID,
   265			"from", srv.Status,
   266			"to", newStatus,
   267		)
   268		m.metrics.CountHealthTransition(srv.Status, newStatus)
   269		srv.Status = newStatus // keep the in-memory copy fresh for alert counting
   270		return m.store.UpdateServerStatus(ctx, srv.ID, newStatus)
   271	}
   297		for _, w := range windows {
   298			srv := byID[w.ServerID]
   299			switch {
   300			case !now.Before(w.EndAt):
   301				// Window over: restore, then drop the window.
   302				if srv != nil && w.PreviousStatus != "" && srv.Status == model.StatusMaintenance {
   303					if err := m.transition(ctx, srv, w.PreviousStatus,
   304						"maintenance window %s ended, restoring status", w.ID); err != nil {
   305						m.logger.Error("failed to exit maintenance window",
   306							"window_id", w.ID, "server_id", w.ServerID, "error", err)
   307						continue // keep the window so the restore can be retried
   308					}
   309				}
   310				if err := m.store.DeleteMaintenanceWindow(ctx, w.ID); err != nil {
   311					m.logger.Warn("failed to delete expired maintenance window",
   312						"window_id", w.ID, "error", err)
   313				}
   314			case w.Active(now) && w.PreviousStatus == "":
   315				// Window open, not applied yet.
   316				previous := model.ServerStatus("")
   317				if srv != nil && srv.Status.AutoManaged() {
   318					previous = srv.Status
   319					if err := m.transition(ctx, srv, model.StatusMaintenance,
   320						"maintenance window %s started", w.ID); err != nil {
   321						m.logger.Error("failed to enter maintenance window",
   322							"window_id", w.ID, "server_id", w.ServerID, "error", err)
   323						continue // unapplied: retry next sweep
   324					}
   325				}
   326				if err := m.store.MarkMaintenanceWindowApplied(ctx, w.ID, previous); err != nil {
   327					m.logger.Error("failed to mark maintenance window applied",
   328						"window_id", w.ID, "error", err)
   329				}
   330			}
   195		// lifecycle operations remain the way a declared server participates.
   196		if existing, err := s.servers.GetServer(ctx, req.ID); err == nil {
   197			if existing.Source == "config" {
   198				return nil, fmt.Errorf("%w: server %s is managed by the servers config; update the declaration there (heartbeat is still accepted)", model.ErrConflict, req.ID)
   199			}
   200		} else if !errors.Is(err, model.ErrNotFound) {
   201			return nil, fmt.Errorf("lookup server %s: %w", req.ID, err)
   202		}
   203	
   204		if err := s.servers.RegisterServer(ctx, srv); err != nil {
   205			return nil, fmt.Errorf("register server: %w", err)
   206		}
   207	
   208		// Record an initial heartbeat so runtime data is available immediately.
   209		hb := model.Heartbeat{
   210			Players: req.Players,
   211			Load:    0,
   212			Status:  model.StatusStarting,
   213		}
   214		if err := s.runtime.RecordHeartbeat(ctx, srv.ID, hb); err != nil {
   215			s.logger.Warn("failed to record initial heartbeat", "server_id", srv.ID, "error", err)
   216		}
   253		if err := s.runtime.RecordHeartbeat(ctx, serverID, runtimeHB); err != nil {
   254			return "", fmt.Errorf("record heartbeat: %w", err)
   255		}
   256	
   257		// Auto-promote starting → online on first valid heartbeat.
   258		if srv.Status == model.StatusStarting {
   259			if err := s.servers.UpdateServerStatus(ctx, serverID, model.StatusOnline); err != nil {
   260				return "", fmt.Errorf("promote to online: %w", err)
   261			}
   262			s.logger.Info("server promoted to online", "server_id", serverID)
   263			return model.StatusOnline, nil
   264		}
   265	
   266		// Config-managed servers (ATLAS_SERVERS_CONFIG) cannot re-register — the
   267		// register API rejects them — so a valid heartbeat is their proof of
   268		// life: it re-enters rotation from dead-ish states the same way a
   269		// re-register does for API servers. Without this, a declared server
   270		// aged offline before its instance booted could never come back.
   271		if srv.Source == "config" && (srv.Status == model.StatusSuspect || srv.Status == model.StatusOffline) {
   272			if err := s.servers.UpdateServerStatus(ctx, serverID, model.StatusOnline); err != nil {
   273				return "", fmt.Errorf("promote to online: %w", err)
   274			}
   275			s.logger.Info("config-managed server re-entered rotation", "server_id", serverID, "from", srv.Status)
   276			return model.StatusOnline, nil
   277		}
   278	
   279		return srv.Status, nil
   280	}
    57	// applyCreated upserts a full index row. Re-delivered created events are
    58	// harmless upserts (docs/sync.md §6).
    59	func (s *Service) applyCreated(ctx context.Context, e *event.Event) (*model.Character, error) {
    60		if e.AccountID <= 0 {
    61			return nil, fmt.Errorf("%w: account_id must be > 0", model.ErrInvalid)
    62		}
    63		if e.ServerID == "" {
    64			return nil, fmt.Errorf("%w: server_id is required", model.ErrInvalid)
    65		}
    66		if e.CharacterID <= 0 {
    67			return nil, fmt.Errorf("%w: character_id must be > 0", model.ErrInvalid)
    68		}
    69	
    70		now := time.Now()
    71		ch := &model.Character{
    72			AccountID:   e.AccountID,
    73			ServerID:    e.ServerID,
    74			CharacterID: e.CharacterID,
    75			Name:        e.Name,
    76			Level:       derefInt(e.Level),
    77			ClassID:     derefInt(e.ClassID),
    78			Avatar:      derefString(e.Avatar),
    79			Metadata:    derefMetadata(e.Metadata),
    80			LastLoginAt: &now,
    81		}
    82		if !e.Timestamp.IsZero() {
    83			ch.LastLoginAt = &e.Timestamp
    84		}
    85	
    86		if err := s.characters.UpsertCharacter(ctx, ch); err != nil {
    87			return nil, fmt.Errorf("create character: %w", err)
    88		}
    89		return ch, nil
    68	// CreateMigration creates a new migration record.
    69	func (s *Service) CreateMigration(ctx context.Context, req CreateMigrationRequest) (*model.Migration, error) {
    70		if len(req.SourceServers) == 0 {
    71			return nil, fmt.Errorf("%w: at least one source server is required", model.ErrInvalid)
    72		}
    73		if req.TargetServer == "" {
    74			return nil, fmt.Errorf("%w: target server is required", model.ErrInvalid)
    75		}
    76	
    77		// Validate source servers exist.
    78		for _, srcID := range req.SourceServers {
    79			if _, err := s.store.GetServer(ctx, srcID); err != nil {
    80				return nil, fmt.Errorf("source server %s: %w", srcID, err)
    81			}
    82		}
    83	
    84		// Validate target server exists.
    85		if _, err := s.store.GetServer(ctx, req.TargetServer); err != nil {
    86			return nil, fmt.Errorf("target server %s: %w", req.TargetServer, err)
    87		}
    88	
    89		now := time.Now()
    90	
    91		// Linkage: auto-create maintenance windows on source servers so they
    92		// enter protection during the migration. Fail-closed: if windows can't
    93		// be created, the migration record is not written (sources would be
    94		// unprotected).
    95		if s.windows != nil {
    96			if err := s.windows.CreateWindows(ctx, req.SourceServers, now, now.Add(DefaultMigrationWindowDuration)); err != nil {
    97				return nil, fmt.Errorf("create maintenance windows: %w", err)
    98			}
    99		}
   100	
   101		m := &model.Migration{
   102			ID:            fmt.Sprintf("mig-%d", now.UnixNano()),
   103			SourceServers: req.SourceServers,
   104			TargetServer:  req.TargetServer,
   105			Status:        model.MigrationPending,
   106			StartedAt:     now,
   107		}
   108	
   109		if err := s.store.CreateMigration(ctx, m); err != nil {
   110			return nil, fmt.Errorf("create migration: %w", err)
   111		}
   112	
   113		return m, nil
   114	}
   125	// RollbackMigration transitions a migration to rolled_back status.
   126	// Only migrations in pending, migrating, or failed status can be rolled back.
   127	func (s *Service) RollbackMigration(ctx context.Context, id string) error {
   128		m, err := s.store.GetMigration(ctx, id)
   129		if err != nil {
   130			return fmt.Errorf("rollback migration: %w", err)
   131		}
   132	
   133		switch m.Status {
   134		case model.MigrationPending, model.MigrationMigrating, model.MigrationFailed:
   135			// OK to rollback.
   136		default:
   137			return fmt.Errorf("%w: cannot rollback migration in status %q", model.ErrInvalid, m.Status)
   138		}
   139	
   140		return s.store.UpdateMigrationStatus(ctx, id, model.MigrationRolledBack, nil)
   141	}
[0m$ [0mnl -ba cmd/atlas/main.go | sed -n '552,605p'; nl -ba docs/zh/security.md | sed -n '4,9p;46,56p;72,76p'; nl -ba internal/httpapi/audit.go | sed -n '29,50p;107,128p'; nl -ba internal/event/kafka/adapter.go | sed -n '206,224p'; nl -ba internal/event/nats/adapter.go | sed -n '214,232p'
552		// ── Admin API (management network) ─────────────────────
   553		adminMux := http.NewServeMux()
   554		handler.RegisterAdminRoutes(adminMux)
   555		adminMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
   556			if err := composite.Ping(r.Context()); err != nil {
   557				w.WriteHeader(http.StatusServiceUnavailable)
   558				w.Write([]byte(`{"status":"not ready","error":"` + err.Error() + `"}`))
   559				return
   560			}
   561			w.WriteHeader(http.StatusOK)
   562			w.Write([]byte(`{"status":"ok"}`))
   563		})
   564		adminMux.Handle("GET /metrics", prom.Handler())
   565	
   566		var adminHandler http.Handler = http.Handler(adminMux)
   567		// Audit inside auth (records the resolved actor), rate limit outside
   568		// (cheap per-IP rejection before any key check).
   569		if auditLog != nil {
   570			adminHandler = auditLog.Middleware(adminHandler)
   571		}
   572		if len(adminKeys) > 0 || len(adminIPs) > 0 {
   573			adminHandler = httpapi.AdminAuth(httpapi.AuthConfig{
   574				APIKeys:     adminKeys,
   575				Roles:       adminRoles,
   576				IPWhitelist: adminIPs,
   577				Logger:      logger,
   578			})(adminHandler)
   579		}
   580		if limiter != nil {
   581			adminHandler = limiter.Middleware(adminHandler)
   582		}
   583		adminHandler = metrics.RequestCounter(prom.AdminRequests)(adminHandler)
   584		adminHandler = httpapi.Tracing(logger)(httpapi.Spans()(adminHandler))
   585		// CORS outermost (outside auth): preflight OPTIONS carries no admin
   586		// key, and the dashboard dev server needs it answered before the
   587		// browser will send the real request.
   588		adminHandler = httpapi.CORSMiddleware(cfg.CORSAllowedOrigins)(adminHandler)
   589		adminSrv := &http.Server{
   590			Addr:         cfg.AdminAddr,
   591			Handler:      adminHandler,
   592			ReadTimeout:  10 * time.Second,
   593			WriteTimeout: 10 * time.Second,
   594			IdleTimeout:  60 * time.Second,
   595		}
   596	
   597		// ── gRPC API (all five services, TODO v0.1.5) ──────────
   598		// Same security domains as REST (docs/api.md §认证): Registry and
   599		// Admin RPCs are guarded when tokens/keys are configured, unconfigured
   600		// stays the open dev mode. The Go SDK already sends the right
   601		// credential per call over gRPC (grpc.go callCtx). Order mirrors the
   602		// REST muxes: tracing outermost (every call gets a request id, even
   603		// rejected ones), then metrics (like RequestCounter wrapping
   604		// limiter → auth → audit, so rejected calls count), rate limit
   605		// outside auth (cheap per-IP rejection before any key check), audit
     4	v0.1.17 起 Atlas 内建四层防护：Registry mTLS、Admin RBAC、操作审计与限流。mTLS / RBAC / 限流为**可选启用**（未配置即对应防护关闭）；审计默认**开启**（`ATLAS_AUDIT_ENABLED=0` 关闭）。
     5	
     6	## 1. TLS / mTLS（Registry :8081 与 gRPC :9090）
     7	
     8	两个面向内网的服务间监听口——REST 注册口与 gRPC 五服务口——共用同一套 TLS 语义，各自独立配置：配置证书后开启 TLS；再配置客户端 CA 即升级为**双向 TLS**——客户端必须出示受信任 CA 签发的证书。
     9	
    46	## 3. 操作审计
    47	
    48	开启后（默认开启，`ATLAS_AUDIT_ENABLED=0` 关闭），每一条 Admin 请求都会被记录：
    49	
    50	- **actor**：两段式 `role:key指纹`（key 的 SHA-256 前 12 位十六进制，不可逆推原 key）；mTLS 场景为三段式 `role:key指纹:证书指纹`（`auth.go` `Actor.String()`），未配置 Key 的开发模式为 `role:anonymous`；
    51	- **timestamp**：RFC3339 纳秒时间；
    52	- **method / path / query / status**；
    53	- **diff**：变更类方法（POST/PUT/PATCH/DELETE）的请求体（上限 4KB，非法 JSON 包装为字符串存储）。
    54	
    55	记录同时落入结构化日志（`slog.Info`，`msg=admin audit`）与内存环形缓冲（默认 1,000 条），并通过 `GET /v1/admin/audit?limit=50` 提供最近条目：
    56	
    72	审计位于认证**之内**（拿到解析后的 actor）、限流**之内**——链路为 limiter → 认证（审计中间件在其内）→ 业务，被限流拒绝的请求到不了认证、也就不进审计环；审计环不落盘，持久化交给日志采集管道（结构化日志行）。
    73	
    74	管理台「操作审计」页直接读这个环（`/audit` 路由）：写操作逐条展示变更体、读操作标出无变更体，状态码按 2xx / 4xx / 5xx 分色，改条数即改 `limit`。页面底部如实标注这是内存环、重启即丢——排查近期谁动了什么够用，合规归档仍以结构化日志为准。
    75	
    76	**双传输**：gRPC（:9090）的 AdminService RPC 记入同一个环——`path` 为全方法名（如 `/atlas.v1.AdminService/Disable`），读 RPC 记 `GET`（无 diff）、写 RPC 记 `POST`（diff 为 protojson 请求，同受 4KB 上限），gRPC status code 按 google.rpc 惯例映射为等价 HTTP 状态入环；actor 同为 `role:key指纹`（校验由 gRPC 认证拦截器完成，未配置 Key 的开发模式显示 `:anonymous`）。Registry / Discovery / Directory / Routing 的 RPC 不进审计环（与 REST 只审计 `/v1/admin/*` 一致）。
    29	// AuditLog records Admin API operations.
    30	type AuditLog struct {
    31		logger *slog.Logger
    32		max    int // ring capacity
    33	
    34		mu      sync.Mutex
    35		ring    []AuditEntry
    36		maxBody int
    37	}
    38	
    39	// NewAuditLog creates an audit log holding the most recent max entries
    40	// (<= 0 means 1000). maxBody bounds the captured request payload per entry
    41	// (<= 0 means 4096 bytes).
    42	func NewAuditLog(max, maxBody int, logger *slog.Logger) *AuditLog {
    43		if max <= 0 {
    44			max = 1000
    45		}
    46		if maxBody <= 0 {
    47			maxBody = 4096
    48		}
    49		return &AuditLog{logger: logger, max: max, maxBody: maxBody}
    50	}
   107	// Record appends an entry to the ring and mirrors it to the structured log.
   108	// Transport-neutral: the REST middleware and the gRPC UnaryAudit
   109	// interceptor both funnel through here, so ops tooling sees one ring and
   110	// one log vocabulary regardless of how the operation arrived.
   111	func (a *AuditLog) Record(e AuditEntry) {
   112		a.mu.Lock()
   113		a.ring = append(a.ring, e)
   114		if len(a.ring) > a.max {
   115			a.ring = a.ring[len(a.ring)-a.max:]
   116		}
   117		a.mu.Unlock()
   118	
   119		a.logger.Info("admin audit",
   120			"actor", e.Actor,
   121			"method", e.Method,
   122			"path", e.Path,
   123			"query", e.Query,
   124			"status", e.Status,
   125			"body", string(e.Body),
   126			"time", e.Time.Format(time.RFC3339Nano),
   127		)
   128	}
   206	func (a *Adapter) handle(ctx context.Context, h event.Handler, msg kafka.Message) {
   207		var e event.Event
   208		if err := json.Unmarshal(msg.Value, &e); err != nil {
   209			// Poison payload: it can never succeed, so commit it and move on.
   210			a.logger.Error("event: undecodable payload, dropping",
   211				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
   212			a.commit(ctx, msg)
   213			return
   214		}
   215		e.ID = fmt.Sprintf("%d@%d", msg.Partition, msg.Offset)
   216	
   217		if err := h(ctx, &e); err != nil {
   218			a.logger.Error("event: handler failed, offset not committed",
   219				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset,
   220				"type", string(e.Type), "error", err)
   221			return
   222		}
   223		a.commit(ctx, msg)
   224	}
   214		var e event.Event
   215		if err := json.Unmarshal(msg.Data, &e); err != nil {
   216			// Poison payload: it can never succeed, so ack it and move on.
   217			a.logger.Error("event: undecodable payload, dropping",
   218				"subject", msg.Subject, "stream_seq", seq, "error", err)
   219			_ = msg.Ack()
   220			return
   221		}
   222		e.ID = fmt.Sprintf("%d", seq)
   223	
   224		if err := h(ctx, &e); err != nil {
   225			a.logger.Error("event: handler failed, left unacked",
   226				"subject", msg.Subject, "stream_seq", seq,
   227				"type", string(e.Type), "error", err)
   228			return
   229		}
   230		if err := msg.Ack(); err != nil {
   231			a.logger.Error("event: ack failed", "subject", msg.Subject, "stream_seq", seq, "error", err)
   232		}
[0m$ [0mnl -ba internal/health/maintenance_test.go | sed -n '30,66p;138,177p'; nl -ba internal/health/monitor_test.go | sed -n '85,108p'
30	func TestMaintenanceWindow_AppliesAndRestores(t *testing.T) {
    31		mem := memory.New()
    32		ctx := context.Background()
    33		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
    34	
    35		registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
    36	
    37		base := time.Now()
    38		win := &model.MaintenanceWindow{
    39			ID:       "mwin-1",
    40			ServerID: "srv-1",
    41			StartAt:  base.Add(-time.Minute),
    42			EndAt:    base.Add(time.Hour),
    43		}
    44		if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
    45			t.Fatalf("create window: %v", err)
    46		}
    47	
    48		// Window open → server moves to maintenance, previous status recorded.
    49		applyWindowAt(t, mon, ctx, base)
    50		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusMaintenance {
    51			t.Fatalf("in-window status = %s, want maintenance", got)
    52		}
    53		w, _ := mem.GetMaintenanceWindow(ctx, "mwin-1")
    54		if w.PreviousStatus != model.StatusOnline {
    55			t.Fatalf("previous status = %q, want online", w.PreviousStatus)
    56		}
    57	
    58		// Window over → status restored, window deleted.
    59		applyWindowAt(t, mon, ctx, base.Add(2*time.Hour))
    60		if got := statusOf(t, ctx, mem, "srv-1"); got != model.StatusOnline {
    61			t.Fatalf("post-window status = %s, want online (restored)", got)
    62		}
    63		if _, err := mem.GetMaintenanceWindow(ctx, "mwin-1"); err == nil {
    64			t.Fatal("expired window should be deleted")
    65		}
    66	}
   138	func TestMaintenanceWindow_NonAutoManagedSkipped(t *testing.T) {
   139		mem := memory.New()
   140		ctx := context.Background()
   141		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
   142	
   143		// A draining server and a disabled server with windows open.
   144		registerServer(t, ctx, mem, "srv-drain", model.StatusDraining)
   145		registerServer(t, ctx, mem, "srv-disabled", model.StatusDisabled)
   146	
   147		base := time.Now()
   148		for i, id := range []string{"srv-drain", "srv-disabled"} {
   149			win := &model.MaintenanceWindow{
   150				ID:       "mwin-" + id,
   151				ServerID: id,
   152				StartAt:  base.Add(-time.Minute),
   153				EndAt:    base.Add(time.Hour),
   154			}
   155			if err := mem.CreateMaintenanceWindow(ctx, win); err != nil {
   156				t.Fatalf("create window %d: %v", i, err)
   157			}
   158		}
   159	
   160		applyWindowAt(t, mon, ctx, base)
   161	
   162		if got := statusOf(t, ctx, mem, "srv-drain"); got != model.StatusDraining {
   163			t.Errorf("draining server = %s, want draining (operator-owned)", got)
   164		}
   165		if got := statusOf(t, ctx, mem, "srv-disabled"); got != model.StatusDisabled {
   166			t.Errorf("disabled server = %s, want disabled (operator-owned)", got)
   167		}
   168		// Windows are consumed either way (no per-sweep retries).
   169		for _, id := range []string{"srv-drain", "srv-disabled"} {
   170			w, err := mem.GetMaintenanceWindow(ctx, "mwin-"+id)
   171			if err != nil {
   172				t.Fatalf("window %s: %v", id, err)
   173			}
   174			if w.PreviousStatus != "" {
   175				t.Errorf("window %s previous = %q, want empty (nothing to restore)", id, w.PreviousStatus)
   176			}
   177		}
    85	func TestSweep_OnlineToOffline(t *testing.T) {
    86		mem := memory.New()
    87		ctx := context.Background()
    88	
    89		mon := newTestMonitor(mem, 30*time.Second, 60*time.Second)
    90	
    91		registerServer(t, ctx, mem, "srv-1", model.StatusOnline)
    92		recordHeartbeat(t, ctx, mem, "srv-1", 100)
    93	
    94		heartbeatTime := time.Now()
    95		mon.now = func() time.Time { return heartbeatTime.Add(65 * time.Second) }
    96	
    97		if err := mon.sweep(ctx); err != nil {
    98			t.Fatalf("sweep: %v", err)
    99		}
   100	
   101		srv, err := mem.GetServer(ctx, "srv-1")
   102		if err != nil {
   103			t.Fatalf("GetServer: %v", err)
   104		}
   105		if srv.Status != model.StatusOffline {
   106			t.Errorf("expected offline, got %s", srv.Status)
   107		}
   108	}
[0m$ [0mnl -ba docs/zh/realms-shards.md | sed -n '89,93p'; nl -ba docs/zh/roadmap.md | sed -n '49,54p'; nl -ba docs/zh/performance.md | sed -n '21,30p;87,94p'; nl -ba docs/performance.md | sed -n '33,40p;164,172p'; nl -ba README.zh.md | sed -n '118,128p'; nl -ba Makefile | sed -n '9,14p'; nl -ba internal/store/store.go | sed -n '142,155p;163,189p'
89	### 分服合并（合服）
    90	
    91	Shard 层面的合并由[合服迁移](/zh/migration)完成：`POST /v1/admin/migrations` 把源服务器角色
    92	索引原子迁到目标，之后源服务器下线、客户端按同一 `shard` 维度筛选即可无感。
    93	
    49	| ✅ gRPC 双传输 | 5 服务 23 RPC，:9090（核心面与 REST 同源） | v0.1.5 |
    50	| ✅ 六语言 SDK | Go(双传输) / C++ / Python(同步异步) / JS/TS / Java / C#，自动心跳内置 | v0.1.6 ~ v0.1.11 |
    51	| ✅ APISIX 插件 | atlas-auth 玩家 token 鉴权注入 + atlas-ratelimit 端点组限流 | v0.1.13 |
    52	| ✅ Realm / Shard 管理 | Admin CRUD + 三存储实现 + 索引迁移 | v0.1.14 |
    53	| ✅ 健康告警 | suspect / offline 占比阈值 + webhook 通知（锁存语义） | v0.1.15 |
    54	| ✅ 角色索引分片 | account-hash 分片 + 跨分片查询 + 重切工具 | v0.1.16 |
    21	10 万台服务器 × 10s 心跳 = 1 万 QPS 持续写。实现是 Redis hash + 管线（`internal/store/redisstore`）：
    22	
    23	```go
    24	pipe := s.client.Pipeline()
    25	pipe.HSet(ctx, key, fields)      // status/players/load/last_seen
    26	pipe.Expire(ctx, key, runtimeTTL) // 120s —— 失联自动消失,无清理任务
    27	```
    28	
    29	一次网络往返写完全部字段；TTL 到期即"下线"，**不需要任何后台清扫协程**。基准：单核约 20 万 QPS（`BenchmarkHeartbeat` ~5.1µs/op；64 台并发扇入 `BenchmarkHeartbeatParallel` ~4.4µs/op，攒批吸收竞争后基本持平）——1 万 QPS 的真实负载用一个核的 5%。成本主要来自 v0.2 指令队列的逐条回执（改前直锁路径 129ns，对照见 §8.5），换来同实体严格 FIFO、幂等防重与可回放。瓶颈从来不在心跳本身，而在 Redis 与网络。
    30	
    87	| 游戏服网络分区 | 心跳超龄 suspect → offline，客户端列表不抖动（[lifecycle.md](lifecycle.md) §3-§4） | ✅ 自动化：`internal/health` 巡检演练（OnlineToSuspect / SuspectToOffline / SuspectRecovered）+ discovery 可见性测试 |
    88	| 游戏服重启 / 心跳延迟 | 重注册幂等恢复；3:1:6 节奏容忍抖动（lifecycle §4.2） | ✅ 自动化：`internal/store/storetest` 重注册契约（suspect/offline 重置表）+ redisstore miniredis 契约 |
    89	| 重复注册 / 重复迁移 | 注册幂等 upsert；迁移幂等可重放（[migration.md](migration.md) §7） | ✅ 自动化：storetest 幂等 upsert + `internal/admin` 迁移编排 / rollback 用例 |
    90	| Redis 数据丢失 | 档案在 PG，TTL 自清理，心跳重填（[data-model.md](data-model.md) §6） | ✅ 自动化（2026-10-04）：`internal/store/redisstore` `TestDataLossFlushAll_HeartbeatRefills`（miniredis FLUSHALL → 缺失即缺席、心跳重填） |
    91	| PG 短暂不可用 | pgx 池 + 健康检查自动回收死连接（本文 §4） | 服务层故障传播 ✅（discovery 后端降级测试、registry 错误路径）；连接池自愈仍属部署演练（ha.md） |
    92	| 迁移中途失败 | 先复制、后切换、再清理，源数据常驻（migration §7） | ✅ 自动化：`internal/admin` rollback 流程用例（pending/completed/not-found 三分支） |
    93	| Atlas 副本宕机 | 无状态多副本 + HAProxy 心跳扇入（[ha.md](ha.md)） | 部署级演练（ha.md，属多副本编排，不进单元套件） |
    94	
    33	pipe.Expire(ctx, key, runtimeTTL) // 120s —— lost contact disappears on its own, no cleanup job
    34	```
    35	
    36	One network round trip writes every field; when the TTL expires the server is
    37	effectively "offline", and **no background sweeper goroutine is needed at all**.
    38	Benchmark: about 200k QPS on one core (`BenchmarkHeartbeat` ~5.1µs/op; 64-server
    39	concurrent fan-in `BenchmarkHeartbeatParallel` ~4.4µs/op — essentially flat once
    40	batching absorbs the contention) — a real 10k QPS load costs 5% of one core. Most of
   164	| Failure scenario | Existing guarantee (doc source) | Verification (automated location) |
   165	| --- | --- | --- |
   166	| Game server network partition | Heartbeat ages out → suspect → offline, no client-list flicker ([lifecycle.md](lifecycle.md) §3-§4) | ✅ Automated: `internal/health` sweep drills (OnlineToSuspect / SuspectToOffline / SuspectRecovered) + discovery visibility tests |
   167	| Game server restart / heartbeat delay | Idempotent re-registration recovers; 3:1:6 pacing tolerates jitter (lifecycle §4.2) | ✅ Automated: `internal/store/storetest` re-registration contract (suspect/offline reset table) + redisstore miniredis contract |
   168	| Duplicate registration / duplicate migration | Registration is an idempotent upsert; migrations are idempotent and replayable ([migration.md](migration.md) §7) | ✅ Automated: storetest idempotent upsert + `internal/admin` migration orchestration / rollback cases |
   169	| Redis data loss | Profiles live in PG, TTL self-cleans, heartbeats refill ([data-model.md](data-model.md) §6) | ✅ Automated (2026-10-04): `internal/store/redisstore` `TestDataLossFlushAll_HeartbeatRefills` (miniredis FLUSHALL → missing means absent, heartbeats refill) |
   170	| PG briefly unavailable | pgx pool + health checks automatically recycle dead connections (see §4) | Service-layer failure propagation ✅ (discovery backend degradation test, registry error paths); pool self-healing still belongs to deployment drills (ha.md) |
   171	| Migration fails midway | Copy first, switch over, then clean up; source data stays resident (migration §7) | ✅ Automated: `internal/admin` rollback flow cases (the pending/completed/not-found branches) |
   172	| Atlas replica goes down | Stateless multi-replica + HAProxy heartbeat fan-in ([ha.md](ha.md)) | Deployment-level drill (ha.md; part of multi-replica orchestration, not in the unit suite) |
   118	## Quick Start
   119	
   120	```bash
   121	# Run with in-memory store (no dependencies)
   122	
   123	> [English](README.md) | **中文**
   124	make run
   125	
   126	# Run tests
   127	make test
   128	```
     9	build:
    10		CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/atlas ./cmd/atlas
    11	
    12	run: build
    13		ATLAS_STORE=memory ./bin/atlas
    14	
   142	// MigrationStore persists server migration records.
   143	type MigrationStore interface {
   144		// CreateMigration inserts a new migration record.
   145		CreateMigration(ctx context.Context, m *model.Migration) error
   146	
   147		// GetMigration returns a migration by ID.
   148		GetMigration(ctx context.Context, id string) (*model.Migration, error)
   149	
   150		// UpdateMigrationStatus changes the status of a migration.
   151		UpdateMigrationStatus(ctx context.Context, id string, status model.MigrationStatus, completedAt *time.Time) error
   152	
   153		// ListMigrations returns recent migrations, ordered by start time descending.
   154		ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error)
   155	}
   163	// RealmStore manages realm records (administrative server groupings,
   164	// TODO v0.1.14).
   165	type RealmStore interface {
   166		// CreateRealm inserts a realm. ErrConflict when the ID already exists.
   167		CreateRealm(ctx context.Context, r *model.Realm) error
   168	
   169		// GetRealm returns a realm by ID (ErrNotFound when absent).
   170		GetRealm(ctx context.Context, id string) (*model.Realm, error)
   171	
   172		// ListRealms returns realms ordered by created_at descending, capped
   173		// at limit (<= 0 means no cap).
   174		ListRealms(ctx context.Context, limit int) ([]*model.Realm, error)
   175	}
   176	
   177	// ShardStore manages shard records (realm subdivisions, TODO v0.1.14).
   178	type ShardStore interface {
   179		// CreateShard inserts a shard. ErrConflict when the ID already exists.
   180		CreateShard(ctx context.Context, s *model.Shard) error
   181	
   182		// GetShard returns a shard by ID (ErrNotFound when absent).
   183		GetShard(ctx context.Context, id string) (*model.Shard, error)
   184	
   185		// ListShards returns shards ordered by created_at descending, capped
   186		// at limit (<= 0 means no cap). A non-empty realmID narrows to that
   187		// realm's shards.
   188		ListShards(ctx context.Context, realmID string, limit int) ([]*model.Shard, error)
   189	}
[0m✗ [0mGrep "func \(s \*Store\) RegisterServer|case opRegisterServer|RegisterServer(ctx" failed
[91m[1mError: [0mInvalid regex pattern: rg: regex parse error:
    (?:func \(s \*Store\) RegisterServer|case opRegisterServer|RegisterServer(ctx)
    ^
error: unclosed group
[0m→ [0mRead docs/zh/benchmarks.md [90m[offset=1, limit=130][0m
[0m$ [0mnl -ba cmd/atlas/main.go | sed -n '640,720p'; nl -ba docs/zh/lifecycle.md | sed -n '168,180p'; nl -ba docs/zh/sync.md | sed -n '140,158p'; nl -ba internal/event/rabbitmq/adapter.go | sed -n '220,251p'
640				logger.Info("grpc mTLS enabled (client certificates required)")
   641			} else {
   642				logger.Info("grpc TLS enabled")
   643			}
   644		}
   645		if len(grpcInterceptors) > 0 {
   646			grpcSrvOpts = append(grpcSrvOpts, grpc.ChainUnaryInterceptor(grpcInterceptors...))
   647		}
   648		grpcSrv := grpc.NewServer(grpcSrvOpts...)
   649		atlasgrpc.New(regSvc, discSvc, dirSvc, rtSvc, admSvc, evtAdapter).
   650			WithFleetIndex(fleetIdx).
   651			WithTelemetry(sampler).
   652			RegisterServices(grpcSrv)
   653	
   654		// Graceful shutdown on SIGINT/SIGTERM.
   655		sigCh := make(chan os.Signal, 1)
   656		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
   657	
   658		go func() {
   659			logger.Info("public API listening", "addr", cfg.HTTPAddr)
   660			if err := publicSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
   661				logger.Error("public API error", "error", err)
   662				os.Exit(1)
   663			}
   664		}()
   665		go func() {
   666			scheme := "http"
   667			if regTLS != nil {
   668				scheme = "tls"
   669			}
   670			logger.Info("registry API listening", "addr", cfg.RegistryAddr, "scheme", scheme)
   671			regLn, err := net.Listen("tcp", cfg.RegistryAddr)
   672			if err != nil {
   673				logger.Error("registry API listen failed", "error", err)
   674				os.Exit(1)
   675			}
   676			if regTLS != nil {
   677				regLn = tls.NewListener(regLn, regTLS)
   678			}
   679			if err := regSrv.Serve(regLn); err != nil && err != http.ErrServerClosed {
   680				logger.Error("registry API error", "error", err)
   681				os.Exit(1)
   682			}
   683		}()
   684		go func() {
   685			logger.Info("admin API listening", "addr", cfg.AdminAddr)
   686			if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
   687				logger.Error("admin API error", "error", err)
   688				os.Exit(1)
   689			}
   690		}()
   691		go func() {
   692			if cfg.GRPCAddr == "" {
   693				logger.Info("gRPC API disabled (ATLAS_GRPC_ADDR empty)")
   694				return
   695			}
   696			lis, err := net.Listen("tcp", cfg.GRPCAddr)
   697			if err != nil {
   698				logger.Error("gRPC listen error", "addr", cfg.GRPCAddr, "error", err)
   699				os.Exit(1)
   700			}
   701			logger.Info("gRPC API listening", "addr", cfg.GRPCAddr)
   702			if err := grpcSrv.Serve(lis); err != nil {
   703				logger.Error("gRPC API error", "error", err)
   704			}
   705		}()
   706	
   707		sig := <-sigCh
   708		logger.Info("shutting down", "signal", sig)
   709	
   710		// Stop health monitor.
   711		monitorCancel()
   712	
   713		// Shutdown all HTTP servers with a timeout.
   714		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
   715		defer shutdownCancel()
   716	
   717		publicSrv.Shutdown(shutdownCtx)
   718		regSrv.Shutdown(shutdownCtx)
   719		adminSrv.Shutdown(shutdownCtx)
   720	
   168	> TTL（120s）比判活阈值大，刻意如此：Redis 键过期只负责"彻底失联的服务器的运行时数据自动消失"，生死判定一律以 `last_seen_at` 年龄为准，避免两层语义打架。
   169	
   170	阈值建议：
   171	
   172	| 参数 | 默认值 | 说明 |
   173	| --- | --- | --- |
   174	| 心跳间隔 | 10s | 游戏服务器上报频率 |
   175	| runtime HASH TTL | 120s | 固定值，仅键自清理；每次心跳刷新 |
   176	| suspect 阈值 | 30s | 3× 心跳间隔（见 §4.2） |
   177	| offline 阈值 | 60s | 6× 心跳间隔 |
   178	
   179	阈值应可配置，不同部署环境（同机房 vs 跨地域）差异较大。从未上报过心跳的服务器（无 runtime 数据）：仅 `starting` 状态参与年龄判死——以 `CreatedAt` 起算，超过 offline 阈值**直接置 `offline`（不经过 suspect）**；其余状态不参与该巡检（`maintenance` / `disabled` 等由运维显式管理）。
   180	
   140	| 进程内同步 | `http`（默认） | — | 事件在请求内同步落地，保留 v0.1 行为，响应含投影结果 |
   141	| Redis Streams | `redis` | `ATLAS_REDIS_URL` | `XADD` 入流，消费组 `atlas` 经 `XREADGROUP` 异步消费后 `XACK`；写端点返回 202 |
   142	| Kafka | `kafka` | `ATLAS_KAFKA_BROKERS`（逗号分隔，默认 `localhost:9092`） | 写入 topic `atlas.characters`（segmentio/kafka-go，纯 Go）；消费组 `atlas` 手动提交 offset |
   143	| NATS | `nats` | `ATLAS_NATS_URL`（默认 `nats://localhost:4222`） | JetStream 流 `ATLAS`（subject `atlas.characters`），durable pull consumer `atlas` 显式 Ack |
   144	| RabbitMQ | `rabbitmq` | `ATLAS_RABBITMQ_URL`（默认 `amqp://localhost:5672/`） | durable topic exchange `atlas`，队列按 topic 命名；persistent 消息 + 手动 Ack |
   145	
   146	除 `http` 外全部为 at-least-once 投递，处理失败的事件会重投：
   147	
   148	- **redis**：失败事件留在 PEL，30 秒后被 `XAUTOCLAIM` 重投；超过 5 次投递
   149	  记日志死信。
   150	- **kafka**：失败事件的 offset 不提交，下一次 fetch/rebalance 重新投递。
   151	- **nats**：失败消息不 Ack，超过 AckWait（默认 30s）由 durable consumer 重投。
   152	- **rabbitmq**：失败消息先按 `requeueBackoff`（默认 5s）退避、再 `Nack(requeue=true)` 重投——无 delivery counter 可用来封顶重试，退避挡在消费者 goroutine 上，顺带节流后续投递。
   153	
   154	因此消费端必须幂等——`Directory.ApplyEvent` 的 upsert 天然幂等（见 §6）。
   155	
   156	接入只需改一个环境变量，例如：
   157	
   158	```bash
   220	func (a *Adapter) consume(ctx context.Context, deliveries <-chan amqp.Delivery, h event.Handler) {
   221		defer a.wg.Done()
   222		for {
   223			select {
   224			case <-ctx.Done():
   225				return
   226			case msg, ok := <-deliveries:
   227				if !ok {
   228					return // channel closed by the broker/shutdown
   229				}
   230				a.handle(ctx, h, msg)
   231			}
   232		}
   233	}
   234	
   235	func (a *Adapter) handle(ctx context.Context, h event.Handler, msg amqp.Delivery) {
   236		var e event.Event
   237		if err := json.Unmarshal(msg.Body, &e); err != nil {
   238			// Poison payload: it can never succeed, so ack it and move on.
   239			a.logger.Error("event: undecodable payload, dropping",
   240				"exchange", a.exchange, "message_id", msg.MessageId, "error", err)
   241			_ = a.ch.Ack(msg.DeliveryTag, false)
   242			return
   243		}
   244		e.ID = msg.MessageId
   245	
   246		if err := h(ctx, &e); err != nil {
   247			a.logger.Error("event: handler failed, requeueing",
   248				"exchange", a.exchange, "message_id", msg.MessageId,
   249				"type", string(e.Type), "error", err)
   250			// Back off before the requeue: rabbitmq has no delivery counter to
   251			// cap retries against, and Nack(requeue) on the same connection
[0m✱ [0mGrep "乱序|out.of.order|不处理" [90min docs/zh/sync.md · 3 matches[0m
### 表一 缺陷（按风险排序）

| 问题 | file:line | 为什么是问题 | 建议动作 |
|---|---|---|---|
| **高｜维护窗口可能让服务器永久停在 maintenance** | `internal/health/monitor.go:314-329`；`internal/health/monitor.go:302-307`；现有测试只覆盖正常应用与恢复：`internal/health/maintenance_test.go:30-66` | 窗口开始时先把服务器改为 `maintenance`，再保存原状态。如果 `MarkMaintenanceWindowApplied` 失败，下轮巡检会因服务器已是 `maintenance` 而把空的原状态写回；窗口结束时没有原状态可恢复，服务器会一直留在维护状态。 | 将状态变更与原状态记录做成原子操作；或确保记录失败后下轮仍能恢复原状态。增加持久化失败回归测试。 |
| **高｜巡检的旧快照可能覆盖重注册状态，服务器持续离线** | `internal/health/monitor.go:84-95,154-163,262-270`；`internal/registry/registry.go:204-216,257-279` | 巡检读取服务器快照后，若服务器重注册并恢复心跳，巡检仍可能基于旧快照无条件写入 `offline`。普通 API 管理服务器的心跳不会把 `offline` 自动改回 `online`，因此可能需要再次注册才能恢复。 | 状态更新加入预期旧状态条件（CAS/条件更新）；巡检基于过期快照写入时放弃转移，并补并发重注册回归测试。 |
| **中｜存储写失败时仍将状态计入转移结果** | `internal/health/monitor.go:268-270`；`internal/health/monitor.go:193-196` | `transition` 在 `UpdateServerStatus` 成功前就修改本地快照并计数。写入失败后，巡检仍用这份快照计算舰队告警，可能误报；转移指标也会记录未成功的状态变化。 | 写入成功后再修改本地状态和计数；区分尝试次数与成功转移指标。 |
| **中｜乱序的旧角色事件会覆盖较新的投影数据** | `internal/directory/events.go:70-87`；已记录的取舍：`docs/zh/sync.md:234-241` | 重放旧 `created` 事件会完整 upsert 旧字段及旧时间戳；当前投影没有事件版本或序列号校验，可能把较新的角色信息覆盖掉。 | 为事件增加源端单调版本/序列号，并在存储写入时拒绝旧版本；保留乱序投递回归测试。 |

### 表二 文档与实现不一致

| 文档说法 | 文档位置 | 代码实际 | 代码位置 |
|---|---|---|---|
| 创建迁移会把源服务器角色索引**原子迁到目标**，并使源服务器下线。 | `docs/zh/realms-shards.md:91-92`；类似的“复制、切换、清理”保证也见 `docs/zh/performance.md:87-93` | 创建接口只校验服务器、创建维护窗口并写迁移记录；迁移服务没有角色索引复制、切换或自动下线操作。回滚也只更新迁移状态。 | `internal/migration/service.go:68-113,125-140`；`internal/store/store.go:142-155` |
| Realm / Shard 管理已提供 Admin CRUD。 | `docs/zh/roadmap.md:52` | HTTP 管理路由只有创建与列表；存储接口虽有单条读取，但没有更新或删除接口。 | `internal/httpapi/handlers.go:192-195`；`internal/store/store.go:163-188` |
| Redis runtime TTL 到期就等于服务器下线，而且不需要后台巡检。 | `docs/zh/performance.md:21-29`；英文版：`docs/performance.md:33-37` | TTL 只清理 runtime 键；服务器状态由健康巡检按 `last_seen_at` 年龄推进为 `suspect/offline`。生命周期文档也明确 TTL 不参与判活。 | `internal/health/monitor.go:77-83,151-181`；`docs/zh/lifecycle.md:168-179` |
| Quick Start 的 bash 示例包含语言切换链接，之后运行 `make run`。 | `README.zh.md:120-128`；英文版：`README.md:120-128` | `>` 行是 Markdown 引用文本，不是 shell 命令；在代码块中会被 shell 当作重定向语法处理。实际启动命令是 `make run`。 | `Makefile:12-13` |

### 表三 值得补的能力

| 能力 | 为什么现在没有 | 价值高低 |
|---|---|---|
| Admin 审计的持久化后端或可靠投递保证 | 审计环是最多 1000 条的进程内存数据，另写结构化日志；没有内建持久化存储或投递确认机制。`internal/httpapi/audit.go:39-49,107-127`；`docs/zh/security.md:55,72-74` | 高 |
| Kafka / NATS 失败事件的重试上限与死信通道 | handler 失败时 Kafka 不提交 offset、NATS 不 ACK；目前缺少重试封顶和 DLQ。`internal/event/kafka/adapter.go:217-223`；`internal/event/nats/adapter.go:224-232`；现状说明：`docs/zh/sync.md:146-152` | 高 |
| Admin 监听口 TLS | TLS 配置用于 Registry 与 gRPC；Admin HTTP 服务直接使用 `ListenAndServe`，需要依赖网络隔离、密钥及 IP 白名单。`cmd/atlas/main.go:589-595,684-690`；`docs/zh/security.md:6-8` | 高 |
| 生产形状的压测与长稳观测（p95/p99、GC、故障恢复） | 现有基准主要是排除网络与存储 I/O 的服务层微基准，没有覆盖端到端分位延迟和长时间运行。`docs/zh/benchmarks.md:4-7,72-79` | 高 |
