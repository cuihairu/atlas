// soakcheck is the plateau half of the production-shape soak run
// (docs/performance.md §9). It samples the runtime series the ≥24h run is
// judged on — steady-state heap, goroutine count, open fds — from each
// replica's :8082/metrics and reports per replica per metric whether the
// series holds a plateau instead of creeping. Run it next to cmd/loadgen and
// deploy/perf/drills.sh; it prints the sampled window as ISO timestamps so the
// verdict can be correlated against injected fault windows, and it exits 0
// only when every series passes.
//
//	go build ./cmd/soakcheck
//	./soakcheck --metrics http://atlas1:8082/metrics http://atlas2:8082/metrics \
//	          --interval 60s --duration 24h --out soak.tsv
//	./soakcheck --sample soak.tsv          # re-verify offline, no machine needed
//
// Samples written with --out are replayable, so a soak's verdict can be
// audited after the fact without re-running the machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

// stringList lets --metrics be repeated or given as one space-separated list.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, " ") }
func (l *stringList) Set(v string) error {
	*l = append(*l, strings.Fields(v)...)
	return nil
}

func main() {
	var urls stringList
	interval := flag.Duration("interval", 60*time.Second, "scrape period per replica")
	duration := flag.Duration("duration", 0, "run length (0 = until interrupt)")
	drift := flag.Float64("drift", 0.20, "tolerated drift between the leading and trailing quarters (0.20 = 20%)")
	timeout := flag.Duration("timeout", 10*time.Second, "per-scrape HTTP timeout")
	warmup := flag.Int("warmup", 0, "drop the first N samples per series before judging (keeps the boot herd out of the plateau window)")
	out := flag.String("out", "", "append sampled series here as TSV, replayable via --sample")
	sample := flag.String("sample", "", "re-verify a sample file instead of scraping")
	flag.Var(&urls, "metrics", "metrics URL, repeated or space-separated (one per replica)")
	flag.Parse()

	if *sample != "" {
		f, err := os.Open(*sample)
		if err != nil {
			fmt.Fprintf(os.Stderr, "soakcheck: open sample file: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		samples, err := ReadSamples(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "soakcheck: read sample file: %v\n", err)
			os.Exit(2)
		}
		os.Exit(verdict(samples, *drift, *warmup, "", ""))
	}

	if len(urls) == 0 {
		fmt.Fprintln(os.Stderr, "usage: soakcheck --metrics URL [URL ...] [--interval 60s] [--duration 24h] [--out FILE]")
		os.Exit(2)
	}

	ctx := context.Background()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	} else {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
	}

	start := time.Now()
	fmt.Fprintf(os.Stderr, "soakcheck: %s scraping %s every %s (tolerated drift %.0f%%)\n",
		start.Format(time.RFC3339), urls, *interval, *drift*100)
	samples := scrape(ctx, urls, *interval, *timeout)
	fmt.Fprintf(os.Stderr, "soakcheck: %s sampled %d points over %s\n",
		time.Now().Format(time.RFC3339), len(samples), time.Since(start).Round(time.Second))
	if *out != "" {
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "soakcheck: open output: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		if err := WriteSamples(f, samples); err != nil {
			fmt.Fprintf(os.Stderr, "soakcheck: write output: %v\n", err)
			os.Exit(2)
		}
	}
	os.Exit(verdict(samples, *drift, *warmup, start.Format(time.RFC3339), time.Now().Format(time.RFC3339)))
}

// scrape ticks until ctx is done, taking one scrape of every URL per tick.
// Scrapes fail soft: a replica that stops answering yields gaps, and a gap is
// exactly the restart shape the verdict is meant to surface.
func scrape(ctx context.Context, urls []string, iv, timeout time.Duration) []Sample {
	client := &http.Client{Timeout: timeout}
	if iv <= 0 {
		iv = time.Second
	}
	var out []Sample
	take := func() {
		now := time.Now()
		for _, u := range urls {
			m, err := fetch(client, u)
			if err != nil {
				continue
			}
			for k, v := range m {
				out = append(out, Sample{At: now, Source: u, Metric: k, Value: v})
			}
		}
	}
	take()
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return out
		case <-t.C:
			take()
		}
	}
}

func fetch(client *http.Client, u string) (map[string]float64, error) {
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: status %d", u, resp.StatusCode)
	}
	return ParseMetrics(resp.Body)
}

// verdict runs the plateau check over every series and returns the process
// code: 0 all pass, 1 any series fails the plateau or is too thin to judge,
// 2 no samples at all (nothing was reachable).
func verdict(samples []Sample, drift float64, warmup int, winStart, winEnd string) int {
	if winStart != "" {
		fmt.Fprintf(os.Stdout, "window %s .. %s\n", winStart, winEnd)
	}
	if len(samples) == 0 {
		fmt.Fprintln(os.Stderr, "soakcheck: no samples (no replica answered, or the window is shorter than two ticks)")
		return 2
	}
	series := GroupSamples(samples)
	if warmup > 0 {
		series = TrimLeading(series, warmup)
		fmt.Fprintf(os.Stderr, "soakcheck: warm-up dropped the first %d samples per series\n", warmup)
	}
	pass := true
	var vs []Verdict
	for _, s := range series {
		v := Check(s.Source, s.Metric, s.Points, drift)
		if !v.OK {
			pass = false
		}
		vs = append(vs, v)
	}
	fmt.Print(RenderText(vs))
	if !pass {
		fmt.Println("verdict: FAIL — at least one series is not a plateau")
		return 1
	}
	fmt.Println("verdict: PASS — every series holds a plateau")
	return 0
}
