// atlas-demoagents simulates a whole game-server fleet against a real Atlas.
//
// It exists so a demo deployment (docker compose / the demo domain) shows a
// live system instead of an empty dashboard: every fleet entry registers
// itself, keeps its own heartbeat, drifts its player count, writes demo
// characters into the directory index, and the entry named by -watch-server
// keeps a cross-server config watcher converged — so publishing a new config
// from the admin API is observable as a hot update in this process's log.
//
// The fleet is data, not code: -fleet loads a JSON file with the same schema
// as the built-in default (deployments/demo/fleet.json).
//
// Registry writes (register/heartbeat) and directory writes (characters) go
// through the Go SDK; ops-owned state (tags, maintenance) is seeded through
// the admin API exactly the way an operator would set it up by hand.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/event"
	redisEvent "github.com/cuihairu/atlas/internal/event/redis"
	"github.com/cuihairu/atlas/internal/model"
	atlas "github.com/cuihairu/atlas/sdk/go/atlas"
)

// fleetEntry is one simulated game server. Everything here is declarative:
// the live state (current player count, load) lives in liveState so the
// heartbeat goroutine and the status endpoint never share a field.
type fleetEntry struct {
	ServerID string         `json:"server_id"`
	Name     string         `json:"name"`
	Type     string         `json:"type,omitempty"`
	Region   string         `json:"region"`
	RealmID  string         `json:"realm_id,omitempty"`
	ShardID  string         `json:"shard_id,omitempty"`
	Version  string         `json:"version,omitempty"`
	Platform string         `json:"platform,omitempty"`
	Endpoint atlas.Endpoint `json:"endpoint"`
	Capacity int            `json:"capacity"`

	// Players is the baseline online count; Drift (percent of capacity) is
	// the amplitude of a slow sine around it. Drift 0 means a flat count.
	Players int `json:"players,omitempty"`
	Drift   int `json:"drift,omitempty"`

	// NotifyMode declares the cross-server config notification channel:
	// subscribe | callback | poll (empty = poll). See docs/config-center.md.
	NotifyMode string `json:"notify_mode,omitempty"`

	// Tags are ops-owned badges seeded through the admin API after
	// registration ({"code":"hot"}, or a full custom tag).
	Tags []demoTag `json:"tags,omitempty"`

	// SeedOnly registers the entry once and never heartbeats it, so a status
	// the demo then sets through the admin API (e.g. maintenance) sticks.
	SeedOnly bool `json:"seed_only,omitempty"`

	// Characters seeds the directory index for this server: one entry per
	// account, so the cross-server character view has data to show.
	Characters []demoCharacter `json:"characters,omitempty"`
}

type demoTag struct {
	Code   string `json:"code"`
	Label  string `json:"label,omitempty"`
	Tier   string `json:"tier,omitempty"`
	Public *bool  `json:"public,omitempty"`
}

type demoCharacter struct {
	AccountID   int64  `json:"account_id"`
	CharacterID int64  `json:"character_id"`
	Name        string `json:"name"`
	Level       int    `json:"level"`
	ClassID     int    `json:"class_id"`
}

// liveState is the mutable half of a fleet entry. phase/baseline/drift are
// only touched by the owning heartbeat goroutine; players/load are atomic
// because the status endpoint reads them.
type liveState struct {
	players  atomic.Int64
	loadBits atomic.Uint64
	phase    float64
	baseline int
	drift    int
	capacity int
}

func newLiveState(e *fleetEntry) *liveState {
	s := &liveState{baseline: e.Players, drift: e.Drift, capacity: e.Capacity}
	s.players.Store(int64(e.Players))
	s.loadBits.Store(math.Float64bits(ratio(e.Players, e.Capacity)))
	return s
}

func ratio(players, capacity int) float64 {
	if capacity <= 0 {
		return 0
	}
	return math.Round(float64(players)/float64(capacity)*100) / 100
}

