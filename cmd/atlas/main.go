// Atlas is a game server registry, discovery, and character directory service.
//
// See https://github.com/cuihairu/atlas for documentation.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	atlasgrpc "github.com/cuihairu/atlas/internal/grpc"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/config"
	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpEvent "github.com/cuihairu/atlas/internal/event/http"
	kafkaEvent "github.com/cuihairu/atlas/internal/event/kafka"
	natsEvent "github.com/cuihairu/atlas/internal/event/nats"
	rabbitEvent "github.com/cuihairu/atlas/internal/event/rabbitmq"
	redisEvent "github.com/cuihairu/atlas/internal/event/redis"
	"github.com/cuihairu/atlas/internal/fleet"
	"github.com/cuihairu/atlas/internal/health"
	"github.com/cuihairu/atlas/internal/httpapi"
	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/serversconfig"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
	pgStore "github.com/cuihairu/atlas/internal/store/postgres"
	redisStore "github.com/cuihairu/atlas/internal/store/redisstore"
	"github.com/cuihairu/atlas/internal/store/sharded"
	"github.com/cuihairu/atlas/internal/telemetry"
	"github.com/cuihairu/atlas/internal/tlsutil"
	"github.com/cuihairu/atlas/internal/version"
)

func main() {
	// `atlas healthcheck` is a container health probe: exit 0 when the
	// public API answers /healthz. Distroless images have no shell, so the
	// Dockerfile HEALTHCHECK / compose healthcheck calls the binary itself.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := config.Load()

	logger.Info("starting Atlas",
		"version", version.String(),
		"store", cfg.StoreType,
		"public", cfg.HTTPAddr,
		"registry", cfg.RegistryAddr,
		"admin", cfg.AdminAddr,
		"grpc", cfg.GRPCAddr,
	)

	// Create stores.
	var (
		serverStore    store.ServerStore
		charStore      store.CharacterStore
		runtimeStore   store.RuntimeStore
		migrationStore store.MigrationStore
		statsStore     store.StatsStore
		realmStore     store.RealmStore
		shardStore     store.ShardStore
		mainWinStore   store.MaintenanceWindowStore
		annStore       store.AnnouncementStore
		crossCfgStore  store.CrossServerConfigStore
		pingCloser     func()
	)

	ctx := context.Background()

	switch cfg.StoreType {
	case "memory":
		mem := memory.New()
		serverStore = mem
		charStore = mem
		runtimeStore = mem
		migrationStore = mem
		statsStore = mem
		realmStore = mem
		shardStore = mem
		mainWinStore = mem
		annStore = mem
		crossCfgStore = mem

	case "postgres":
		// PostgreSQL for server + character storage.
		poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			logger.Error("invalid database URL", "error", err)
			os.Exit(1)
		}
		applyPGPoolOptions(poolCfg, cfg)
		logger.Info("postgres pool configured",
			"max_conns", poolCfg.MaxConns,
			"min_conns", poolCfg.MinConns,
			"max_conn_lifetime", poolCfg.MaxConnLifetime,
			"max_conn_idle_time", poolCfg.MaxConnIdleTime,
			"health_check_period", poolCfg.HealthCheckPeriod,
		)
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			logger.Error("failed to connect to PostgreSQL", "error", err)
			os.Exit(1)
		}
		if err := pool.Ping(ctx); err != nil {
			logger.Error("failed to ping PostgreSQL", "error", err)
			os.Exit(1)
		}
		pg := pgStore.New(pool)
		serverStore = pg
		charStore = pg
		migrationStore = pg
		statsStore = pg
		realmStore = pg
		shardStore = pg
		mainWinStore = pg
		annStore = pg
		crossCfgStore = pg

		// Redis for runtime/heartbeat storage. Topology follows the config:
		// cluster seeds > Sentinel failover > single node (TODO v0.1.19).
		rdb := buildRedisClient(cfg)
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Error("failed to ping Redis", "error", err)
			os.Exit(1)
		}
		rs := redisStore.New(rdb)
		runtimeStore = rs
		// Player counts live in Redis; wire the heartbeat view into the
		// SQL stats so TotalPlayers is not silently 0.
		pg.WithRuntime(rs)

		pingCloser = func() {
			pool.Close()
			rdb.Close()
		}

	default:
		logger.Error("unknown store type (expected 'memory' or 'postgres')", "type", cfg.StoreType)
		os.Exit(1)
	}

	// Fleet aggregate index (BUGS ③): region / status / version / type /
	// realm / shard / tag counts plus the server-ID index live in memory,
	// maintained on the write path by decorators around the stores. Every
	// admin view — overview stats, /servers filter bar, admin list — reads
	// this one aggregate instead of each page recounting on its own. Seeded
	// below after the servers config is applied; a periodic reconcile is the
	// safety net for cross-replica writes this process did not observe.
	fleetIdx := fleet.New()
	serverStore = fleet.TrackServers(serverStore, fleetIdx)
	runtimeStore = fleet.TrackRuntime(runtimeStore, fleetIdx)

	// Character index sharding (TODO v0.1.16): ATLAS_CHAR_SHARDS > 1 routes
	// every character operation through an account-hash sharded composite.
	if cfg.CharShards > 1 {
		shardStores := make([]store.CharacterStore, cfg.CharShards)
		for i := range shardStores {
			shardStores[i] = charStore
		}
		charStore = sharded.New(shardStores, store.HashShardStrategy{})
		logger.Info("character index sharding enabled", "shards", cfg.CharShards)
	}

	// Config-declared servers (docs/server-config.md): a JSON file declares a
	// fleet that exists as soon as Atlas starts — no register call needed.
	// Any validation failure aborts startup; a declared set is applied as a
	// whole (create / refresh / release-back), and live status is untouched.
	if cfg.ServersConfigFile != "" {
		prof, profileName, err := serversconfig.Load(cfg.ServersConfigFile, cfg.ServersProfile)
		if err != nil {
			logger.Error("invalid servers config", "file", cfg.ServersConfigFile, "error", err)
			os.Exit(1)
		}
		res, err := serversconfig.Apply(ctx, serverStore, prof)
		if err != nil {
			logger.Error("failed to apply servers config", "file", cfg.ServersConfigFile, "error", err)
			os.Exit(1)
		}
		logger.Info("servers config applied",
			"file", cfg.ServersConfigFile,
			"profile", profileName,
			"declared", len(prof.Servers),
			"created", res.Created,
			"updated", res.Updated,
			"released", res.Released,
		)
	}

	// Startup seed: load the fleet already in the stores into the index —
	// SQL-backed processes start with an empty index and only new writes
	// would reach the decorators. Failure is non-fatal: the reconciler
	// retries on its next tick while the previous snapshot keeps serving.
	if err := fleetIdx.Reconcile(ctx, serverStore, runtimeStore); err != nil {
		logger.Warn("fleet index seed failed; reconcile ticks will retry", "error", err)
	}
	fleetCtx, fleetCancel := context.WithCancel(context.Background())
	defer fleetCancel()
	go fleet.RunReconciler(fleetCtx, fleetIdx, serverStore, runtimeStore, 30*time.Second)

	// Create services.
	// 维护中 registration policy (ATLAS_MAINTENANCE_ENFORCE): block | warn.
	regSvc := registry.New(serverStore, runtimeStore, logger).WithMaintenanceEnforce(cfg.MaintenanceEnforce)
	discSvc := discovery.New(serverStore, runtimeStore)
	dirSvc := directory.New(charStore)

	// Event adapter: transports character index writes into the directory
	// projection (docs/sync.md §4).
	var evtAdapter event.EventAdapter
	var evtRdb redis.UniversalClient
	switch cfg.EventAdapter {
	case "", "http":
		evtAdapter = httpEvent.New()
	case "redis":
		evtRdb = buildRedisClient(cfg)
		if err := evtRdb.Ping(ctx).Err(); err != nil {
			logger.Error("failed to ping Redis for event adapter", "error", err)
			os.Exit(1)
		}
		evtAdapter = redisEvent.New(evtRdb, redisEvent.Options{Logger: logger})
	case "kafka":
		ka, err := kafkaEvent.Open(ctx, kafkaEvent.Options{
			Brokers: strings.Split(cfg.KafkaBrokers, ","),
			Logger:  logger,
		})
		if err != nil {
			logger.Error("failed to connect Kafka for event adapter", "error", err)
			os.Exit(1)
		}
		evtAdapter = ka
	case "nats":
		na, err := natsEvent.Open(natsEvent.Options{URL: cfg.NATSURL, Logger: logger})
		if err != nil {
			logger.Error("failed to connect NATS for event adapter", "error", err)
			os.Exit(1)
		}
		evtAdapter = na
	case "rabbitmq":
		ra, err := rabbitEvent.Open(rabbitEvent.Options{URL: cfg.RabbitURL, Logger: logger})
		if err != nil {
			logger.Error("failed to connect RabbitMQ for event adapter", "error", err)
			os.Exit(1)
		}
		evtAdapter = ra
	default:
		logger.Error("unknown event adapter (expected 'http', 'redis', 'kafka', 'nats' or 'rabbitmq')", "adapter", cfg.EventAdapter)
		os.Exit(1)
	}

	// 消息总线观测 (TODO 观测深化): wrap the adapter with per-topic
	// publish/consume/in-flight counters — the data face for the admin bus
	// panel. Wrapped before any subscriber or publisher is wired, so every
	// event the process handles is counted exactly once.
	evtCounting := event.WrapCounting(evtAdapter)
	evtAdapter = evtCounting

	// Lightweight series sampler (概览负载时间视图 / 消息总线曲线): 15s
	// samples, ≥10h retention, in-memory rings — no TSDB by decree. Probes:
	// fleet load per server/region/fleet, bus counters per topic.
	sampler := telemetry.NewSampler(15*time.Second, 10*time.Hour+5*time.Minute)
	sampler.Probe(func() map[string]float64 {
		out := map[string]float64{}
		// Fleet / region / per-server load scopes from the same index every
		// admin view reads (BUGS ③): drill-down and overview agree.
		type agg struct {
			players, loadSum float64
			n                int
		}
		regions := map[string]*agg{}
		fleet := &agg{}
		for _, srv := range fleetIdx.ListAll() {
			out["load.server."+srv.ID+".players"] = float64(srv.Players)
			out["load.server."+srv.ID+".load"] = srv.Load
			rg := regions[srv.Region]
			if rg == nil {
				rg = &agg{}
				regions[srv.Region] = rg
			}
			rg.players += float64(srv.Players)
			rg.loadSum += srv.Load
			rg.n++
			fleet.players += float64(srv.Players)
			fleet.loadSum += srv.Load
			fleet.n++
		}
		fleetScope := func(prefix string, a *agg) {
			out[prefix+".players"] = a.players
			if a.n > 0 {
				out[prefix+".load"] = a.loadSum / float64(a.n)
			}
		}
		fleetScope("load.fleet", fleet)
		for region, a := range regions {
			fleetScope("load.region."+region, a)
		}
		for _, st := range evtCounting.Stats() {
			out["bus."+st.Topic+".depth"] = float64(st.InFlight)
			out["bus."+st.Topic+".produced"] = float64(st.Published)
			out["bus."+st.Topic+".consumed"] = float64(st.Consumed)
		}
		return out
	})
	samplerCtx, samplerCancel := context.WithCancel(context.Background())
	defer samplerCancel()
	go sampler.Run(samplerCtx)

	evtCtx, evtCancel := context.WithCancel(context.Background())
	defer evtCancel()
	if err := evtAdapter.Subscribe(evtCtx, event.TopicCharacters, func(ctx context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(ctx, e)
		return err
	}); err != nil {
		logger.Error("failed to subscribe event adapter", "error", err)
		os.Exit(1)
	}

	// Create composite store for health checks / ping / close.
	composite := &compositeStore{
		ServerStore:            serverStore,
		CharacterStore:         charStore,
		RuntimeStore:           runtimeStore,
		MigrationStore:         migrationStore,
		StatsStore:             statsStore,
		RealmStore:             realmStore,
		ShardStore:             shardStore,
		MaintenanceWindowStore: mainWinStore,
		AnnouncementStore:      annStore,
		CrossServerConfigStore: crossCfgStore,
	}

	// Cross-server config center (docs/config-center.md): hosts the versioned
	// coordination config and signals changes over the same event adapter
	// (subscribe mode) plus callback POSTs (callback mode).
	crossSvc := crossserver.New(crossCfgStore, serverStore, evtAdapter, logger).WithPublicURL(cfg.PublicURL)

	admSvc := admin.New(composite)

	// Prometheus instrumentation (TODO v0.1.3).
	prom := metrics.New(composite)
	discSvc.WithMetrics(prom)
	dirSvc.WithMetrics(prom)
	regSvc.WithMetrics(prom)

	// Start health monitor.
	monitor := health.New(composite, cfg.SuspectAfter, cfg.OfflineAfter, cfg.HealthInterval, logger).WithMetrics(prom)
	if cfg.AlertSuspectRatio > 0 || cfg.AlertOfflineRatio > 0 {
		monitor = monitor.WithAlerts(health.NewAlerter(health.AlertConfig{
			SuspectRatio: cfg.AlertSuspectRatio,
			OfflineRatio: cfg.AlertOfflineRatio,
			WebhookURL:   cfg.AlertWebhookURL,
		}, logger))
	}
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	go monitor.Run(monitorCtx)

	// Set up HTTP handlers. 维护前引导: the routing service reads
	// maintenance windows so recommendations never land players on a server
	// inside — or minutes from — a maintenance window. The lead horizon is
	// tuned via ATLAS_ROUTING_MAINTENANCE_LEAD (default 5m in the service).
	rtSvc := routing.New(serverStore, runtimeStore, charStore, composite)
	if cfg.RoutingMaintenanceLead > 0 {
		rtSvc = rtSvc.WithMaintenanceLead(cfg.RoutingMaintenanceLead)
	}
	handler := httpapi.New(regSvc, discSvc, dirSvc, admSvc, rtSvc, crossSvc, composite, evtAdapter, logger).
		WithFleetIndex(fleetIdx).
		WithTelemetry(sampler).
		WithBusStats(evtCounting)

	// Parse auth config.
	adminKeys := httpapi.ParseAPIKeys(cfg.AdminAPIKeys)
	adminIPs, _ := httpapi.ParseIPWhitelist(cfg.AdminIPWhitelist)
	adminRoles, err := httpapi.ParseAdminRoles(cfg.AdminRoles)
	if err != nil {
		logger.Error("invalid admin roles", "error", err)
		os.Exit(1)
	}
	regTokens := httpapi.ParseAPIKeys(cfg.RegistryTokens)
	regIPs, _ := httpapi.ParseIPWhitelist(cfg.RegistryIPWhitelist)

	// Registry TLS / mTLS (TODO v0.1.17).
	regTLSOpts := tlsutil.Options{
		CertFile:     cfg.RegistryTLSCert,
		KeyFile:      cfg.RegistryTLSKey,
		ClientCAFile: cfg.RegistryClientCA,
	}
	var regTLS *tls.Config
	if regTLSOpts.Enabled() {
		regTLS, err = tlsutil.ServerConfig(tlsutil.Options{
			CertFile:     cfg.RegistryTLSCert,
			KeyFile:      cfg.RegistryTLSKey,
			ClientCAFile: cfg.RegistryClientCA,
		})
		if err != nil {
			logger.Error("invalid registry TLS config", "error", err)
			os.Exit(1)
		}
		if cfg.RegistryClientCA != "" {
			logger.Info("registry mTLS enabled (client certificates required)")
		} else {
			logger.Info("registry TLS enabled")
		}
	}

	// Admin API audit log (TODO v0.1.17).
	var auditLog *httpapi.AuditLog
	if cfg.AuditEnabled {
		auditLog = httpapi.NewAuditLog(1000, 4096, logger)
		handler = handler.WithAudit(auditLog)
	}

	// Rate limiting (TODO v0.1.17): opt-in via ATLAS_RATE_LIMITS or
	// ATLAS_RATE_LIMIT_DEFAULT.
	var limiter *httpapi.RateLimiter
	if cfg.RateLimits != "" || cfg.RateLimitDefault != "" {
		rules, err := httpapi.ParseRateLimitRules(cfg.RateLimits)
		if err != nil {
			logger.Error("invalid rate limit rules", "error", err)
			os.Exit(1)
		}
		def := httpapi.RateRule{RPS: 100, Burst: 200}
		if cfg.RateLimitDefault != "" {
			def, err = httpapi.ParseRateDefault(cfg.RateLimitDefault)
			if err != nil {
				logger.Error("invalid rate limit default", "error", err)
				os.Exit(1)
			}
		}
		limiter = httpapi.NewRateLimiter(rules, def)
		logger.Info("rate limiting enabled", "rules", len(rules), "default_rps", def.RPS, "default_burst", def.Burst)
		handler.WithRateLimiter(limiter) // read-only rules view on /v1/admin/rate-limits
	}

	// ── Public API (Discovery + Directory) ─────────────────
	publicMux := http.NewServeMux()
	handler.RegisterPublicRoutes(publicMux)
	publicMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	// CORS outermost: preflight clears before anything else, and only for
	// origins ATLAS_CORS_ORIGINS explicitly allows (empty config = no CORS
	// headers at all).
	publicHandler := httpapi.CORSMiddleware(cfg.CORSAllowedOrigins)(httpapi.Tracing(logger)(publicMux))
	publicSrv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      publicHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Registry API (internal network) ────────────────────
	// No CORS middleware here: this port serves game servers (machine
	// traffic), never browsers.
	regMux := http.NewServeMux()
	handler.RegisterRegistryRoutes(regMux)
	regMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	var regHandler http.Handler = http.Handler(regMux)
	if len(regTokens) > 0 || len(regIPs) > 0 {
		regHandler = httpapi.RegistryAuth(httpapi.RegistryAuthConfig{
			Tokens:      regTokens,
			IPWhitelist: regIPs,
			Logger:      logger,
		})(regHandler)
	}
	if limiter != nil {
		regHandler = limiter.Middleware(regHandler)
	}
	// Tracing outermost: register/heartbeat calls rejected by auth or the
	// rate limiter are traced too.
	regHandler = httpapi.Tracing(logger)(regHandler)
	regSrv := &http.Server{
		Addr:         cfg.RegistryAddr,
		Handler:      regHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Admin API (management network) ─────────────────────
	adminMux := http.NewServeMux()
	handler.RegisterAdminRoutes(adminMux)
	adminMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := composite.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not ready","error":"` + err.Error() + `"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	adminMux.Handle("GET /metrics", prom.Handler())

	var adminHandler http.Handler = http.Handler(adminMux)
	// Audit inside auth (records the resolved actor), rate limit outside
	// (cheap per-IP rejection before any key check).
	if auditLog != nil {
		adminHandler = auditLog.Middleware(adminHandler)
	}
	if len(adminKeys) > 0 || len(adminIPs) > 0 {
		adminHandler = httpapi.AdminAuth(httpapi.AuthConfig{
			APIKeys:     adminKeys,
			Roles:       adminRoles,
			IPWhitelist: adminIPs,
			Logger:      logger,
		})(adminHandler)
	}
	if limiter != nil {
		adminHandler = limiter.Middleware(adminHandler)
	}
	adminHandler = metrics.RequestCounter(prom.AdminRequests)(adminHandler)
	adminHandler = httpapi.Tracing(logger)(adminHandler)
	// CORS outermost (outside auth): preflight OPTIONS carries no admin
	// key, and the dashboard dev server needs it answered before the
	// browser will send the real request.
	adminHandler = httpapi.CORSMiddleware(cfg.CORSAllowedOrigins)(adminHandler)
	adminSrv := &http.Server{
		Addr:         cfg.AdminAddr,
		Handler:      adminHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── gRPC API (all five services, TODO v0.1.5) ──────────
	// Same security domains as REST (docs/api.md §认证): Registry and
	// Admin RPCs are guarded when tokens/keys are configured, unconfigured
	// stays the open dev mode. The Go SDK already sends the right
	// credential per call over gRPC (grpc.go callCtx). Audit rides the
	// same chain, after auth (audit inside auth, like the REST admin mux):
	// Admin RPCs land in the same ring as REST operations.
	var grpcInterceptors []grpc.UnaryServerInterceptor
	if len(regTokens) > 0 || len(regIPs) > 0 || len(adminKeys) > 0 || len(adminIPs) > 0 {
		grpcInterceptors = append(grpcInterceptors, atlasgrpc.UnaryAuth(atlasgrpc.AuthConfig{
			RegistryTokens: regTokens,
			RegistryIPs:    regIPs,
			AdminKeys:      adminKeys,
			AdminRoles:     adminRoles,
			AdminIPs:       adminIPs,
			Logger:         logger,
		}))
	}
	if auditLog != nil {
		grpcInterceptors = append(grpcInterceptors, atlasgrpc.UnaryAudit(auditLog))
	}
	var grpcSrv *grpc.Server
	if len(grpcInterceptors) > 0 {
		grpcSrv = grpc.NewServer(grpc.ChainUnaryInterceptor(grpcInterceptors...))
	} else {
		grpcSrv = grpc.NewServer()
	}
	atlasgrpc.New(regSvc, discSvc, dirSvc, rtSvc, admSvc, evtAdapter).RegisterServices(grpcSrv)

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("public API listening", "addr", cfg.HTTPAddr)
		if err := publicSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("public API error", "error", err)
			os.Exit(1)
		}
	}()
	go func() {
		scheme := "http"
		if regTLS != nil {
			scheme = "tls"
		}
		logger.Info("registry API listening", "addr", cfg.RegistryAddr, "scheme", scheme)
		regLn, err := net.Listen("tcp", cfg.RegistryAddr)
		if err != nil {
			logger.Error("registry API listen failed", "error", err)
			os.Exit(1)
		}
		if regTLS != nil {
			regLn = tls.NewListener(regLn, regTLS)
		}
		if err := regSrv.Serve(regLn); err != nil && err != http.ErrServerClosed {
			logger.Error("registry API error", "error", err)
			os.Exit(1)
		}
	}()
	go func() {
		logger.Info("admin API listening", "addr", cfg.AdminAddr)
		if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("admin API error", "error", err)
			os.Exit(1)
		}
	}()
	go func() {
		if cfg.GRPCAddr == "" {
			logger.Info("gRPC API disabled (ATLAS_GRPC_ADDR empty)")
			return
		}
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			logger.Error("gRPC listen error", "addr", cfg.GRPCAddr, "error", err)
			os.Exit(1)
		}
		logger.Info("gRPC API listening", "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("gRPC API error", "error", err)
		}
	}()

	sig := <-sigCh
	logger.Info("shutting down", "signal", sig)

	// Stop health monitor.
	monitorCancel()

	// Shutdown all HTTP servers with a timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	publicSrv.Shutdown(shutdownCtx)
	regSrv.Shutdown(shutdownCtx)
	adminSrv.Shutdown(shutdownCtx)

	// Graceful-stop gRPC (fall back to hard stop on timeout).
	grpcDone := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(grpcDone)
	}()
	select {
	case <-grpcDone:
	case <-shutdownCtx.Done():
		grpcSrv.Stop()
	}

	// Stop event pipeline.
	evtCancel()
	evtAdapter.Close()
	if evtRdb != nil {
		evtRdb.Close()
	}

	// Close stores.
	if pingCloser != nil {
		pingCloser()
	}

	logger.Info("Atlas stopped")
}

