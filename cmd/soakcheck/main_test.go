package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// exposition is what a replica's :8082/metrics serves, including the series the
// checker ignores.
const exposition = `
# HELP go_memstats_heap_inuse_bytes Bytes in use.
# TYPE go_memstats_heap_inuse_bytes gauge
go_memstats_heap_inuse_bytes 123456
http_requests_total{method="GET"} 9
go_goroutines 33
process_open_fds 108
`

func findSeries(series []Series, src, metric string) *Series {
	for i := range series {
		if series[i].Source == src && series[i].Metric == metric {
			return &series[i]
		}
	}
	return nil
}

func TestScrapeCollectsEveryWantedSeriesFromEveryReplica(t *testing.T) {
	var hits [2]int
	mk := func(i int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits[i]++
			if r.URL.Path != "/metrics" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(exposition))
		}))
	}
	a, b := mk(0), mk(1)
	defer a.Close()
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var rec bytes.Buffer
	samples := scrape(ctx, []string{a.URL + "/metrics", b.URL + "/metrics"}, 20*time.Millisecond, 2*time.Second, &rec)

	if hits[0] < 3 || hits[1] < 3 {
		t.Fatalf("replica A scraped %d times, replica B %d times; want at least three each", hits[0], hits[1])
	}
	series := GroupSamples(samples)
	if len(series) != 6 {
		t.Fatalf("got %d series, want 6 (2 replicas x 3 plateau metrics)", len(series))
	}
	want := map[string]float64{
		"go_memstats_heap_inuse_bytes": 123456,
		"go_goroutines":                33,
		"process_open_fds":             108,
	}
	for m, val := range want {
		sa := findSeries(series, a.URL+"/metrics", m)
		sb := findSeries(series, b.URL+"/metrics", m)
		if sa == nil || sb == nil {
			t.Fatalf("missing series for %s on both replicas (A=%v B=%v)", m, sa != nil, sb != nil)
		}
		if len(sa.Points) < 3 || len(sb.Points) < 3 {
			t.Fatalf("%s: replica A has %d points, B %d; want at least three each", m, len(sa.Points), len(sb.Points))
		}
		for _, p := range sa.Points {
			if p.Value != val {
				t.Errorf("%s sample = %v, want %v", m, p.Value, val)
			}
		}
	}
	// http_requests_total is not a plateau series and must not be collected.
	for _, s := range samples {
		if s.Metric == "http_requests_total" {
			t.Fatalf("collected non-plateau series %q", s.Metric)
		}
	}
	if n := len(samples); n != 3*(hits[0]+hits[1]) {
		t.Fatalf("sampled %d points from %d scrapes; want 3 plateau series per scrape", n, hits[0]+hits[1])
	}
	// The record lands per tick, not at teardown: whatever the verdict saw is
	// already on disk, so killing a 24h soak keeps its hours.
	fromDisk, err := ReadSamples(bytes.NewReader(rec.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(fromDisk) != len(samples) {
		t.Fatalf("record holds %d samples, verdict saw %d", len(fromDisk), len(samples))
	}
	for i, s := range samples {
		if fromDisk[i].Source != s.Source || fromDisk[i].Metric != s.Metric || fromDisk[i].Value != s.Value {
			t.Fatalf("record row %d = %+v, want %+v", i, fromDisk[i], s)
		}
	}
}

func TestFetchRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := fetch(srv.Client(), srv.URL); err == nil {
		t.Fatal("a non-200 response must surface as an error, so the verdict shows the gap")
	}
}

// The exact invocation shape soak.sh once produced — a bare second URL after
// --metrics — must be refused, not silently misparsed: the flag package stops
// at the bare token, --interval/--duration land unparsed as positionals, and
// the run drifts on defaults (60s interval, run-until-interrupt) forever.
func TestSoakcheckRejectsStrayArguments(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "soakcheck-test")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"--metrics", "http://127.0.0.1:9/metrics", "http://127.0.0.1:9/metrics",
		"--interval", "10s").CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("stray arguments ran until the timeout instead of refusing:\n%s", out)
	}
	if err == nil {
		t.Fatalf("stray arguments accepted:\n%s", out)
	}
	if !strings.Contains(string(out), "unexpected arguments") {
		t.Errorf("refusal does not name the stray arguments:\n%s", out)
	}
}

func TestVerdictExitCodes(t *testing.T) {
	if got := verdict(nil, 0.20, 0, 0, "", ""); got != 2 {
		t.Fatalf("no samples: exit %d, want 2", got)
	}

	mk := func(n int, f func(int) float64) []Sample {
		out := make([]Sample, n)
		for i := range out {
			out[i] = Sample{At: time.Unix(int64(i), 0), Source: "a", Metric: "go_goroutines", Value: f(i)}
		}
		return out
	}
	if got := verdict(mk(10, func(int) float64 { return 100 }), 0.20, 0, 0, "", ""); got != 0 {
		t.Fatalf("a plateau: exit %d, want 0", got)
	}
	if got := verdict(mk(10, func(i int) float64 { return 100 + float64(i)*20 }), 0.20, 0, 0, "", ""); got != 1 {
		t.Fatalf("a leak: exit %d, want 1", got)
	}
	if got := verdict(mk(4, func(int) float64 { return 100 }), 0.20, 0, 0, "", ""); got != 1 {
		t.Fatalf("too few samples: exit %d, want 1", got)
	}

	// A boot transient that flips the exit code once it is warmed up.
	boot := mk(17, func(i int) float64 {
		if i == 0 {
			return 111
		}
		return 22
	})
	if got := verdict(boot, 0.20, 0, 0, "", ""); got != 1 {
		t.Fatalf("boot transient, cold: exit %d, want 1", got)
	}
	if got := verdict(boot, 0.20, 1, 0, "", ""); got != 0 {
		t.Fatalf("boot transient, warmed up: exit %d, want 0", got)
	}

	// A soak with a hole is not evidence, whatever the mean does.
	mkEvery := func(every time.Duration, n int) []Sample {
		out := make([]Sample, n)
		for i := range out {
			out[i] = Sample{At: time.Unix(0, int64(time.Duration(i)*every)), Source: "a", Metric: "go_goroutines", Value: 100}
		}
		return out
	}
	if got := verdict(mkEvery(time.Minute, 20), 0.20, 0, time.Minute, "", ""); got != 0 {
		t.Fatalf("a fully covered plateau: exit %d, want 0", got)
	}
	if got := verdict(mkEvery(3*time.Minute, 20), 0.20, 0, time.Minute, "", ""); got != 1 {
		t.Fatalf("a covered-by-a-third plateau: exit %d, want 1", got)
	}
}
