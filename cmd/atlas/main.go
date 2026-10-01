// Atlas is a game server registry, discovery, and character directory service.
//
// See https://github.com/cuihairu/atlas for documentation.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	atlasgrpc "github.com/cuihairu/atlas/internal/grpc"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/config"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpEvent "github.com/cuihairu/atlas/internal/event/http"
	kafkaEvent "github.com/cuihairu/atlas/internal/event/kafka"
	natsEvent "github.com/cuihairu/atlas/internal/event/nats"
	rabbitEvent "github.com/cuihairu/atlas/internal/event/rabbitmq"
	redisEvent "github.com/cuihairu/atlas/internal/event/redis"
	"github.com/cuihairu/atlas/internal/health"
	"github.com/cuihairu/atlas/internal/httpapi"
	"github.com/cuihairu/atlas/internal/metrics"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
	pgStore "github.com/cuihairu/atlas/internal/store/postgres"
	redisStore "github.com/cuihairu/atlas/internal/store/redisstore"
	"github.com/cuihairu/atlas/internal/version"
)

func main() {
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

	case "postgres":
		// PostgreSQL for server + character storage.
		poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			logger.Error("invalid database URL", "error", err)
			os.Exit(1)
		}
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

		// Redis for runtime/heartbeat storage.
		redisAddr := parseRedisAddr(cfg.RedisURL)
		rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Error("failed to ping Redis", "error", err)
			os.Exit(1)
		}
		rs := redisStore.New(rdb)
		runtimeStore = rs

		pingCloser = func() {
			pool.Close()
			rdb.Close()
		}

	default:
		logger.Error("unknown store type (expected 'memory' or 'postgres')", "type", cfg.StoreType)
		os.Exit(1)
	}

	// Create services.
	regSvc := registry.New(serverStore, runtimeStore, logger)
	discSvc := discovery.New(serverStore, runtimeStore)
	dirSvc := directory.New(charStore)

	// Event adapter: transports character index writes into the directory
	// projection (docs/sync.md §4).
	var evtAdapter event.EventAdapter
	var evtRdb *redis.Client
	switch cfg.EventAdapter {
	case "", "http":
		evtAdapter = httpEvent.New()
	case "redis":
		evtRdb = redis.NewClient(&redis.Options{Addr: parseRedisAddr(cfg.RedisURL)})
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
		ServerStore:    serverStore,
		CharacterStore: charStore,
		RuntimeStore:   runtimeStore,
		MigrationStore: migrationStore,
		StatsStore:     statsStore,
	}

	admSvc := admin.New(composite)

	// Prometheus instrumentation (TODO v0.1.3).
	prom := metrics.New(composite)
	discSvc.WithMetrics(prom)

	// Start health monitor.
	monitor := health.New(composite, cfg.SuspectAfter, cfg.OfflineAfter, cfg.HealthInterval, logger).WithMetrics(prom)
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	go monitor.Run(monitorCtx)

	// Set up HTTP handlers.
	rtSvc := routing.New(serverStore, runtimeStore, charStore)
	handler := httpapi.New(regSvc, discSvc, dirSvc, admSvc, rtSvc, composite, evtAdapter, logger)

	// Parse auth config.
	adminKeys := httpapi.ParseAPIKeys(cfg.AdminAPIKeys)
	adminIPs, _ := httpapi.ParseIPWhitelist(cfg.AdminIPWhitelist)
	regTokens := httpapi.ParseAPIKeys(cfg.RegistryTokens)
	regIPs, _ := httpapi.ParseIPWhitelist(cfg.RegistryIPWhitelist)

	// ── Public API (Discovery + Directory) ─────────────────
	publicMux := http.NewServeMux()
	handler.RegisterPublicRoutes(publicMux)
	publicMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	publicSrv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      publicMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Registry API (internal network) ────────────────────
	regMux := http.NewServeMux()
	handler.RegisterRegistryRoutes(regMux)
	regMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	var regHandler http.Handler = regMux
	if len(regTokens) > 0 || len(regIPs) > 0 {
		regHandler = httpapi.RegistryAuth(httpapi.RegistryAuthConfig{
			Tokens:      regTokens,
			IPWhitelist: regIPs,
			Logger:      logger,
		})(regMux)
	}
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

	var adminHandler http.Handler = adminMux
	if len(adminKeys) > 0 || len(adminIPs) > 0 {
		adminHandler = httpapi.AdminAuth(httpapi.AuthConfig{
			APIKeys:     adminKeys,
			IPWhitelist: adminIPs,
			Logger:      logger,
		})(adminMux)
	}
	adminHandler = metrics.RequestCounter(prom.AdminRequests)(adminHandler)
	adminSrv := &http.Server{
		Addr:         cfg.AdminAddr,
		Handler:      adminHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── gRPC API (all five services, TODO v0.1.5) ──────────
	grpcSrv := grpc.NewServer()
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
		logger.Info("registry API listening", "addr", cfg.RegistryAddr)
		if err := regSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

// parseRedisAddr extracts "host:port" from a redis:// URL.
func parseRedisAddr(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "localhost:6379"
	}
	host := u.Hostname()
	port := u.Port()
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "6379"
	}
	return host + ":" + port
}

// compositeStore combines separate store implementations into a single store.Store.
type compositeStore struct {
	store.ServerStore
	store.CharacterStore
	store.RuntimeStore
	store.MigrationStore
	store.StatsStore
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