// applyPGPoolOptions layers ATLAS_PG_POOL_* tuning on top of the DSN
// parsed config. Zero values leave the library defaults in place
// (TODO v0.1.19).
func applyPGPoolOptions(poolCfg *pgxpool.Config, cfg config.Config) {
	if cfg.PGPoolMaxConns > 0 {
		poolCfg.MaxConns = int32(cfg.PGPoolMaxConns)
	}
	if cfg.PGPoolMinConns > 0 {
		poolCfg.MinConns = int32(cfg.PGPoolMinConns)
	}
	if cfg.PGPoolMaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.PGPoolMaxConnLifetime
	}
	if cfg.PGPoolMaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.PGPoolMaxConnIdleTime
	}
	if cfg.PGPoolHealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.PGPoolHealthCheckPeriod
	}
	// MinConns must not exceed MaxConns; pgx would otherwise error at connect.
	if poolCfg.MinConns > 0 && poolCfg.MaxConns > 0 && poolCfg.MinConns > poolCfg.MaxConns {
		poolCfg.MinConns = poolCfg.MaxConns
	}
}

// redisTopology describes the resolved Redis deployment shape.
type redisTopology struct {
	mode                     string // "cluster", "sentinel" or "single"
	seeds                    []string
	master                   string
	addr, username, password string
	db                       int
	poolSize                 int
}

