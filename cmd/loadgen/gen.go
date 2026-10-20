// loadgen drives a production-shaped load model against a running Atlas
// (docs/performance.md §9): a fleet of virtual game servers registering and
// heartbeating at SDK cadence, a mixed read stream (list / detail /
// recommend), character writes into the directory index, an ops trickle on
// the admin port, and a rolling-restart storm that re-registers the whole
// fleet inside one suspect window. Every call goes through the Go SDK, so
// measurements include the client path real integrators use. It is the load
// driver picked by the §9 decision record: the model is cadence-shaped
// (N servers each on their own beat), not RPS-shaped, which is why k6 /
// vegeta / ghz were rejected (see docs/performance.md §9 "Driver decision").
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	atlas "github.com/cuihairu/atlas/sdk/go/atlas"
)

// Flow names, also the report rows.
const (
	flowRegister      = "register"
	flowHeartbeat     = "heartbeat"
	flowStorm         = "storm-register"
	flowReadList      = "read-list"
	flowReadDetail    = "read-detail"
	flowReadRecommend = "read-recommend"
	flowCharWrite     = "char-write"
	flowOps           = "ops-stats"
)

// readKinds is the §9 read mix: list / detail / recommend at 5:3:2.
var readKinds = []struct {
	flow   string
	weight int64
}{
	{flowReadList, 5},
	{flowReadDetail, 3},
	{flowReadRecommend, 2},
}

// fleetServer is one virtual game server: identity plus the live-state
// model (baseline players with slow sine drift) the heartbeat reports.
type fleetServer struct {
	id    string
	req   atlas.RegisterRequest
	beat  time.Duration // per-server cadence: Heartbeat ± jitter, fixed for the run
	base  int           // baseline player count
	drift int           // sine amplitude around the baseline
	phase float64       // per-server phase so the fleet doesn't beat in lockstep
}

// Config sizes the load. Zero rates mean the stream stays off.
type Config struct {
	PublicURL   string        // discovery / routing / directory base
	RegistryURL string        // register / heartbeat base (Atlas split port)
	AdminURL    string        // admin base for the ops trickle ("" = off)
	AdminKey    string        // ATLAS_ADMIN_API_KEY for the admin client
	Servers     int           // fleet size (§9 scale points: 100 / 1000 / 5000)
	Duration    time.Duration // 0 in main = run until interrupt
	Heartbeat   time.Duration // fleet beat; 0 = SDK default 10s
	ReadRPS     float64       // aggregate read rate across the 5:3:2 mix
	CharRPS     float64       // character write rate
	OpsEvery    time.Duration // admin stats interval (needs AdminURL)
	StormEvery  time.Duration // rolling-restart drill interval
	Jitter      float64       // per-server beat jitter fraction (0.1 = ±10%)
	Timeout     time.Duration // per-call HTTP timeout
	MaxInFlight int           // concurrent request cap across all streams
	Seed        uint64        // RNG seed; fixed = reproducible mix and phases
}

// Engine runs the load model and accumulates per-flow latency streams.
type Engine struct {
	cfg        Config
	fleet      *atlas.Client // public + registry bases
	admin      *atlas.Client // admin base, nil when AdminURL is empty
	flows      map[string]*stream
	servers    []fleetServer
	sem        chan struct{}
	fleetReady chan struct{}
	rng        *rand.Rand
	charSeq    int64 // monotonically increasing character/account ids
	mu         sync.Mutex
}

