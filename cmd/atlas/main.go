// Atlas is a game server registry, discovery, and character directory service.
//
// See https://github.com/cuihairu/atlas for documentation.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/config"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/health"
	"github.com/cuihairu/atlas/internal/httpapi"
	"github.com/cuihairu/atlas/internal/registry"
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
		"addr", cfg.HTTPAddr,
	)

	// Create stores.
	var (
		serverStore  store.ServerStore
		charStore    store.CharacterStore
		runtimeStore store.RuntimeStore
		pingCloser   func()
	)

	ctx := context.Background()

	switch cfg.StoreType {
	case "memory":
		mem := memory.New()
		serverStore = mem
		charStore = mem
		runtimeStore = mem

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

	// Create composite store for health checks / ping / close.
	composite := &compositeStore{
		ServerStore:  serverStore,
		CharacterStore: charStore,
		RuntimeStore: runtimeStore,
	}

	// Create services.
	regSvc := registry.New(serverStore, runtimeStore, logger)
	discSvc := discovery.New(serverStore, runtimeStore)
	dirSvc := directory.New(charStore)

	// Start health monitor.
	monitor := health.New(composite, cfg.SuspectAfter, cfg.OfflineAfter, cfg.HealthInterval, logger)
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	go monitor.Run(monitorCtx)

	// Set up HTTP server.
	handler := httpapi.New(regSvc, discSvc, dirSvc, composite, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("HTTP server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	sig := <-sigCh
	logger.Info("shutting down", "signal", sig)

	// Stop health monitor.
	monitorCancel()

	// Shutdown HTTP server with a timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
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