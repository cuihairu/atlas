// Heatmap aggregation (TODO 服务器热力图): turns the per-server load series
// into the bucketed averages the dashboard heatmap colors. Everything is
// derived from the same rings the 负载时间视图 reads — one sampler, one truth.
package telemetry

import (
	"sort"
	"strconv"
	"time"

	"github.com/cuihairu/atlas/internal/model"
)

// HeatBucket is one time bucket of a server's series: the average over the
// bucket span. Samples == 0 means no data in the bucket (sampler restart,
// server registered mid-window, …) — consumers must render that as its own
// "no data" state, never as a zero-load color.
type HeatBucket struct {
	T       time.Time `json:"t"`
	Load    float64   `json:"load"`
	Players float64   `json:"players"`
	Samples int       `json:"samples"`
}

// ServerHeatmap is one grid row: the live server record (same snapshot the
// admin lists read) joined with its bucketed load history.
type ServerHeatmap struct {
	ServerID string       `json:"server_id"`
	Name     string       `json:"name"`
	Region   string       `json:"region"`
	Type     string       `json:"type"`
	Status   string       `json:"status"`
	Players  int          `json:"players"`
	Capacity int          `json:"capacity"`
	Load     float64      `json:"load"`
	Buckets  []HeatBucket `json:"buckets"`
}

// ParseHeatmapWindow maps the heatmap window tiers: "1h" → 12 buckets of
// 5m, "24h" → 24 buckets of 1h. Empty defaults to 1h. Kept separate from
// the chart tiers (5m…10h) because the heatmap trades resolution for span:
// 24h needs the sampler retention raised accordingly.
func ParseHeatmapWindow(raw string) (window time.Duration, buckets int, ok bool) {
	switch raw {
	case "", "1h":
		return time.Hour, 12, true
	case "24h":
		return 24 * time.Hour, 24, true
	}
	return 0, 0, false
}

// BucketSpanLabel renders a bucket span compactly ("5m", "1h") for the
// heatmap envelope — shared by the REST and gRPC faces so both answer with
// the same label.
func BucketSpanLabel(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/time.Minute)) + "m"
}

// bucketizeAverages accumulates one series into per-bucket sums and sample
// counts over the window ending at now. All bucketCount buckets are present;
// sparse series leave zero samples rather than shifting the grid.
func bucketizeAverages(points []Point, window time.Duration, bucketCount int, now time.Time) (sums []float64, samples []int) {
	span := window / time.Duration(bucketCount)
	start := now.Add(-window)
	sums = make([]float64, bucketCount)
	samples = make([]int, bucketCount)
	for _, p := range points {
		if p.T.Before(start) || p.T.After(now) {
			continue
		}
		idx := int(p.T.Sub(start) / span)
		if idx >= bucketCount {
			idx = bucketCount - 1
		}
		sums[idx] += p.V
		samples[idx]++
	}
	return sums, samples
}

// BucketizePair averages a load series and a players series (two rings of
// the same probe, index-aligned like the chart zip) into shared buckets.
func BucketizePair(load, players []Point, window time.Duration, bucketCount int, now time.Time) []HeatBucket {
	loadSums, loadN := bucketizeAverages(load, window, bucketCount, now)
	playerSums, playerN := bucketizeAverages(players, window, bucketCount, now)

	span := window / time.Duration(bucketCount)
	start := now.Add(-window)
	out := make([]HeatBucket, bucketCount)
	for i := range out {
		b := HeatBucket{T: start.Add(span * time.Duration(i))}
		if loadN[i] > 0 {
			b.Load = loadSums[i] / float64(loadN[i])
			b.Players = playerSums[i] / float64(playerN[i])
			b.Samples = loadN[i]
		}
		out[i] = b
	}
	return out
}

// BuildServerHeatmap joins live server records with their per-server series
// (names "load.server.<id>.load" / "load.server.<id>.players", the same
// rings the 负载时间视图 serves). series may be nil for embedders without a
// sampler: rows still carry the live record with empty buckets. Rows sort
// by region then server id so the grid is stable across refreshes.
func BuildServerHeatmap(servers []*model.Server, series func(name string, window time.Duration) []Point, window time.Duration, bucketCount int, now time.Time) []ServerHeatmap {
	out := make([]ServerHeatmap, 0, len(servers))
	for _, srv := range servers {
		if srv == nil {
			continue
		}
		row := ServerHeatmap{
			ServerID: srv.ID,
			Name:     srv.Name,
			Region:   srv.Region,
			Type:     srv.Type,
			Status:   string(srv.Status),
			Players:  srv.Players,
			Capacity: srv.Capacity,
			Load:     srv.Load,
			Buckets:  []HeatBucket{},
		}
		if series != nil {
			load := series("load.server."+srv.ID+".load", window)
			players := series("load.server."+srv.ID+".players", window)
			row.Buckets = BucketizePair(load, players, window, bucketCount, now)
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Region != out[j].Region {
			return out[i].Region < out[j].Region
		}
		return out[i].ServerID < out[j].ServerID
	})
	return out
}