// resolveRedisTopology picks the Redis topology from config, in precedence
// order: cluster seeds > Sentinel failover > single node from
// ATLAS_REDIS_URL (TODO v0.1.19). Password and logical DB from the URL
// apply to every mode.
func resolveRedisTopology(cfg config.Config) redisTopology {
	topo := redisTopology{
		mode:     "single",
		poolSize: cfg.RedisPoolSize,
	}
	topo.addr, topo.username, topo.password, topo.db = redisURLParts(cfg.RedisURL)

	if seeds := splitAddrs(cfg.RedisClusterAddrs); len(seeds) > 0 {
		topo.mode = "cluster"
		topo.seeds = seeds
		return topo
	}
	if sentinels := splitAddrs(cfg.RedisSentinelAddrs); len(sentinels) > 0 {
		topo.mode = "sentinel"
		topo.seeds = sentinels
		topo.master = cfg.RedisMasterName
	}
	return topo
}

// buildRedisClient instantiates the go-redis client for the resolved
// topology. The returned client is owned by the caller.
func buildRedisClient(cfg config.Config) redis.UniversalClient {
	topo := resolveRedisTopology(cfg)
	switch topo.mode {
	case "cluster":
		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    topo.seeds,
			Username: topo.username,
			Password: topo.password,
			PoolSize: topo.poolSize,
		})
	case "sentinel":
		return redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    topo.master,
			SentinelAddrs: topo.seeds,
			Username:      topo.username,
			Password:      topo.password,
			DB:            topo.db,
			PoolSize:      topo.poolSize,
		})
	default:
		return redis.NewClient(&redis.Options{
			Addr:     topo.addr,
			Username: topo.username,
			Password: topo.password,
			DB:       topo.db,
			PoolSize: topo.poolSize,
		})
	}
}

