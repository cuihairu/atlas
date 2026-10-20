package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// mk builds n evenly spaced points with value f(i).
func mk(n int, f func(int) float64) []Point {
	out := make([]Point, n)
	for i := 0; i < n; i++ {
		out[i] = Point{At: time.Unix(int64(i), 0), Value: f(i)}
	}
	return out
}

func TestPlateauPasses(t *testing.T) {
	pts := mk(12, func(i int) float64 { return 100 + float64(i%3) })
	v := Check("http://atlas1:8082/metrics", "go_memstats_heap_inuse_bytes", pts, 0.20)
	if !v.Plateau || !v.OK {
		t.Fatalf("flat series must hold a plateau: %+v", v)
	}
	if v.Note != "plateau" {
		t.Fatalf("note = %q", v.Note)
	}
}

func TestMonotonicGrowthFails(t *testing.T) {
	pts := mk(20, func(i int) float64 { return 100 + float64(i)*10 })
	v := Check("atlas1", "go_memstats_heap_inuse_bytes", pts, 0.20)
	if v.Plateau || v.OK {
		t.Fatalf("a leak must fail the plateau: %+v", v)
	}
	if v.Note != "monotonic growth above tolerance" {
		t.Fatalf("note = %q", v.Note)
	}
	if v.Drift <= 0 {
		t.Fatalf("drift = %v, want > 0", v.Drift)
	}
}

func TestDropFails(t *testing.T) {
	// Steady, then halved: the shape of a restart or a crash mid-soak.
	pts := mk(20, func(i int) float64 {
		if i < 10 {
			return 100
		}
		return 50
	})
	v := Check("atlas2", "go_goroutines", pts, 0.20)
	if v.Plateau || v.OK {
		t.Fatalf("a restart-shaped drop must fail: %+v", v)
	}
	if v.Note != "drop above tolerance (restart, crash, or a transient in the leading quarter)" {
		t.Fatalf("note = %q", v.Note)
	}
}

// TestBootHerdNeedsWarmUp reproduces the live shape: one big spike at the start
// (the fleet registering through sticky HAProxy) and steady state after. The
// verdict must refuse to bless it cold, and warm-up must clear it.
func TestBootHerdNeedsWarmUp(t *testing.T) {
	pts := mk(17, func(i int) float64 {
		if i == 0 {
			return 111
		}
		return 21 + float64(i%3)
	})
	series := []Series{{Source: "atlas2", Metric: "go_goroutines", Points: pts}}

	cold := Check(series[0].Source, series[0].Metric, series[0].Points, 0.20)
	if cold.Plateau || cold.OK {
		t.Fatalf("a boot transient in the judged window must fail cold: %+v", cold)
	}

	trimmed := TrimLeading([]Series{{Source: series[0].Source, Metric: series[0].Metric, Points: pts}}, 1)
	if len(trimmed[0].Points) != 16 {
		t.Fatalf("warm-up left %d points, want 16", len(trimmed[0].Points))
	}
	warm := Check(trimmed[0].Source, trimmed[0].Metric, trimmed[0].Points, 0.20)
	if !warm.Plateau || !warm.OK {
		t.Fatalf("the same series past the boot transient must hold a plateau: %+v", warm)
	}
}

func TestTrimLeadingShorterThanWarmup(t *testing.T) {
	mkSeries := func() []Series {
		return []Series{{Source: "a", Metric: "go_goroutines", Points: mk(2, func(int) float64 { return 1 })}}
	}
	out := TrimLeading(mkSeries(), 5)
	if out[0].Points != nil {
		t.Fatalf("warm-up longer than the series left %+v", out[0].Points)
	}
	v := Check(out[0].Source, out[0].Metric, out[0].Points, 0.20)
	if v.OK {
		t.Fatalf("an emptied series must not be judged a plateau: %+v", v)
	}
	if got := TrimLeading(mkSeries(), 0); len(got) != 1 || len(got[0].Points) != 2 || got[0].Points[0].Value != 1 {
		t.Fatalf("a zero warm-up must return the series untouched: %+v", got)
	}
}

// every builds n points spaced by step apart.
func every(step time.Duration, n int, f func(int) float64) []Point {
	out := make([]Point, n)
	for i := range out {
		out[i] = Point{At: time.Unix(0, int64(time.Duration(i)*step)), Value: f(i)}
	}
	return out
}

func TestCoveragePassesWhenComplete(t *testing.T) {
	v := Check("a", "go_goroutines", every(time.Minute, 20, func(int) float64 { return 100 }), 0.20)
	got, cov := AssessCoverage(v, time.Minute, every(time.Minute, 20, func(int) float64 { return 100 }))
	if got.Expected != 20 || cov.Missing != 0 {
		t.Fatalf("expected %d missing %d, want 20 and 0", got.Expected, cov.Missing)
	}
	if !got.Plateau || !got.OK {
		t.Fatalf("a fully covered plateau must stand: %+v", got)
	}
}

