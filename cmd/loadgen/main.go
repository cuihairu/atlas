// loadgen is the production-shape load driver for Atlas soak runs
// (docs/performance.md §9). Build once, point at the deployment under test,
// size the fleet, and let it run:
//
//	go build ./cmd/loadgen
//	./loadgen -public http://atlas:8080 -registry http://atlas:8081 \
//	          -admin http://atlas:8082 -admin-key $ATLAS_ADMIN_API_KEY \
//	          -servers 1000 -read-rps 200 -char-rps 20 \
//	          -ops-every 30s -storm-every 10m -duration 24h -report run.json
//
// On interrupt (or -duration) it prints the per-flow p50/p95/p99 table and
// optionally writes the JSON report. Skips mean the -max-inflight cap
// saturated — that is an overload signal, not an error.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"
)

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.PublicURL, "public", "http://127.0.0.1:8080", "public base URL (discovery / routing / directory)")
	flag.StringVar(&cfg.RegistryURL, "registry", "http://127.0.0.1:8081", "registry base URL (register / heartbeat)")
	flag.StringVar(&cfg.AdminURL, "admin", "", "admin base URL for the ops trickle (empty = off)")
	flag.StringVar(&cfg.AdminKey, "admin-key", "", "admin API key when -admin is set")
	flag.IntVar(&cfg.Servers, "servers", 100, "virtual fleet size (scale points: 100 / 1000 / 5000)")
	flag.DurationVar(&cfg.Duration, "duration", 0, "run length (0 = until interrupt)")
	flag.DurationVar(&cfg.Heartbeat, "heartbeat", 10*time.Second, "fleet heartbeat cadence (SDK default 10s)")
	flag.Float64Var(&cfg.ReadRPS, "read-rps", 0, "aggregate read rate over the list/detail/recommend mix")
	flag.IntVar(&cfg.ReadWorkers, "read-workers", 16, "concurrent read issuers (reads are fleet-scaled; raise with -read-rps)")
	flag.Float64Var(&cfg.CharRPS, "char-rps", 0, "character write rate")
	flag.DurationVar(&cfg.OpsEvery, "ops-every", 0, "admin stats trickle interval (needs -admin)")
	flag.DurationVar(&cfg.StormEvery, "storm-every", 0, "rolling-restart drill interval (re-registers the fleet in one suspect window)")
	flag.Float64Var(&cfg.Jitter, "jitter", 0.1, "per-server beat jitter fraction")
	flag.DurationVar(&cfg.Timeout, "timeout", 5*time.Second, "per-call HTTP timeout")
	flag.IntVar(&cfg.MaxInFlight, "max-inflight", 256, "concurrent request cap across all streams")
	flag.Uint64Var(&cfg.Seed, "seed", 1, "RNG seed (fleet phases, read mix)")
	reportPath := flag.String("report", "", "write the JSON report here when the run ends")
	flag.Parse()

	e, err := NewEngine(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer e.Close()

	ctx := context.Background()
	if cfg.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Duration)
		defer cancel()
	} else {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
	}

	fmt.Fprintf(os.Stderr, "loadgen: %d servers against %s (registry %s), beat %s, reads %.0f rps\n",
		cfg.Servers, cfg.PublicURL, cfg.RegistryURL, cfg.Heartbeat, cfg.ReadRPS)
	rep := e.Run(ctx)

	fmt.Println(rep.RenderText())
	if *reportPath != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "loadgen: marshal report: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*reportPath, b, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "loadgen: write report: %v\n", err)
			os.Exit(1)
		}
	}
}