// NewEngine validates cfg and builds the SDK clients. Retries are disabled
// (MaxRetries -1) so a measured latency is one honest attempt, not a retry
// ladder; saturation is reported as skips, never silently retried away.
func NewEngine(cfg Config) (*Engine, error) {
	if cfg.Servers <= 0 {
		return nil, fmt.Errorf("loadgen: Servers must be > 0")
	}
	if cfg.PublicURL == "" {
		return nil, fmt.Errorf("loadgen: PublicURL is required")
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 10 * time.Second // SDK default (sdk/go/atlas StartHeartbeat)
	}
	if cfg.Jitter <= 0 {
		cfg.Jitter = 0.1
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 256
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}
	fleet, err := atlas.New(atlas.Options{
		Addr:         cfg.PublicURL,
		RegistryAddr: cfg.RegistryURL,
		HTTPClient:   httpClient,
		MaxRetries:   -1,
	})
	if err != nil {
		return nil, fmt.Errorf("loadgen: fleet client: %w", err)
	}
	var admin *atlas.Client
	if cfg.AdminURL != "" {
		admin, err = atlas.New(atlas.Options{
			Addr:        cfg.AdminURL,
			HTTPClient:  httpClient,
			AdminAPIKey: cfg.AdminKey,
			MaxRetries:  -1,
		})
		if err != nil {
			return nil, fmt.Errorf("loadgen: admin client: %w", err)
		}
	}

	e := &Engine{
		cfg:   cfg,
		fleet: fleet,
		admin: admin,
		flows: map[string]*stream{
			flowRegister:      newStream(flowRegister, reservoirCap),
			flowHeartbeat:     newStream(flowHeartbeat, reservoirCap),
			flowStorm:         newStream(flowStorm, reservoirCap),
			flowReadList:      newStream(flowReadList, reservoirCap),
			flowReadDetail:    newStream(flowReadDetail, reservoirCap),
			flowReadRecommend: newStream(flowReadRecommend, reservoirCap),
			flowCharWrite:     newStream(flowCharWrite, reservoirCap),
			flowOps:           newStream(flowOps, reservoirCap),
		},
		sem:        make(chan struct{}, cfg.MaxInFlight),
		fleetReady: make(chan struct{}),
		rng:        rand.New(rand.NewPCG(cfg.Seed, 0)),
	}
	e.servers = e.buildFleet()
	return e, nil
}

// Close releases the SDK clients.
func (e *Engine) Close() error {
	if e.admin != nil {
		_ = e.admin.Close()
	}
	return e.fleet.Close()
}

// buildFleet derives the virtual fleet deterministically from the RNG seed:
// ids loadgen-0001..N spread over a few regions/versions so list filters and
// routing have real variety to chew on.
func (e *Engine) buildFleet() []fleetServer {
	regions := []string{"eu-1", "us-1", "ap-1"}
	versions := []string{"1.0.0", "1.1.0"}
	platforms := []string{"windows", "linux"}
	out := make([]fleetServer, e.cfg.Servers)
	for i := range out {
		id := fmt.Sprintf("loadgen-%04d", i+1)
		capacity := 500
		out[i] = fleetServer{
			id: id,
			req: atlas.RegisterRequest{
				ServerID: id,
				Name:     "loadgen-" + regions[i%len(regions)],
				Type:     "gameserver",
				Region:   regions[i%len(regions)],
				Version:  versions[i%len(versions)],
				Platform: platforms[i%len(platforms)],
				Endpoint: atlas.Endpoint{Host: "10.0.0.1", Port: 1000 + i%9000},
				Capacity: capacity,
			},
			beat:  time.Duration(float64(e.cfg.Heartbeat) * (1 + (e.rng.Float64()*2-1)*e.cfg.Jitter)),
			base:  capacity * 3 / 5,
			drift: capacity / 5,
			phase: e.rng.Float64() * 2 * math.Pi,
		}
	}
	return out
}

