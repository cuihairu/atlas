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
	got, cov := AssessCoverage(v, time.Minute, every(time.Minute, 20, func(int) float64 { return 100 }), nil)
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
	got, cov := AssessCoverage(Check("a", "go_goroutines", pts, 0.20), time.Minute, pts, nil)
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
	if got, cov := AssessCoverage(Check("a", "go_goroutines", pts, 0.20), 10*time.Second, pts, nil); cov.Expected != 17 || cov.Missing != 0 || !got.OK {
		t.Fatalf("a truncated tail is not blindness: expected %d missing %d %+v", cov.Expected, cov.Missing, got)
	}
	// One late scrape is what the tolerance is for: 18 predicted ticks, 17 seen.
	late := every(10*time.Second, 16, func(int) float64 { return 100 })
	late = append(late, Point{At: time.Unix(0, int64(175*time.Second)), Value: 100})
	got, cov := AssessCoverage(Check("a", "go_goroutines", late, 0.20), 10*time.Second, late, nil)
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
	if _, out := AssessCoverage(got, 0, pts, nil); out.Expected != 0 {
		t.Fatalf("no interval means no coverage claim: %+v", out)
	}
	if _, out := AssessCoverage(got, time.Minute, pts[:1], nil); out.Expected != 0 {
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

func at(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestParseDrillWindows(t *testing.T) {
	log := `2026-11-22T14:52:42+08:00 drill=replica-kill service=atlas1 project=atlas-ha down=30s
2026-11-22T14:53:12+08:00 atlas1 stopped; outage window open
2026-11-22T14:53:42+08:00 atlas1 started; waiting for healthy
2026-11-22T14:53:57+08:00 drill=replica-kill complete; stack back to healthy
2026-11-22T14:55:00+08:00 drill=pg-outage service=postgres-primary project=atlas-ha down=20s
2026-11-22T14:55:20+08:00 postgres-primary stopped; outage window open
2026-11-22T14:55:40+08:00 postgres-primary started; waiting for healthy
2026-11-22T14:55:52+08:00 drill=pg-outage complete; stack back to healthy
`
	wins, err := ParseDrillWindows(strings.NewReader(log), 2*time.Minute)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(wins) != 2 {
		t.Fatalf("got %d windows, want 2: %+v", len(wins), wins)
	}
	if !wins[0].Start.Equal(at(t, "2026-11-22T14:53:12+08:00")) || !wins[0].End.Equal(at(t, "2026-11-22T14:55:57+08:00")) {
		t.Fatalf("window 0 = %s..%s, want open at 14:53:12 and close+grace at 14:55:57+08:00", wins[0].Start, wins[0].End)
	}
	if !wins[1].Start.Equal(at(t, "2026-11-22T14:55:20+08:00")) || !wins[1].End.Equal(at(t, "2026-11-22T14:57:52+08:00")) {
		t.Fatalf("window 1 = %s..%s, want open at 14:55:20 and close+grace at 14:57:52+08:00", wins[1].Start, wins[1].End)
	}
}

func TestParseDrillWindowsClosesAnUnclosedSpan(t *testing.T) {
	log := `2026-11-22T14:52:42+08:00 drill=replica-kill service=atlas2 project=atlas-ha down=30s
2026-11-22T14:53:12+08:00 atlas2 stopped; outage window open
2026-11-22T14:55:40+08:00 atlas2 started; waiting for healthy
`
	wins, err := ParseDrillWindows(strings.NewReader(log), 2*time.Minute)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(wins) != 1 {
		t.Fatalf("got %d windows, want 1 (span left open at EOF closes at the last timestamp)", len(wins))
	}
	if want := at(t, "2026-11-22T14:57:40+08:00"); !wins[0].End.Equal(want) {
		t.Fatalf("unclosed window end = %s, want %s", wins[0].End, want)
	}
}

func TestParseDrillWindowsSkipsNonTimestampLines(t *testing.T) {
	log := `some chatter without a timestamp
2026-11-22T14:53:12+08:00 atlas1 stopped; outage window open
still not a timestamp
2026-11-22T14:53:57+08:00 drill=replica-kill complete; stack back to healthy
`
	wins, err := ParseDrillWindows(strings.NewReader(log), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(wins) != 1 {
		t.Fatalf("got %d windows, want 1 despite the non-timestamp chatter", len(wins))
	}
}

func TestSplitByWindowsGroupsSteadySegments(t *testing.T) {
	pts := every(10*time.Second, 40, func(int) float64 { return 100 })
	wins := []Window{{Start: time.Unix(40, 0), End: time.Unix(60, 0)}}
	segs := splitByWindows(pts, wins)
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2 (a window splits the run)", len(segs))
	}
	if len(segs[0]) != 4 || len(segs[1]) != 33 {
		t.Fatalf("segment sizes = %d/%d, want 4/33 (3 ticks inside the window)", len(segs[0]), len(segs[1]))
	}
}

func TestDrillWindowLevelStepIsNotAFailure(t *testing.T) {
	// The 100-scale drill-folded run: atlas2's process_open_fds sat 51-60
	// steady before its replica-kill drill, and a fresh process trims the pool
	// to real demand (31-44). That one-time step is the drill's expected effect,
	// so a negative cross-run drift at a drill boundary must not fail the
	// verdict — while the same series without the windows stays a FAIL.
	pts := every(10*time.Second, 168, func(i int) float64 {
		if i < 40 {
			return 100 + float64(i%3)
		}
		return 60 + float64(i%3)
	})
	wins := []Window{{Start: time.Unix(400, 0), End: time.Unix(470, 0)}}
	if v := CheckWithWindows("a", "process_open_fds", pts, 0.20, nil); v.OK {
		t.Fatalf("a -40%% level step with no windows is a drop above tolerance; got OK")
	}
	if v := CheckWithWindows("a", "process_open_fds", pts, 0.20, wins); !v.OK {
		t.Fatalf("a level step at a drill boundary is the drill's expected effect; got %q (drift %.1f%%)", v.Note, v.Drift*100)
	}
}

func TestDrillWindowLeakStillFails(t *testing.T) {
	pts := every(10*time.Second, 168, func(i int) float64 {
		if i < 40 {
			return 100 + float64(i%3)
		}
		return 100 + float64(i-40)/3 // 100 → 140 inside the second segment
	})
	wins := []Window{{Start: time.Unix(400, 0), End: time.Unix(470, 0)}}
	v := CheckWithWindows("a", "go_memstats_heap_inuse_bytes", pts, 0.20, wins)
	if v.OK || !strings.Contains(v.Note, "segment 2") {
		t.Fatalf("creep inside a steady segment must still fail: %q (OK=%v)", v.Note, v.OK)
	}
}

func TestUnplannedCrashInsideSegmentFails(t *testing.T) {
	pts := every(10*time.Second, 168, func(i int) float64 {
		if i < 100 {
			return 100 + float64(i%3)
		}
		return 50 + float64(i%3) // a restart nobody scheduled
	})
	wins := []Window{{Start: time.Unix(400, 0), End: time.Unix(470, 0)}}
	v := CheckWithWindows("a", "process_open_fds", pts, 0.20, wins)
	if v.OK || !strings.Contains(v.Note, "drop above tolerance") {
		t.Fatalf("a drop that is not at a drill window is an unplanned crash: %q (OK=%v)", v.Note, v.OK)
	}
}

func TestCrossRunGrowthFails(t *testing.T) {
	// A leak that survives every restart: each segment is flat, but the run
	// grew head-to-tail across the drill boundary.
	pts := every(10*time.Second, 168, func(i int) float64 {
		if i < 40 {
			return 100 + float64(i%3)
		}
		return 140 + float64(i%3)
	})
	wins := []Window{{Start: time.Unix(400, 0), End: time.Unix(470, 0)}}
	v := CheckWithWindows("a", "go_memstats_heap_inuse_bytes", pts, 0.20, wins)
	if v.OK || !strings.Contains(v.Note, "growth across the run") {
		t.Fatalf("growth across restarts is still a leak: %q (OK=%v)", v.Note, v.OK)
	}
}

func TestCoverageIgnoresPlannedDrillWindows(t *testing.T) {
	pts := every(10*time.Second, 60, func(int) float64 { return 100 })
	wins := []Window{{Start: time.Unix(300, 0), End: time.Unix(360, 0)}} // ticks 30..36
	v := CheckWithWindows("a", "go_goroutines", pts, 0.20, wins)
	got, cov := AssessCoverage(v, 10*time.Second, pts, wins)
	if cov.Expected != 53 || cov.Missing != 0 || !got.OK {
		t.Fatalf("planned drill windows must not read as a coverage gap: expected %d missing %d ok %v", cov.Expected, cov.Missing, got.OK)
	}
}

func TestCheckWithWindowsWithoutWindowsMatchesCheck(t *testing.T) {
	pts := mk(48, func(i int) float64 { return 100 + float64(i%3) })
	got := CheckWithWindows("a", "go_goroutines", pts, 0.20, nil)
	want := Check("a", "go_goroutines", pts, 0.20)
	if got.Note != want.Note || got.Drift != want.Drift || got.OK != want.OK {
		t.Fatalf("no windows must reproduce the plain verdict: got %q/%v/%v want %q/%v/%v", got.Note, got.Drift, got.OK, want.Note, want.Drift, want.OK)
	}
}
