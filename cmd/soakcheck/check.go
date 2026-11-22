// soakcheck turns the §9 soak plateau criterion (docs/performance.md §9) into
// a mechanical verdict: it samples the runtime series that a ≥24h soak is
// judged on — steady-state heap, goroutine count and open fds — and reports
// per replica whether each series holds a plateau instead of creeping. The
// pass condition is plateaus, not peaks, so the gate compares the trailing
// quarter of the run against the leading quarter rather than the extremes.
package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// plateauMetrics are the series §9's soak verdict is defined on. All three
// are unlabeled gauges off the standard Go and process collectors.
var plateauMetrics = []string{
	"go_memstats_heap_inuse_bytes",
	"go_goroutines",
	"process_open_fds",
}

// Point is one scrape of one metric at one instant.
type Point struct {
	At    time.Time
	Value float64
}

// Series is one metric on one replica, oldest first.
type Series struct {
	Source string // e.g. http://atlas1:8082/metrics
	Metric string
	Points []Point
}

// Verdict is the plateau check for one Series.
type Verdict struct {
	Source string
	Metric string

	Samples int
	Min     float64
	Max     float64

	FirstMean float64 // mean of the leading quarter
	LastMean  float64 // mean of the trailing quarter
	Drift     float64 // (LastMean-FirstMean)/FirstMean; +creep, -restart-shaped drop

	Plateau bool // within the tolerance
	OK      bool // Plateau, covered, and enough samples to judge
	Note    string

	Expected int // ticks the interval predicts over this span; 0 = not assessed
}

// Coverage is the run-validity half of a soak verdict: a plateau judged over a
// window the checker could not observe is not evidence, so missing ticks fail
// the verdict the same way a leak does.
type Coverage struct {
	Expected int
	Missing  int
	Note     string
}

const minSamples = 8 // quarters need at least 8 points to mean anything

// Check decides the plateau verdict for one series against drift (a fraction:
// 0.2 = 20% tolerated drift between the leading and trailing quarters).
// Both directions fail: steady growth is a leak, a drop of that size is a
// restart or a crash that no soak verdict can be trusted across.
func Check(src, metric string, pts []Point, drift float64) Verdict {
	v := Verdict{Source: src, Metric: metric, Samples: len(pts), Plateau: true, Note: "plateau"}
	// The observed range is reported even for a too-thin series, so a row that
	// cannot be judged still shows what was actually seen.
	for i, p := range pts {
		if i == 0 || p.Value < v.Min {
			v.Min = p.Value
		}
		if i == 0 || p.Value > v.Max {
			v.Max = p.Value
		}
	}
	if len(pts) < minSamples {
		v.Plateau = false
		v.Note = fmt.Sprintf("insufficient samples (%d < %d)", len(pts), minSamples)
		return v
	}
	first, last := quarterMeans(pts)
	if first == 0 {
		v.Plateau = false
		v.Note = "leading quarter is zero; drift is not definable"
		return v
	}
	v.FirstMean = first
	v.LastMean = last
	v.Drift = (last - first) / first
	if v.Drift > drift || v.Drift < -drift {
		v.Plateau = false
		if v.Drift > 0 {
			v.Note = "monotonic growth above tolerance"
		} else {
			v.Note = "drop above tolerance (restart, crash, or a transient in the leading quarter)"
		}
	}
	// A verdict is only trustworthy with enough samples to compare quarters.
	v.OK = v.Plateau
	return v
}

// quarterMeans returns the leading-quarter and trailing-quarter means. The
// quarters are positional, not time-based: the criterion is "is the tail at the
// same level as the head", so both are measured over whatever the run recorded.
func quarterMeans(pts []Point) (first, last float64) {
	q := len(pts) / 4
	if q < 2 {
		q = 2
	}
	return mean(vals(pts[:len(pts)-3*q])), mean(vals(pts[len(pts)-q:]))
}