// tick returns the next player count and load for a heartbeat. The count
// drifts on a slow sine around its baseline so the dashboard trend chart and
// the capacity bars move like a live fleet instead of sitting flat.
func (s *liveState) tick(interval time.Duration) (players int, load float64) {
	s.phase += interval.Seconds() / 30
	if s.drift != 0 {
		v := s.baseline + int(math.Round(float64(s.drift)*math.Sin(s.phase)))
		if v < 0 {
			v = 0
		}
		if v > s.capacity {
			v = s.capacity
		}
		s.players.Store(int64(v))
		load = ratio(v, s.capacity)
		s.loadBits.Store(math.Float64bits(load))
	}
	return int(s.players.Load()), math.Float64frombits(s.loadBits.Load())
}

// defaultFleet mirrors the demo cluster used across the docs (screenshots and
// 场景导览 walkthroughs): 华东/华北/华南 game servers plus two cross-server
// config agent nodes, and one server parked in maintenance.
func defaultFleet() []fleetEntry {
	return []fleetEntry{
		{
			ServerID: "game-1001", Name: "一区·青龙", Region: "cn-east",
			RealmID: "realm-cn", ShardID: "shard-east", Version: "1.2.0",
			Endpoint: atlas.Endpoint{Host: "10.0.1.21", Port: 30001}, Capacity: 2000,
			NotifyMode: atlas.NotifyModeSubscribe,
			Players:    843, Drift: 40,
			Tags: []demoTag{{Code: "hot"}},
			Characters: []demoCharacter{
				{AccountID: 424242, CharacterID: 100101, Name: "青龙", Level: 58, ClassID: 1},
				{AccountID: 424242, CharacterID: 100102, Name: "青龙·影", Level: 41, ClassID: 2},
				{AccountID: 777001, CharacterID: 100103, Name: "云中君", Level: 63, ClassID: 3},
			},
		},
		{
			ServerID: "game-1002", Name: "二区·白虎", Region: "cn-east",
			RealmID: "realm-cn", ShardID: "shard-east", Version: "1.2.0",
			Endpoint: atlas.Endpoint{Host: "10.0.1.22", Port: 30002}, Capacity: 2000,
			Players: 1265, Drift: 60,
			Tags: []demoTag{{Code: "new"}, {Code: "recommended"}},
			Characters: []demoCharacter{
				{AccountID: 424242, CharacterID: 100201, Name: "白虎", Level: 12, ClassID: 4},
			},
		},
		{
			ServerID: "game-2001", Name: "华北·朱雀", Region: "cn-north",
			RealmID: "realm-cn", ShardID: "shard-north", Version: "1.2.0",
			Endpoint: atlas.Endpoint{Host: "10.0.2.11", Port: 30011}, Capacity: 1500,
			Players: 402, Drift: 25,
			Characters: []demoCharacter{
				{AccountID: 777001, CharacterID: 200101, Name: "朱雀", Level: 47, ClassID: 2},
			},
		},
		{
			ServerID: "game-2002", Name: "华北·玄武", Region: "cn-north",
			RealmID: "realm-cn", ShardID: "shard-north", Version: "1.1.9",
			Endpoint: atlas.Endpoint{Host: "10.0.2.12", Port: 30012}, Capacity: 1500,
			Players: 1498, Drift: 30,
			Tags:       []demoTag{{Code: "full"}},
			Characters: []demoCharacter{{AccountID: 888002, CharacterID: 200201, Name: "玄武", Level: 60, ClassID: 5}},
		},
		{
			ServerID: "game-3001", Name: "华南·麒麟", Region: "cn-south",
			Version:  "1.2.0",
			Endpoint: atlas.Endpoint{Host: "10.0.3.31", Port: 30031}, Capacity: 800,
			Players: 77, Drift: 15,
			Characters: []demoCharacter{
				{AccountID: 888002, CharacterID: 300101, Name: "麒麟", Level: 33, ClassID: 1},
			},
		},
		{
			// Cross-server config agent node, poll mode (no bus needed).
			ServerID: "game-9001", Name: "华东-跨服节点", Region: "cn-east",
			Version:  "1.0.0",
			Endpoint: atlas.Endpoint{Host: "127.0.0.1", Port: 30001}, Capacity: 500,
			NotifyMode: atlas.NotifyModePoll, Players: 42, Drift: 8,
		},
		{
			// Cross-server config agent node, subscribe mode.
			ServerID: "game-9002", Name: "华东-回调节点", Region: "cn-east",
			Version:  "1.0.0",
			Endpoint: atlas.Endpoint{Host: "127.0.0.1", Port: 30001}, Capacity: 500,
			NotifyMode: atlas.NotifyModeSubscribe, Players: 42, Drift: 8,
		},
		{
			// Registered once, then left in maintenance by the demo seed —
			// the maintenance-window demo server.
			ServerID: "game-7000", Name: "维护演示·天枢", Region: "cn-east",
			RealmID: "realm-cn", ShardID: "shard-east", Version: "1.2.0",
			Endpoint: atlas.Endpoint{Host: "10.0.7.70", Port: 30070}, Capacity: 1000,
			SeedOnly: true, Players: 31,
		},
	}
}