// Run drives all configured streams until ctx is done, then returns the
// report. It blocks; call from one goroutine. The read/char/ops/storm rate
// loops stay parked until the whole fleet has registered (HA smoke
// 2026-10-20: ungated reads raced the boot herd and produced honest-looking
// SERVER_NOT_FOUND noise that belongs to the harness, not the system).
func (e *Engine) Run(ctx context.Context) *report {
	start := time.Now()
	var wg sync.WaitGroup

	var boot sync.WaitGroup
	boot.Add(len(e.servers))
	go func() {
		boot.Wait()
		close(e.fleetReady)
	}()
	fleetLoop := func(idx int) {
		defer wg.Done()
		e.fleetLoop(ctx, idx, &boot)
	}
	for i := range e.servers {
		wg.Add(1)
		go fleetLoop(i)
	}

	// gate parks a rate loop until the fleet is up (or the run is torn
	// down — a fleet that cannot boot must not wedge the harness).
	gate := func() bool {
		select {
		case <-e.fleetReady:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if e.cfg.ReadRPS > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !gate() {
				return
			}
			e.rateLoop(ctx, time.Duration(float64(time.Second)/e.cfg.ReadRPS), func() { e.readTick(ctx) })
		}()
	}
	if e.cfg.CharRPS > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !gate() {
				return
			}
			e.rateLoop(ctx, time.Duration(float64(time.Second)/e.cfg.CharRPS), func() { e.charTick(ctx) })
		}()
	}
	if e.admin != nil && e.cfg.OpsEvery > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !gate() {
				return
			}
			e.rateLoop(ctx, e.cfg.OpsEvery, func() { e.opsTick(ctx) })
		}()
	}
	if e.cfg.StormEvery > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !gate() {
				return
			}
			e.rateLoop(ctx, e.cfg.StormEvery, func() { e.storm(ctx) })
		}()
	}

	<-ctx.Done()
	wg.Wait()
	return e.report(time.Since(start))
}

// fleetLoop registers one virtual server — retrying through admission-control
// skips until the registry has accepted it (a game server that cannot get an
// in-flight slot at boot keeps trying; it does not spend the run heartbeating
// against a registry that never learned its id) — then heartbeats on its own
// cadence, the same shape as sdk StartHeartbeat (immediate beat, then tick).
func (e *Engine) fleetLoop(ctx context.Context, idx int, boot *sync.WaitGroup) {
	svc := e.servers[idx]
	for {
		if !e.call(flowRegister, func() error {
			_, err := e.fleet.Register(ctx, svc.req)
			return err
		}) {
			break
		}
		select {
		case <-ctx.Done():
			boot.Done() // torn down mid-boot: still leave the boot set
			return
		case <-time.After(e.bootBackoff()):
		}
	}
	// Signal readiness here, right after registration — not via defer, or the
	// fleet-ready gate would only open at teardown and reads would never run.
	boot.Done()
	e.heartbeat(ctx, svc, time.Now())
	t := time.NewTicker(svc.beat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			e.heartbeat(ctx, svc, now)
		}
	}
}

// bootBackoff is the per-server retry delay after a skipped boot register;
// jittered so a saturated semaphore does not re-sync the whole herd onto one
// tick.
func (e *Engine) bootBackoff() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return time.Duration(25+e.rng.Int64N(50)) * time.Millisecond
}

func (e *Engine) heartbeat(ctx context.Context, svc fleetServer, now time.Time) {
	players := svc.livePlayers(now)
	e.call(flowHeartbeat, func() error {
		_, err := e.fleet.Heartbeat(ctx, svc.id, atlas.HeartbeatRequest{
			Players: players,
			Load:    float64(players) / float64(svc.req.Capacity),
		})
		return err
	})
}

// livePlayers drifts the player count on a slow sine (5-minute period),
// like the demo fleet's Drift — a fleet of constant-value heartbeats would
// not exercise load-based routing.
func (s fleetServer) livePlayers(now time.Time) int {
	t := now.Unix() + int64(s.phase*100)
	wave := math.Sin(2 * math.Pi * float64(t%300) / 300)
	return s.base + int(float64(s.drift)*wave)
}

// rateLoop fires fn on a fixed interval until ctx is done. Ticks are not
// made up when fn is slow — the load shape is open-loop.
func (e *Engine) rateLoop(ctx context.Context, iv time.Duration, fn func()) {
	if iv <= 0 {
		iv = time.Millisecond
	}
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// nextKind draws the next read kind; all RNG use funnels through e.mu.
func (e *Engine) nextKind() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return pickReadKind(e.rng)
}