func TestCoverageGapFails(t *testing.T) {
	// 20 points three minutes apart on a one-minute scrape: two of every three
	// minutes of the soak were never observed.
	pts := every(3*time.Minute, 20, func(int) float64 { return 100 })
	got, cov := AssessCoverage(Check("a", "go_goroutines", pts, 0.20), time.Minute, pts)
	if cov.Expected != 58 || cov.Missing != 38 {
		t.Fatalf("expected %d missing %d, want 58 and 38", cov.Expected, cov.Missing)
	}
	if got.Plateau || got.OK {
		t.Fatalf("a hole that big must fail the verdict: %+v", got)
	}
	if got.Note != "coverage gap: 38 of 58 ticks missing" {
		t.Fatalf("note = %q", got.Note)
	}
}

func TestCoverageToleranceAbsorbsOneLateTick(t *testing.T) {
	// A run that ends between ticks creates no hole: 17 ticks of a 160s span is
	// a complete 17-tick window.
	pts := every(10*time.Second, 17, func(int) float64 { return 100 })
	if got, cov := AssessCoverage(Check("a", "go_goroutines", pts, 0.20), 10*time.Second, pts); cov.Expected != 17 || cov.Missing != 0 || !got.OK {
		t.Fatalf("a truncated tail is not blindness: expected %d missing %d %+v", cov.Expected, cov.Missing, got)
	}
	// One late scrape is what the tolerance is for: 18 predicted ticks, 17 seen.
	late := every(10*time.Second, 16, func(int) float64 { return 100 })
	late = append(late, Point{At: time.Unix(0, int64(175*time.Second)), Value: 100})
	got, cov := AssessCoverage(Check("a", "go_goroutines", late, 0.20), 10*time.Second, late)
	if cov.Expected != 18 || cov.Missing != 1 {
		t.Fatalf("expected %d missing %d, want 18 and 1", cov.Expected, cov.Missing)
	}
	if !got.Plateau || !got.OK {
		t.Fatalf("one tick of tolerance must not fail the run: %+v", got)
	}
}

func TestCoverageSkippedWithoutAnInterval(t *testing.T) {
	pts := every(time.Minute, 20, func(int) float64 { return 100 })
	got := Check("a", "go_goroutines", pts, 0.20)
	if _, out := AssessCoverage(got, 0, pts); out.Expected != 0 {
		t.Fatalf("no interval means no coverage claim: %+v", out)
	}
	if _, out := AssessCoverage(got, time.Minute, pts[:1]); out.Expected != 0 {
		t.Fatalf("one point cannot span a window: %+v", out)
	}
}

func TestCountsShowsCoverage(t *testing.T) {
	if got := counts(Verdict{Samples: 17, Expected: 18}); got != "17/18" {
		t.Fatalf("counts with coverage = %q", got)
	}
	if got := counts(Verdict{Samples: 17}); got != "17" {
		t.Fatalf("counts without coverage = %q", got)
	}
}

func TestWobbleWithinTolerancePasses(t *testing.T) {
	// A 15% band around the base level is drift, not creep: ±8% of the mean.
	pts := mk(16, func(i int) float64 {
		if i%2 == 0 {
			return 100
		}
		return 115
	})
	v := Check("atlas1", "process_open_fds", pts, 0.20)
	if !v.Plateau || !v.OK {
		t.Fatalf("a bounded wobble must pass: %+v", v)
	}
}

func TestOnlyTheEndQuartersAreCompared(t *testing.T) {
	// 16 points: the verdict must compare pts[:4] against pts[12:], and a huge
	// excursion in the middle of the run must not be visible to it.
	pts := mk(16, func(i int) float64 {
		switch {
		case i < 4:
			return 100
		case i < 12:
			return 999999
		default:
			return 110
		}
	})
	v := Check("atlas1", "go_memstats_heap_inuse_bytes", pts, 0.20)
	if v.FirstMean != 100 || v.LastMean != 110 {
		t.Fatalf("means = %v/%v, want the leading and trailing quarters 100/110", v.FirstMean, v.LastMean)
	}
	if v.Drift != 0.10 {
		t.Fatalf("drift = %v, want 0.10", v.Drift)
	}
	if !v.Plateau || !v.OK {
		t.Fatalf("10%% drift is inside the 20%% tolerance: %+v", v)
	}
}

func TestInsufficientSamples(t *testing.T) {
	v := Check("atlas1", "go_goroutines", mk(5, func(int) float64 { return 100 }), 0.20)
	if v.Plateau || v.OK {
		t.Fatalf("5 points cannot be judged: %+v", v)
	}
	if !strings.Contains(v.Note, "insufficient samples (5 < 8)") {
		t.Fatalf("note = %q", v.Note)
	}
}

