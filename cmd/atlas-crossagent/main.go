// atlas-crossagent is a reference game-server agent for the cross-server
// config center (docs/config-center.md). It demonstrates the full receiver
// contract against a real Atlas:
//
//   - startup pull with fail-fast (cannot read config → loud exit),
//   - notify-then-pull updates in all three modes:
//     subscribe (message bus), callback (atlas POSTs a signal URL the
//     agent exposes) and poll (version polling fallback),
//   - version-guarded application (older/duplicate snapshots refused),
//   - keep running on the previous config when a pull fails, with
//     backoff retries and an alarm log,
//   - re-subscribe + immediate re-pull after a bus reconnect.
//
// Fault-injection control endpoints make the failure semantics walkable
// without touching Atlas:
//
//	POST /ctl/failpull  {"on":true|false}  — make every config pull fail
//	POST /ctl/bus-off                      — drop the bus subscription
//	                                       (updates missed until reset)
//	POST /ctl/bus-reset                    — rebuild the subscription, then
//	                                       re-pull once (断连补拉)
//	GET  /ctl/status                        — current snapshot + counters
//
// The same listener serves POST /notify, the callback-mode signal URL.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/event"
	redisEvent "github.com/cuihairu/atlas/internal/event/redis"
	"github.com/cuihairu/atlas/internal/model"
	atlas "github.com/cuihairu/atlas/sdk/go/atlas"
)