// splitAddrs splits a comma-separated address list, dropping blanks.
func splitAddrs(raw string) []string {
	var out []string
	for _, a := range strings.Split(raw, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// redisURLParts splits a redis://[user:pass@]host:port/db URL into go-redis
// connection parts, with localhost:6379 / anonymous / DB 0 defaults.
func redisURLParts(rawURL string) (addr, username, password string, db int) {
	addr = "localhost:6379"
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	host := u.Hostname()
	port := u.Port()
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "6379"
	}
	addr = host + ":" + port
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	if p := strings.TrimPrefix(u.Path, "/"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n >= 0 {
			db = n
		}
	}
	return
}

// compositeStore combines separate store implementations into a single store.Store.
type compositeStore struct {
	store.ServerStore
	store.CharacterStore
	store.RuntimeStore
	store.MigrationStore
	store.StatsStore
	store.RealmStore
	store.ShardStore
	store.MaintenanceWindowStore
	store.AnnouncementStore
	store.CrossServerConfigStore
}

func (c *compositeStore) Ping(ctx context.Context) error {
	// Ping the server store (PostgreSQL) as the primary health check.
	if pinger, ok := c.ServerStore.(interface{ Ping(context.Context) error }); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *compositeStore) Close() error {
	var firstErr error
	type closer interface {
		Close() error
	}
	if cl, ok := c.ServerStore.(closer); ok {
		if err := cl.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if cl, ok := c.RuntimeStore.(closer); ok {
		if err := cl.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Suppress unused import warnings for packages only used in specific code paths.
var (
	_ = strings.Join
	_ = pgxpool.ParseConfig
	_ = redis.NewClient
)

// runHealthcheck probes the public API's /healthz and exits 0 on success.
// It is the container health probe: the distroless runtime image has no
// shell, so the compose healthcheck calls the binary itself. Honors
// ATLAS_HTTP_ADDR to follow the same bind address as the server.
func runHealthcheck() int {
	addr := os.Getenv("ATLAS_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: /healthz returned %s\n", resp.Status)
		return 1
	}
	return 0
}