// readTick issues one read of the 5:3:2 mix.
func (e *Engine) readTick(ctx context.Context) {
	switch e.nextKind() {
	case 0:
		e.call(flowReadList, func() error {
			_, err := e.fleet.ListServers(ctx, atlas.ServerFilter{Limit: 50})
			return err
		})
	case 1:
		svc := e.randomServer()
		e.call(flowReadDetail, func() error {
			_, err := e.fleet.GetServer(ctx, svc.id)
			return err
		})
	default:
		svc := e.randomServer()
		e.call(flowReadRecommend, func() error {
			_, err := e.fleet.Recommend(ctx, 0, svc.req.Region, svc.req.Version, svc.req.Platform)
			return err
		})
	}
}

// charTick writes one character (v1: creates only — the update/delete mix
// is a harness follow-up; creates already drive projection + inverted index).
func (e *Engine) charTick(ctx context.Context) {
	e.mu.Lock()
	e.charSeq++
	seq := e.charSeq
	e.mu.Unlock()
	svc := e.randomServer()
	e.call(flowCharWrite, func() error {
		_, err := e.fleet.CreateCharacter(ctx, atlas.CreateCharacterRequest{
			AccountID:   100000 + seq,
			ServerID:    svc.id,
			CharacterID: int64(1000000 + seq),
			Name:        fmt.Sprintf("gen-%06d", seq),
			Level:       1 + int(seq%80),
			ClassID:     1 + int(seq%8),
		})
		return err
	})
}

func (e *Engine) opsTick(ctx context.Context) {
	e.call(flowOps, func() error {
		_, err := e.admin.Stats(ctx)
		return err
	})
}

// storm re-registers the whole fleet spread across one suspect window
// (3 × Heartbeat, capped at the 30s default) — the §9 rolling-restart
// drill. Registration is an idempotent upsert, so heartbeats keep flowing.
func (e *Engine) storm(ctx context.Context) {
	window := 3 * e.cfg.Heartbeat
	if window > 30*time.Second {
		window = 30 * time.Second
	}
	if window <= 0 {
		window = time.Second
	}
	for i := range e.servers {
		delay := time.Duration(i) * window / time.Duration(len(e.servers))
		go func(svc fleetServer, d time.Duration) {
			if d > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(d):
				}
			}
			e.call(flowStorm, func() error {
				_, err := e.fleet.Register(ctx, svc.req)
				return err
			})
		}(e.servers[i], delay)
	}
}

// call runs one measured request through the in-flight cap and reports
// whether the attempt was skipped for saturation (true) — the retryable case;
// the boot register loop is the only retrying caller, rate-loop skips stay
// counted as honest overload signals. When the cap is saturated the call is
// skipped and counted — not an error and not a silent retry. Calls still in
// flight when the run ctx is cancelled are shutdown noise, counted as skips
// too: they never measured a full attempt, so counting them as errors would
// pollute the run's verdict.
func (e *Engine) call(flow string, fn func() error) (skipped bool) {
	select {
	case e.sem <- struct{}{}:
	default:
		e.flows[flow].Skip()
		return true
	}
	defer func() { <-e.sem }()
	start := time.Now()
	err := fn()
	if err != nil && ctxClosed(err) {
		e.flows[flow].Skip()
		return false
	}
	if err != nil {
		e.flows[flow].Err(err)
		return false
	}
	e.flows[flow].Record(time.Since(start))
	return false
}

// ctxClosed reports whether err is just the run being torn down.
func ctxClosed(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (e *Engine) randomServer() fleetServer {
	e.mu.Lock()
	idx := e.rng.IntN(len(e.servers))
	e.mu.Unlock()
	return e.servers[idx]
}

// pickReadKind draws a read kind by weight (5:3:2).
func pickReadKind(rng *rand.Rand) int {
	var total int64
	for _, k := range readKinds {
		total += k.weight
	}
	d := rng.Int64N(total)
	for i, k := range readKinds {
		if d < k.weight {
			return i
		}
		d -= k.weight
	}
	return 0
}