// loadFleet reads the fleet from a JSON file, or returns the built-in one.
// The file schema is {"servers":[...]}, a list of fleetEntry objects.
func loadFleet(path string) ([]fleetEntry, error) {
	if path == "" {
		fleet := defaultFleet()
		for i := range fleet {
			if err := fleet[i].validate(); err != nil {
				return nil, err
			}
		}
		return fleet, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Servers []fleetEntry `json:"servers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Servers) == 0 {
		return nil, fmt.Errorf("%s declares no servers", path)
	}
	for i := range doc.Servers {
		if err := doc.Servers[i].validate(); err != nil {
			return nil, fmt.Errorf("%s: server %d: %w", path, i, err)
		}
	}
	return doc.Servers, nil
}

func (e *fleetEntry) validate() error {
	if e.ServerID == "" {
		return errors.New("server_id is required")
	}
	if e.Region == "" {
		return fmt.Errorf("server %s: region is required", e.ServerID)
	}
	if e.Capacity <= 0 {
		return fmt.Errorf("server %s: capacity must be > 0", e.ServerID)
	}
	if e.Players < 0 {
		return fmt.Errorf("server %s: players must be >= 0", e.ServerID)
	}
	switch e.NotifyMode {
	case "", atlas.NotifyModeSubscribe, atlas.NotifyModeCallback, atlas.NotifyModePoll:
	default:
		return fmt.Errorf("server %s: invalid notify_mode %q", e.ServerID, e.NotifyMode)
	}
	return nil
}

func (e *fleetEntry) registerRequest() atlas.RegisterRequest {
	req := atlas.RegisterRequest{
		ServerID:   e.ServerID,
		Name:       e.Name,
		Type:       orString(e.Type, "game"),
		Region:     e.Region,
		Version:    orString(e.Version, "1.2.0"),
		Platform:   orString(e.Platform, "pc"),
		Endpoint:   e.Endpoint,
		Capacity:   e.Capacity,
		NotifyMode: orString(e.NotifyMode, atlas.NotifyModePoll),
	}
	if e.RealmID != "" {
		req.RealmID = &e.RealmID
	}
	if e.ShardID != "" {
		req.ShardID = &e.ShardID
	}
	return req
}

func orString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// emitFleet prints the built-in fleet in the -fleet file format so operators
// start from working data (and the checked-in deployments/demo/fleet.json can
// never drift from the code).
func emitFleet(fleet []fleetEntry) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"servers": fleet}); err != nil {
		fmt.Fprintf(os.Stderr, "atlas-demoagents: %v\n", err)
		os.Exit(1)
	}
}

// runHealthcheck probes the status endpoint and exits 0 on success.
func runHealthcheck(addr string) int {
	if addr == "" {
		addr = ":9098"
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

func main() {
	// `atlas-demoagents healthcheck` is the container health probe: it hits
	// this process's own status endpoint, so a fleet whose heartbeats died is
	// reported unhealthy (the distroless image has no shell or curl to do it).
	// The probe address follows the same flag/env the server binds.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck(os.Getenv("ATLAS_DEMOAGENTS_HTTP_ADDR")))
	}

	var (
		registryAddr = flag.String("registry", "http://127.0.0.1:8081", "Atlas registry base URL (register/heartbeat)")
		publicAddr   = flag.String("public", "http://127.0.0.1:8080", "Atlas public base URL (character writes)")
		adminAddr    = flag.String("admin", "http://127.0.0.1:8082", "Atlas admin base URL (tags, maintenance)")
		fleetPath    = flag.String("fleet", "", "fleet JSON file (default: built-in demo fleet)")
		interval     = flag.Duration("interval", 10*time.Second, "heartbeat interval")
		pollEvery    = flag.Duration("poll-interval", 30*time.Second, "cross-server config poll interval (0 disables; subscribe-mode servers still keep this as the final fallback)")
		watchServer  = flag.String("watch-server", "game-1001", "server id that must keep a cross-server config watcher converged (extra log lines)")
		redisURL     = flag.String("redis-url", "", "Redis URL for subscribe mode (empty = subscribe-mode servers fall back to polling only)")
		statusAddr   = flag.String("http", ":9098", "status endpoint address (empty disables)")
		seedChars    = flag.Bool("seed-characters", true, "write the demo characters into the directory index")
		printFleet   = flag.Bool("print-fleet", false, "print the built-in demo fleet as JSON and exit (start from it with -fleet)")
		verbose      = flag.Bool("v", false, "debug logging")
	)
	flag.Parse()

	if *printFleet {
		emitFleet(defaultFleet())
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	fleet, err := loadFleet(*fleetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas-demoagents: %v\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := atlas.New(atlas.Options{Addr: *publicAddr, RegistryAddr: *registryAddr})
	if err != nil {
		log.Error("client init failed", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	admin := &adminClient{base: strings.TrimRight(*adminAddr, "/"), http: &http.Client{Timeout: 5 * time.Second}}

	live := make(map[string]*liveState, len(fleet))
	for i := range fleet {
		live[fleet[i].ServerID] = newLiveState(&fleet[i])
	}

	// Register the whole fleet before anything else: a demo with a
	// half-registered fleet shows half-empty tables for the first minute.
	if err := registerFleet(ctx, log, client, admin, fleet, *seedChars); err != nil {
		log.Error("fleet registration failed", "error", err)
		os.Exit(1)
	}

	var hbOK, hbFail, lastBeat atomic.Int64
	var wg sync.WaitGroup

	// Heartbeats: one goroutine per server so a slow or failing endpoint
	// cannot delay the rest of the fleet.
	for i := range fleet {
		e := &fleet[i]
		if e.SeedOnly {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			heartbeat(ctx, log, client, e, live[e.ServerID], *interval, &hbOK, &hbFail, &lastBeat)
		}()
	}

	// Config watchers. Every subscribe-mode entry gets one (bus-signal driven,
	// poll as the final fallback), plus the -watch-server entry whatever its
	// declared mode — so publishing a document from the admin API is visible
	// as a hot update in this log.
	watched := 0
	for i := range fleet {
		e := &fleet[i]
		isTarget := e.ServerID == *watchServer
		if !isTarget && e.NotifyMode != atlas.NotifyModeSubscribe {
			continue
		}
		watched++
		wg.Add(1)
		go func() {
			defer wg.Done()
			watchConfig(ctx, log, client, e, *pollEvery, *redisURL, isTarget)
		}()
	}
	if *watchServer != "" && watched == 0 {
		log.Warn("watch target not in fleet, no config watcher", "server_id", *watchServer)
	}

	if *statusAddr != "" {
		startStatusServer(ctx, log, *statusAddr, fleet, live, &hbOK, &hbFail, &lastBeat, *interval)
	}

	<-ctx.Done()
	log.Info("shutting down", "heartbeats_ok", hbOK.Load(), "heartbeats_failed", hbFail.Load())
	wg.Wait()
}

func findEntry(fleet []fleetEntry, id string) *fleetEntry {
	for i := range fleet {
		if fleet[i].ServerID == id {
			return &fleet[i]
		}
	}
	return nil
}

// ensureTopology creates every realm and shard the fleet references, in
// realm-then-shard order. Duplicates are fine: Atlas answers 409 and the
// seed moves on. Returns nil when every referenced row is known to exist.
func ensureTopology(ctx context.Context, log *slog.Logger, admin *adminClient, fleet []fleetEntry) error {
	realms := map[string]string{} // realm id → region (first entry wins)
	shards := map[string]string{} // shard id → owning realm id ("" = region-level)
	for i := range fleet {
		if fleet[i].RealmID != "" {
			if _, ok := realms[fleet[i].RealmID]; !ok {
				realms[fleet[i].RealmID] = fleet[i].Region
			}
		}
		if fleet[i].ShardID != "" {
			shards[fleet[i].ShardID] = fleet[i].RealmID
		}
	}
	for id, region := range realms {
		body := map[string]any{"id": id, "name": id, "region": region}
		if err := admin.postJSON(ctx, "/v1/admin/realms", body); err != nil && !isConflict(err) {
			return fmt.Errorf("seed realm %s: %w", id, err)
		}
	}
	for id, realmID := range shards {
		body := map[string]any{"id": id, "name": id}
		if realmID != "" {
			body["realm_id"] = realmID
		}
		if err := admin.postJSON(ctx, "/v1/admin/shards", body); err != nil && !isConflict(err) {
			return fmt.Errorf("seed shard %s: %w", id, err)
		}
	}
	if len(realms) > 0 || len(shards) > 0 {
		log.Info("topology seeded", "realms", len(realms), "shards", len(shards))
	}
	return nil
}

// isConflict reports whether an admin POST failed with HTTP 409 — the row
// already exists, which for an idempotent re-seed is a success outcome.
func isConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "http 409")
}

// registerFleet registers every entry, retrying until Atlas answers — the
// container may start before the registry listener is up.
func registerFleet(ctx context.Context, log *slog.Logger, client *atlas.Client, admin *adminClient, fleet []fleetEntry, seedChars bool) error {
	// Topology first: fleet entries hang under realms/shards, and a fresh
	// Atlas (empty postgres) rejects registrations that reference an unknown
	// realm. Topology is ops-owned data, so it is seeded through the admin
	// API the same way an operator would create it; an HTTP 409 answer means
	// an earlier boot of this container already created the row.
	topologyBackoff := 500 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ensureTopology(ctx, log, admin, fleet); err == nil {
			break
		} else {
			log.Warn("topology seed failed, retrying", "error", err, "backoff", topologyBackoff)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(topologyBackoff):
		}
		topologyBackoff = min(topologyBackoff*2, 5*time.Second)
	}
	for i := range fleet {
		e := &fleet[i]
		backoff := 500 * time.Millisecond
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := registerOne(ctx, log, client, admin, e, seedChars); err == nil {
				break
			} else {
				log.Warn("register failed, retrying", "server_id", e.ServerID, "error", err, "backoff", backoff)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 5*time.Second)
		}
	}
	return nil
}

func registerOne(ctx context.Context, log *slog.Logger, client *atlas.Client, admin *adminClient, e *fleetEntry, seedChars bool) error {
	res, err := client.Register(ctx, e.registerRequest())
	if err != nil {
		return err
	}
	cfgVer := 0
	if res.CrossServerConfig != nil {
		cfgVer = res.CrossServerConfig.Version
	}
	log.Info("registered", "server_id", e.ServerID, "status", res.Status,
		"notify_mode", orString(e.NotifyMode, atlas.NotifyModePoll), "config_version", cfgVer)

	if seedChars {
		for _, c := range e.Characters {
			if _, err := client.CreateCharacter(ctx, atlas.CreateCharacterRequest{
				AccountID:   c.AccountID,
				ServerID:    e.ServerID,
				CharacterID: c.CharacterID,
				Name:        c.Name,
				Level:       c.Level,
				ClassID:     c.ClassID,
			}); err != nil {
				// Character writes are the demo's garnish, not its backbone:
				// a registry that rejects them (a no_register tag left by an
				// earlier demo run, say) must not stop the fleet from booting.
				log.Warn("character seed skipped", "server_id", e.ServerID, "character", c.Name, "error", err)
			}
		}
	}

	// Ops-owned state, seeded through the admin API the way an operator would.
	for _, t := range e.Tags {
		body := map[string]any{"code": t.Code}
		if t.Label != "" {
			body["label"] = t.Label
		}
		if t.Tier != "" {
			body["tier"] = t.Tier
		}
		if t.Public != nil {
			body["public"] = *t.Public
		}
		if err := admin.postJSON(ctx, "/v1/admin/servers/"+e.ServerID+"/tags", body); err != nil {
			log.Warn("tag seed failed", "server_id", e.ServerID, "code", t.Code, "error", err)
		}
	}
	if e.SeedOnly {
		// Park the maintenance demo server: nothing heartbeats it, so the
		// health monitor never moves it back online.
		if err := admin.postJSON(ctx, "/v1/admin/servers/"+e.ServerID+"/maintenance", nil); err != nil {
			log.Warn("maintenance seed failed", "server_id", e.ServerID, "error", err)
		}
	}
	return nil
}

func heartbeat(ctx context.Context, log *slog.Logger, client *atlas.Client, e *fleetEntry, state *liveState, interval time.Duration, ok, fail *atomic.Int64, lastBeat *atomic.Int64) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			players, load := state.tick(interval)
			res, err := client.Heartbeat(ctx, e.ServerID, atlas.HeartbeatRequest{Players: players, Load: load})
			if err != nil {
				fail.Add(1)
				log.Warn("heartbeat failed", "server_id", e.ServerID, "error", err)
				continue
			}
			ok.Add(1)
			lastBeat.Store(time.Now().UnixNano())
			log.Debug("heartbeat", "server_id", e.ServerID, "status", res.Status, "players", players, "load", load)
		}
	}
}

// watchConfig keeps one server's view of the coordination config converged.
// The watcher owns the guards (strictly-newer versions only, idempotent
// re-delivery ignored, failed pulls retried with backoff on the last good
// config) — this function supplies the poll cadence, the hot-apply log line
// and, in subscribe mode, the bus subscription that triggers the pull.
//
// Retry contract: the watcher's startup pull is fail-fast, but a demo fleet
// that boots before anything is published must not go blind forever, so a
// failed startup pull is retried until it succeeds or the process stops.
func watchConfig(ctx context.Context, log *slog.Logger, client *atlas.Client, e *fleetEntry, pollEvery time.Duration, redisURL string, primary bool) {
	poll := pollEvery
	if poll == 0 {
		poll = -1 // the flag says "disable"; the watcher reads 0 as "default"
	}
	watcher := atlas.NewConfigWatcher(client, atlas.ConfigWatcherOptions{
		ServerID:     e.ServerID,
		PollInterval: poll,
		MaxBackoff:   10 * time.Second,
		OnApply: func(cfg *atlas.CrossServerConfig) {
			features := 0
			for _, on := range cfg.Spec.Features {
				if on {
					features++
				}
			}
			level := slog.LevelInfo
			if !primary {
				level = slog.LevelDebug
			}
			log.Log(ctx, level, "cross-server config applied (hot, no restart)",
				"server_id", e.ServerID, "version", cfg.Version, "hash", cfg.Hash,
				"clusters", len(cfg.Spec.Topology.Clusters), "groups", len(cfg.Spec.Groups),
				"features_on", features, "match_domains", len(cfg.Spec.MatchDomains))
		},
	})

	if e.NotifyMode == atlas.NotifyModeSubscribe {
		if redisURL == "" {
			log.Warn("subscribe mode without -redis-url: falling back to polling only", "server_id", e.ServerID)
		} else if cleanup, err := subscribeConfig(ctx, log, watcher, e.ServerID, redisURL); err != nil {
			log.Warn("bus subscribe failed, polling only", "server_id", e.ServerID, "error", err)
		} else {
			defer cleanup()
		}
	}

	// Run blocks until ctx is done once the first pull succeeds; it returns an
	// error only when the startup pull could not be satisfied.
	for {
		err := watcher.Run(ctx, 3)
		if err == nil {
			return // ctx cancelled — clean shutdown
		}
		log.Warn("cross-server config unavailable at startup (retrying)",
			"server_id", e.ServerID, "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

// subscribeConfig puts one server on the config topic and returns a cleanup
// function. Receiving the signal is exactly watcher.Notify() — the pull and
// every version guard live in the watcher. The consumer group is per server so
// every server gets its own copy of the signal (a shared group would compete,
// not broadcast).
func subscribeConfig(ctx context.Context, log *slog.Logger, watcher *atlas.ConfigWatcher, serverID, redisURL string) (func(), error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	group := "demoagents-" + serverID
	adapter := redisEvent.New(rdb, redisEvent.Options{Group: group, Consumer: serverID, Logger: log})
	if err := adapter.Subscribe(ctx, event.TopicConfig, func(_ context.Context, ev *event.Event) error {
		if !addressed(ev.ConfigServers, serverID) {
			return nil
		}
		log.Debug("config signal received", "server_id", serverID,
			"type", string(ev.Type), "version", ev.ConfigVersion, "hash", ev.ConfigHash, "targets", ev.ConfigTargets)
		watcher.Notify()
		return nil
	}); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	log.Info("subscribed to config topic", "server_id", serverID, "topic", event.TopicConfig, "group", group)
	// 补拉: an update may have landed before the subscription existed.
	watcher.Notify()
	return func() {
		_ = adapter.Close()
		_ = rdb.Close()
	}, nil
}

// addressed reports whether a config signal names this server. The receiver
// list is computed by Atlas from the config before and after the change; "*"
// means every server.
func addressed(receivers []string, serverID string) bool {
	for _, r := range receivers {
		if r == model.TargetAll || r == serverID {
			return true
		}
	}
	return false
}

// adminClient is a minimal admin-API caller for the ops-owned demo seeding.
type adminClient struct {
	base string
	http *http.Client
}

func (a *adminClient) postJSON(ctx context.Context, path string, body any) error {
	payload := "{}"
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = string(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: http %d", path, resp.StatusCode)
	}
	return nil
}

func startStatusServer(ctx context.Context, log *slog.Logger, addr string, fleet []fleetEntry, live map[string]*liveState, ok, fail, lastBeat *atomic.Int64, heartbeatInterval time.Duration) {
	type statusEntry struct {
		ServerID string  `json:"server_id"`
		Players  int64   `json:"players"`
		Load     float64 `json:"load"`
		Capacity int     `json:"capacity"`
		SeedOnly bool    `json:"seed_only"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		out := struct {
			Servers        []statusEntry `json:"servers"`
			HeartbeatsOK   int64         `json:"heartbeats_ok"`
			HeartbeatsFail int64         `json:"heartbeats_failed"`
		}{HeartbeatsOK: ok.Load(), HeartbeatsFail: fail.Load()}
		for i := range fleet {
			st := live[fleet[i].ServerID]
			out.Servers = append(out.Servers, statusEntry{
				ServerID: fleet[i].ServerID,
				Players:  st.players.Load(),
				Load:     math.Float64frombits(st.loadBits.Load()),
				Capacity: fleet[i].Capacity,
				SeedOnly: fleet[i].SeedOnly,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		// Healthy means "heartbeats are still landing", not "the process
		// exists": a fleet that silently lost Atlas must show up in
		// docker ps / compose as unhealthy.
		if last, seen := lastBeat.Load(), ok.Load(); seen > 0 && last > 0 {
			if time.Since(time.Unix(0, last)) > 3*heartbeatInterval {
				http.Error(w, "heartbeats stale", http.StatusServiceUnavailable)
				return
			}
		}
		fmt.Fprintln(w, "ok")
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("status endpoint up", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("status endpoint failed", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
}