func TestZeroLeadingQuarter(t *testing.T) {
	// 20 points, so the leading quarter is the first five.
	pts := mk(20, func(i int) float64 {
		if i < 5 {
			return 0
		}
		return 100
	})
	v := Check("atlas1", "go_goroutines", pts, 0.20)
	if v.Plateau || v.OK {
		t.Fatalf("a zero base cannot support a drift ratio: %+v", v)
	}
	if v.Note != "leading quarter is zero; drift is not definable" {
		t.Fatalf("note = %q", v.Note)
	}
}

func TestParseMetrics(t *testing.T) {
	body := strings.Join([]string{
		"# HELP go_memstats_heap_inuse_bytes Bytes in use.",
		"# TYPE go_memstats_heap_inuse_bytes gauge",
		"go_memstats_heap_inuse_bytes 1234567",
		"go_memstats_heap_alloc_bytes 99",
		"go_goroutines 42",
		"go_goroutines{foo=\"bar\"} 7",
		"process_open_fds 130",
		"http_requests_total{method=\"GET\"} 5",
		"",
		"go_memstats_heap_inuse_bytes not-a-number",
	}, "\n")
	got, err := ParseMetrics(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"go_memstats_heap_inuse_bytes": 1234567,
		"go_goroutines":                42,
		"process_open_fds":             130,
	}
	for m, v := range want {
		if got[m] != v {
			t.Errorf("%s = %v, want %v", m, got[m], v)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %v, want only the three plateau series", got)
	}
}

func TestSampleFileRoundTrip(t *testing.T) {
	in := []Sample{
		{At: time.Unix(1700000000, 0), Source: "a", Metric: "go_goroutines", Value: 42},
		{At: time.Unix(1700000060, 0), Source: "b", Metric: "process_open_fds", Value: 130},
		{At: time.Unix(1700000120, 0), Source: "a", Metric: "process_open_fds", Value: 1.5e9},
	}
	var buf bytes.Buffer
	if err := WriteSamples(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadSamples(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("round trip = %d samples, want %d\n%s", len(out), len(in), buf.String())
	}
	for i, s := range in {
		if out[i].Source != s.Source || out[i].Metric != s.Metric || out[i].At != s.At {
			t.Errorf("row %d = %+v, want %+v", i, out[i], s)
		}
		if out[i].Value != s.Value {
			t.Errorf("row %d value = %v, want %v", i, out[i].Value, s.Value)
		}
	}
}

func TestReadSamplesSkipsGarbage(t *testing.T) {
	body := "# comment\n\nnot four fields\nabc\ta\tb\t1\n1\tatlas1\tgo_goroutines\t42\ntail garbage\n"
	out, err := ReadSamples(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Metric != "go_goroutines" || out[0].Value != 42 {
		t.Fatalf("parsed %+v, want the single clean row", out)
	}
}

func TestGroupSamplesKeepsOrderAndSplit(t *testing.T) {
	s := []Sample{
		{At: time.Unix(1, 0), Source: "a", Metric: "go_goroutines", Value: 10},
		{At: time.Unix(2, 0), Source: "b", Metric: "go_goroutines", Value: 20},
		{At: time.Unix(3, 0), Source: "a", Metric: "go_goroutines", Value: 11},
		{At: time.Unix(4, 0), Source: "a", Metric: "process_open_fds", Value: 100},
		{At: time.Unix(5, 0), Source: "a", Metric: "go_goroutines", Value: 12},
	}
	series := GroupSamples(s)
	if len(series) != 3 {
		t.Fatalf("got %d series, want 3 (source, metric)", len(series))
	}
	want := []struct {
		src, metric string
		n           int
		last        float64
	}{
		{"a", "go_goroutines", 3, 12},
		{"b", "go_goroutines", 1, 20},
		{"a", "process_open_fds", 1, 100},
	}
	for i, w := range want {
		g := series[i]
		if g.Source != w.src || g.Metric != w.metric {
			t.Fatalf("series %d = %s/%s, want %s/%s", i, g.Source, g.Metric, w.src, w.metric)
		}
		if len(g.Points) != w.n {
			t.Fatalf("series %d has %d points, want %d", i, len(g.Points), w.n)
		}
		if g.Points[len(g.Points)-1].Value != w.last {
			t.Fatalf("series %d last = %v, want %v (points out of order)", i, g.Points[len(g.Points)-1].Value, w.last)
		}
	}
}

func TestRenderTextShowsEveryRowAndTheVerdict(t *testing.T) {
	vs := []Verdict{
		{Source: "http://atlas1:8082/metrics", Metric: "go_memstats_heap_inuse_bytes", Samples: 1440, Min: 1.0e8, Max: 1.02e8, Drift: 0.02, Plateau: true, OK: true, Note: "plateau"},
		{Source: "http://atlas2:8082/metrics", Metric: "go_goroutines", Samples: 7, Plateau: false, Note: "insufficient samples (7 < 8)"},
	}
	out := RenderText(vs)
	for _, needle := range []string{
		"go_memstats_heap_inuse_bytes",
		"go_goroutines",
		"insufficient samples (7 < 8)",
		"plateau",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("render missing %q:\n%s", needle, out)
		}
	}
}