func main() {
	var (
		serverID     = flag.String("server-id", "", "server id to register as (required)")
		name         = flag.String("name", "", "display name")
		region       = flag.String("region", "cn-east", "region")
		atlasAddr    = flag.String("atlas", "http://127.0.0.1:8081", "atlas registry base URL")
		notifyMode   = flag.String("notify-mode", "poll", "subscribe | callback | poll")
		callbackAddr = flag.String("http", "", "agent listener addr (callback URL + /ctl endpoints), e.g. :9099")
		publicURL    = flag.String("public-url", "", "externally reachable base of -http (callback mode), e.g. http://127.0.0.1:9099")
		redisURL     = flag.String("redis-url", "redis://127.0.0.1:6379", "redis for subscribe mode")
		pollInterval = flag.Duration("poll-interval", 15*time.Second, "fallback poll interval (0 disables)")
		heartbeatIn  = flag.Duration("heartbeat-interval", 10*time.Second, "heartbeat interval")
		verbose      = flag.Bool("v", false, "debug logging")
	)
	flag.Parse()
	if *serverID == "" {
		fmt.Fprintln(os.Stderr, "atlas-crossagent: -server-id is required")
		flag.Usage()
		os.Exit(2)
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	if *notifyMode != atlas.NotifyModeSubscribe && *notifyMode != atlas.NotifyModeCallback && *notifyMode != atlas.NotifyModePoll {
		fmt.Fprintf(os.Stderr, "atlas-crossagent: invalid -notify-mode %q\n", *notifyMode)
		os.Exit(2)
	}
	if *notifyMode == atlas.NotifyModeCallback && (*publicURL == "" || *callbackAddr == "") {
		fmt.Fprintln(os.Stderr, "atlas-crossagent: callback mode requires -public-url and -http")
		os.Exit(2)
	}
	if *notifyMode == atlas.NotifyModeSubscribe && *callbackAddr == "" {
		// subscribe mode still wants the /ctl fault-injection endpoints.
		fmt.Fprintln(os.Stderr, "atlas-crossagent: subscribe mode requires -http for /ctl (use a spare port)")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Fault-injection switch on the transport: /ctl/failpull makes every
	// config pull fail at the HTTP layer, exercising the retry + backoff +
	// keep-old-config path of the watcher.
	var failPull atomic.Bool
	httpClient := &http.Client{Timeout: 5 * time.Second}
	base := httpClient.Transport
	httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if failPull.Load() && r.URL.Path == "/v1/crossserver/config" {
			return nil, errors.New("fault injection: config pull disabled")
		}
		if base != nil {
			return base.RoundTrip(r)
		}
		return http.DefaultTransport.RoundTrip(r)
	})

	client, err := atlas.New(atlas.Options{
		Addr:         *atlasAddr,
		RegistryAddr: *atlasAddr,
		HTTPClient:   httpClient,
	})
	if err != nil {
		log.Error("client init failed", "error", err)
		os.Exit(1)
	}

	watcher := atlas.NewConfigWatcher(client, atlas.ConfigWatcherOptions{
		ServerID: *serverID,
		// The flag says 0 disables the periodic poll; the watcher reads 0
		// as "default", so translate explicitly.
		PollInterval: func() time.Duration {
			if *pollInterval == 0 {
				return -1
			}
			return *pollInterval
		}(),
		MaxBackoff: 5 * time.Second,
		OnApply: func(cfg *atlas.CrossServerConfig) {
			// "热生效" in the reference agent: swap the in-memory view and
			// log. A real game server re-reads its cross-server modules
			// here — no restart, no re-register.
			features := 0
			for _, on := range cfg.Spec.Features {
				if on {
					features++
				}
			}
			log.Info("CONFIG APPLIED (hot)",
				"version", cfg.Version, "hash", cfg.Hash,
				"clusters", len(cfg.Spec.Topology.Clusters),
				"groups", len(cfg.Spec.Groups),
				"features_on", features,
				"match_domains", len(cfg.Spec.MatchDomains))
		},
	})

	// Register with the notify declaration (callback mode carries the
	// signal URL; subscribe mode just declares the mode — the subscription
	// is the server's own long connection, see below).
	regReq := atlas.RegisterRequest{
		ServerID: *serverID,
		Name:     *name,
		Type:     "game",
		Region:   *region,
		Version:  "1.0.0",
		Platform: "pc",
		Endpoint: atlas.Endpoint{Host: "127.0.0.1", Port: 30001},
		Capacity: 500,
		NotifyMode: func() string {
			switch *notifyMode {
			case atlas.NotifyModeSubscribe:
				return atlas.NotifyModeSubscribe
			case atlas.NotifyModeCallback:
				return atlas.NotifyModeCallback
			default:
				return atlas.NotifyModePoll
			}
		}(),
	}
	if *notifyMode == atlas.NotifyModeCallback {
		regReq.NotifyCallbackURL = *publicURL + "/notify"
	}
	reg, err := client.Register(ctx, regReq)
	if err != nil {
		log.Error("register failed", "error", err)
		os.Exit(1)
	}
	regCfgVer := 0
	if reg.CrossServerConfig != nil {
		regCfgVer = reg.CrossServerConfig.Version
	}
	log.Info("registered",
		"server_id", reg.ServerID, "status", reg.Status, "notify_mode", regReq.NotifyMode,
		"config_version", regCfgVer)

	// Startup pull: fail fast and loudly when the config cannot be read
	// (3 attempts, backoff). The process exits non-zero — an operator sees
	// the boot failure instead of players discovering missing cross-server
	// features.
	runErr := make(chan error, 1)
	go func() { runErr <- watcher.Run(ctx, 3) }()
	go func() {
		if err := <-runErr; err != nil {
			log.Error("STARTUP PULL FAILED — refusing to start", "error", err)
			stop()
		}
	}()

	// Subscribe mode: long-lived bus subscription. Receiving the signal is
	// exactly watcher.Notify() — the pull (and every guard) lives in the
	// watcher. Reconnect (manual /ctl/bus-reset or redis auto-reconnect)
	// re-subscribes and immediately re-pulls once so the disconnect window
	// cannot lose an update.
	var bus io.Closer
	var busRdb io.Closer
	// busDrop ends the subscription without rebuilding it — the
	// fault-injection twin of bus-reset, used to walk the "an update landed
	// while I was disconnected" window (断连→改配置→重连→补拉).
	busDrop := func() {
		if bus != nil {
			bus.Close()
			bus = nil
		}
		if busRdb != nil {
			busRdb.Close()
			busRdb = nil
		}
	}
	busReset := func() error {
		busDrop()
		if *notifyMode != atlas.NotifyModeSubscribe {
			return nil
		}
		opts, err := redis.ParseURL(*redisURL)
		if err != nil {
			return err
		}
		rdb := redis.NewClient(opts)
		if err := rdb.Ping(ctx).Err(); err != nil {
			rdb.Close()
			return err
		}
		adapter := redisEvent.New(rdb, redisEvent.Options{
			Group:    "crossagent-" + *serverID,
			Consumer: *serverID,
			Logger:   log,
		})
		if err := adapter.Subscribe(ctx, event.TopicConfig, func(_ context.Context, e *event.Event) error {
			log.Info("bus signal received",
				"type", string(e.Type), "version", e.ConfigVersion, "hash", e.ConfigHash,
				"targets", e.ConfigTargets, "receivers", e.ConfigServers)
			// 定向投递: pull only when the signal names this server (or
			// everyone). Atlas computes the receiver list from the config
			// before AND after the change, so both "just joined a group"
			// and "just left one" reach this server.
			if addressed(e.ConfigServers, *serverID) {
				watcher.Notify()
				return nil
			}
			log.Info("signal ignored (not a receiver)", "version", e.ConfigVersion,
				"targets", e.ConfigTargets, "receivers", e.ConfigServers)
			return nil
		}); err != nil {
			rdb.Close()
			return err
		}
		bus = adapter
		busRdb = rdb
		log.Info("subscribed to config topic", "topic", event.TopicConfig, "group", "crossagent-"+*serverID)
		// 补拉: an update may have landed while the subscription was down.
		watcher.Notify()
		return nil
	}
	if err := busReset(); err != nil {
		log.Error("bus subscribe failed", "error", err)
		if *notifyMode == atlas.NotifyModeSubscribe {
			os.Exit(1)
		}
	}

	// Heartbeat loop: proof of life, independent of the config center.
	// The first success after failures re-pulls the config: while
	// heartbeats fail Atlas marks the server offline and stops naming it
	// as a signal receiver (下线即退订), so an update landed in that window
	// is picked up as soon as registration comes back (补拉).
	var hbFailed atomic.Bool
	go func() {
		tick := time.NewTicker(*heartbeatIn)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				res, err := client.Heartbeat(ctx, *serverID, atlas.HeartbeatRequest{Players: 42, Load: 0.3})
				if err != nil {
					hbFailed.Store(true)
					log.Warn("heartbeat failed", "error", err)
					continue
				}
				if hbFailed.CompareAndSwap(true, false) {
					log.Info("heartbeat recovered — re-pulling config (补拉)")
					watcher.Notify()
				}
				log.Debug("heartbeat", "status", res.Status)
			}
		}
	}()

	// Agent listener: /notify (callback mode) + /ctl/* (fault injection).
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notify", func(w http.ResponseWriter, r *http.Request) {
		var sig struct {
			Type    string `json:"type"`
			Version int    `json:"version"`
			Hash    string `json:"hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&sig); err != nil {
			http.Error(w, "bad signal", http.StatusBadRequest)
			return
		}
		log.Info("callback signal received", "type", sig.Type, "version", sig.Version, "hash", sig.Hash)
		watcher.Notify() // the pull (with version guards) happens in the watcher
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /notify", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "atlas-crossagent notify endpoint (POST)")
	})
	mux.HandleFunc("POST /ctl/failpull", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On bool `json:"on"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		failPull.Store(body.On)
		log.Warn("FAULT INJECTION", "config_pull_failing", body.On)
		fmt.Fprintf(w, "failpull=%v\n", body.On)
	})
	mux.HandleFunc("POST /ctl/bus-reset", func(w http.ResponseWriter, r *http.Request) {
		if err := busReset(); err != nil {
			log.Error("bus reset failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "bus re-subscribed + re-pull triggered")
	})
	mux.HandleFunc("POST /ctl/bus-off", func(w http.ResponseWriter, r *http.Request) {
		busDrop()
		log.Warn("FAULT INJECTION", "bus_subscription", "dropped")
		fmt.Fprintln(w, "bus subscription dropped (changes missed until /ctl/bus-reset)")
	})
	mux.HandleFunc("GET /ctl/status", func(w http.ResponseWriter, r *http.Request) {
		cur := watcher.Current()
		applied, idempotent, skipped, failures := watcher.Stats()
		resp := map[string]any{
			"server_id":     *serverID,
			"notify_mode":   regReq.NotifyMode,
			"pull_failing":  failPull.Load(),
			"applied":       applied,
			"idempotent":    idempotent,
			"stale_skipped": skipped,
			"pull_failures": failures,
		}
		if cur != nil {
			resp["config_version"] = cur.Version
			resp["config_hash"] = cur.Hash
		} else {
			resp["config_version"] = nil
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	agentSrv := &http.Server{Addr: *callbackAddr, Handler: mux}
	go func() {
		log.Info("agent listener up", "addr", *callbackAddr)
		if err := agentSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("agent listener failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = agentSrv.Shutdown(shutdownCtx)
	if bus != nil {
		bus.Close()
	}
	if busRdb != nil {
		busRdb.Close()
	}
	if _, err := client.Unregister(context.Background(), *serverID); err != nil {
		log.Warn("unregister failed", "error", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// addressed reports whether a bus signal names this server. The receiver
// list is computed by Atlas from the config before and after the change;
// an empty list means no live server is affected — nothing to pull.
func addressed(receivers []string, serverID string) bool {
	for _, r := range receivers {
		if r == model.TargetAll || r == serverID {
			return true
		}
	}
	return false
}