// Window is a span of a run the plateau verdict does not judge: one drill's
// perturbation (outage window open .. stack back to healthy) plus a recovery
// grace after it. Points inside a Window are excluded from the verdict.
type Window struct {
	Start, End time.Time
}

// ParseDrillWindows reads a drills.sh log and returns the perturbation spans.
// Each "outage window open" line opens a span and each "stack back to healthy"
// line closes the current one; a span left open at EOF is closed at the last
// timestamp the log shows. grace is appended after every close, so a Window
// covers the perturbation plus the settling the plateau must not judge across.
// Lines without a leading RFC3339 timestamp are skipped, so a log that also
// carries non-drill chatter stays parseable.
func ParseDrillWindows(r io.Reader, grace time.Duration) ([]Window, error) {
	var wins []Window
	var open *time.Time
	last := time.Time{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, f[0])
		if err != nil {
			continue
		}
		last = ts
		rest := strings.Join(f[1:], " ")
		switch {
		case strings.Contains(rest, "outage window open"):
			t := ts
			open = &t
		case strings.Contains(rest, "stack back to healthy"):
			if open != nil {
				wins = append(wins, Window{Start: *open, End: ts.Add(grace)})
				open = nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if open != nil && last.After(*open) {
		// A run that ends mid-drill still gets its window recorded, so the
		// verdict never judges a span it cannot trust to be steady.
		wins = append(wins, Window{Start: *open, End: last.Add(grace)})
	}
	sort.Slice(wins, func(i, j int) bool { return wins[i].Start.Before(wins[j].Start) })
	return wins, nil
}

// inWindow reports whether t falls inside any of the windows.
func inWindow(t time.Time, wins []Window) bool {
	for _, w := range wins {
		if !t.Before(w.Start) && !t.After(w.End) {
			return true
		}
	}
	return false
}

// splitByWindows removes points inside any window and groups the rest into
// contiguous steady segments. Crossing a window — entering it or jumping past
// its end in one scrape gap — closes the current segment, so no comparison ever
// straddles a drill boundary.
func splitByWindows(pts []Point, wins []Window) [][]Point {
	if len(wins) == 0 {
		return [][]Point{pts}
	}
	var segs [][]Point
	var cur []Point
	wi := 0
	for _, p := range pts {
		for wi < len(wins) && p.At.After(wins[wi].End) {
			if len(cur) > 0 {
				segs = append(segs, cur)
				cur = nil
			}
			wi++
		}
		if wi < len(wins) && !p.At.Before(wins[wi].Start) {
			if len(cur) > 0 {
				segs = append(segs, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, p)
	}
	if len(cur) > 0 {
		segs = append(segs, cur)
	}
	return segs
}

// CheckWithWindows is Check for a run with fault drills folded in (§9): points
// inside a drill window (perturbation + recovery grace) are excluded, and the
// plateau is judged within each steady segment the windows leave behind. A
// one-time level step at a drill boundary is the drill's expected effect — a
// fresh process trims its pools to real demand — so a negative cross-run drift
// is not a failure there. What still fails: creep inside any steady segment, a
// drop inside a segment (an unplanned crash is not at a window), and growth
// across the whole run measured head-to-tail.
func CheckWithWindows(src, metric string, pts []Point, drift float64, wins []Window) Verdict {
	if len(wins) == 0 {
		return Check(src, metric, pts, drift)
	}
	segs := splitByWindows(pts, wins)
	v := Verdict{Source: src, Metric: metric, Samples: 0, Plateau: true}
	var judged []Point
	for _, seg := range segs {
		judged = append(judged, seg...)
	}
	v.Samples = len(judged)
	if v.Samples == 0 {
		v.Plateau = false
		v.Note = fmt.Sprintf("insufficient samples (all %d points inside drill windows)", len(pts))
		return v
	}
	for i, p := range judged {
		if i == 0 || p.Value < v.Min {
			v.Min = p.Value
		}
		if i == 0 || p.Value > v.Max {
			v.Max = p.Value
		}
	}
	var firstMean, lastMean float64
	haveFirst := false
	failed := false
	for i, seg := range segs {
		if len(seg) < minSamples {
			continue // too thin to judge; the sample count on the row says so
		}
		first, last := quarterMeans(seg)
		if first == 0 {
			v.Plateau = false
			v.OK = false
			v.Note = fmt.Sprintf("leading quarter is zero in steady segment %d", i+1)
			return v
		}
		if !haveFirst {
			firstMean, haveFirst = first, true
		}
		lastMean = last
		d := (last - first) / first
		if d > drift {
			failed = true
			if v.Note == "" {
				v.Note = fmt.Sprintf("monotonic growth above tolerance in steady segment %d", i+1)
			}
		} else if d < -drift {
			failed = true
			if v.Note == "" {
				v.Note = fmt.Sprintf("drop above tolerance in steady segment %d (restart, crash, or a transient in the leading quarter)", i+1)
			}
		}
	}
	if !haveFirst {
		v.Plateau = false
		v.Note = fmt.Sprintf("insufficient samples (%d points split into %d segments, none judgeable)", len(pts), len(segs))
		return v
	}
	v.FirstMean = firstMean
	v.LastMean = lastMean
	v.Drift = (lastMean - firstMean) / firstMean
	if v.Drift > drift {
		// Growth that survives every restart is a leak no segment boundary hid.
		failed = true
		if v.Note == "" {
			v.Note = "growth across the run above tolerance"
		}
	}
	if failed {
		v.Plateau = false
	} else {
		v.Note = fmt.Sprintf("plateau across %d steady segments (drill windows excluded)", len(segs))
	}
	v.OK = v.Plateau
	return v
}

// AssessCoverage checks that the series actually covers the window the plateau
// verdict was taken over: interval ticks predict the expected count, and a hole
// bigger than the tolerance means part of the soak was never observed. One tick
// of tolerance absorbs a run that ended between ticks, and the floor rises to
// 5% for long runs where the last scrape can legitimately slip past the window.
// Drill windows count as planned holes: the ticks they cover are neither
// predicted nor observed, so a folded-in drill does not read as a gap.
func AssessCoverage(v Verdict, interval time.Duration, pts []Point, wins []Window) (Verdict, Coverage) {
	if interval <= 0 || len(pts) < 2 {
		return v, Coverage{}
	}
	expected := 0
	for k := 0; ; k++ {
		t := pts[0].At.Add(time.Duration(k) * interval)
		if t.After(pts[len(pts)-1].At) {
			break
		}
		if !inWindow(t, wins) {
			expected++
		}
	}
	observed := 0
	for _, p := range pts {
		if !inWindow(p.At, wins) {
			observed++
		}
	}
	if expected < 1 {
		expected = 1
	}
	missing := expected - observed
	v.Expected = expected
	if missing < 0 {
		missing = 0
	}
	tol := expected / 20
	if tol < 1 {
		tol = 1
	}
	if missing > tol {
		v.Plateau = false
		v.OK = false
		v.Note = fmt.Sprintf("coverage gap: %d of %d ticks missing", missing, expected)
	}
	return v, Coverage{Expected: expected, Missing: missing, Note: v.Note}
}

func vals(pts []Point) []float64 {
	out := make([]float64, len(pts))
	for i, p := range pts {
		out[i] = p.Value
	}
	return out
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

// Sample is one scraped point, kept so a soak can be re-audited offline:
// --out records them during the run, --sample re-verdicts them later without
// re-running the machine.
type Sample struct {
	At     time.Time
	Source string
	Metric string
	Value  float64
}

// WriteSamples writes samples as TSV: epoch\tsource\tmetric\tvalue.
func WriteSamples(w io.Writer, samples []Sample) error {
	bw := bufio.NewWriter(w)
	defer func() { _ = bw.Flush() }()
	for _, s := range samples {
		if _, err := fmt.Fprintf(bw, "%d\t%s\t%s\t%.6g\n", s.At.Unix(), s.Source, s.Metric, s.Value); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// ReadSamples parses the TSV written by WriteSamples; blank lines and lines
// that do not carry four fields are skipped.
func ReadSamples(r io.Reader) ([]Sample, error) {
	var out []Sample
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			continue
		}
		at, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		val, err := strconv.ParseFloat(f[3], 64)
		if err != nil {
			continue
		}
		out = append(out, Sample{At: time.Unix(at, 0), Source: f[1], Metric: f[2], Value: val})
	}
	return out, sc.Err()
}

// GroupSamples splits samples into one Series per (source, metric) in scrape
// order, so the verdict runs per replica per metric.
func GroupSamples(samples []Sample) []Series {
	type key struct{ src, metric string }
	idx := map[key]int{}
	var order []key
	for _, s := range samples {
		k := key{s.Source, s.Metric}
		if _, ok := idx[k]; !ok {
			idx[k] = len(order)
			order = append(order, k)
		}
	}
	series := make([]Series, len(order))
	for i, k := range order {
		series[i] = Series{Source: k.src, Metric: k.metric}
	}
	for _, s := range samples {
		i := idx[key{s.Source, s.Metric}]
		series[i].Points = append(series[i].Points, Point{At: s.At, Value: s.Value})
	}
	return series
}

// TrimLeading drops the first n points of every series so a warm-up knob can
// keep the boot herd outside the judged window: a fleet registering at once is
// one big transient, and the plateau criterion is about steady state, not about
// whether the soak started cold. Points already below n are emptied, which the
// verdict then reports as insufficient samples rather than a plateau.
func TrimLeading(series []Series, n int) []Series {
	if n <= 0 {
		return series
	}
	for i := range series {
		if len(series[i].Points) > n {
			series[i].Points = series[i].Points[n:]
			continue
		}
		series[i].Points = nil
	}
	return series
}

// RenderText renders the verdict table: drift is the trailing-quarter mean
// against the leading-quarter mean, so creep shows as positive and a
// restart-shaped drop as negative.
func RenderText(vs []Verdict) string {
	var b strings.Builder
	b.WriteString("soakcheck plateau verdict\n")
	fmt.Fprintf(&b, "%-28s %-28s %7s %14s %14s %8s  %s\n",
		"source", "metric", "ticks", "min", "max", "drift", "verdict")
	for _, v := range vs {
		fmt.Fprintf(&b, "%-28s %-28s %7s %14s %14s %+7.1f%%  %s\n",
			v.Source, v.Metric, counts(v), trim(v.Min), trim(v.Max), v.Drift*100, v.Note)
	}
	return b.String()
}

// counts shows how many of the predicted ticks were actually observed, so a
// partially-blind window is visible at a glance instead of hidden in a mean.
func counts(v Verdict) string {
	if v.Expected > 0 {
		return fmt.Sprintf("%d/%d", v.Samples, v.Expected)
	}
	return fmt.Sprintf("%d", v.Samples)
}

// trim keeps a magnitude readable without losing the order of growth.
func trim(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.2fK", v/1e3)
	default:
		return fmt.Sprintf("%.1f", v)
	}
}

// ParseMetrics reads one Prometheus text exposition and returns the values of
// plateauMetrics it contains. Comment lines and series off the list are
// ignored; labels on a wanted name are skipped rather than matched, since all
// three series are unlabeled.
func ParseMetrics(r io.Reader) (map[string]float64, error) {
	want := make(map[string]bool, len(plateauMetrics))
	for _, m := range plateauMetrics {
		want[m] = true
	}
	out := make(map[string]float64, len(plateauMetrics))
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		name := line
		if i := strings.IndexByte(name, ' '); i >= 0 {
			name = name[:i]
		}
		if !want[name] {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		out[name] = v
	}
	return out, sc.Err()
}
