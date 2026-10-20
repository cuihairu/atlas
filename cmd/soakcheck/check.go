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
	OK      bool // Plateau and enough samples to judge
	Note    string
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
	q := len(pts) / 4
	if q < 2 {
		q = 2
	}
	first := mean(vals(pts[:len(pts)-3*q]))
	last := mean(vals(pts[len(pts)-q:]))
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
	defer bw.Flush()
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
	fmt.Fprintf(&b, "%-28s %-28s %6s %14s %14s %8s  %s\n",
		"source", "metric", "samples", "min", "max", "drift", "verdict")
	for _, v := range vs {
		fmt.Fprintf(&b, "%-28s %-28s %6d %14s %14s %+7.1f%%  %s\n",
			v.Source, v.Metric, v.Samples, trim(v.Min), trim(v.Max), v.Drift*100, v.Note)
	}
	return b.String()
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
