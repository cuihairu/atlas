package telemetry

import (
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
)

func TestParseHeatmapWindow(t *testing.T) {
	if w, b, ok := ParseHeatmapWindow(""); !ok || w != time.Hour || b != 12 {
		t.Errorf("default = %v/%d/%v, want 1h/12/true", w, b, ok)
	}
	if w, b, ok := ParseHeatmapWindow("24h"); !ok || w != 24*time.Hour || b != 24 {
		t.Errorf("24h = %v/%d/%v, want 24h/24/true", w, b, ok)
	}
	if _, _, ok := ParseHeatmapWindow("7d"); ok {
		t.Error("7d must not be a valid window")
	}
}

func TestBucketizePairAveragesPerBucket(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	// Window 1h, 12 buckets of 5m. Feed three load samples into bucket 11
	// (the last 5 minutes) at 0.2/0.4/0.6 → avg 0.4, and one player sample.
	window := time.Hour
	load := []Point{
		{T: now.Add(-time.Minute), V: 0.2},
		{T: now.Add(-2 * time.Minute), V: 0.4},
		{T: now.Add(-3 * time.Minute), V: 0.6},
		// Outside the window: must be ignored.
		{T: now.Add(-2 * time.Hour), V: 9.9},
		// After now (clock skew): ignored.
		{T: now.Add(time.Minute), V: 9.9},
	}
	players := []Point{{T: now.Add(-time.Minute), V: 42}}

	buckets := BucketizePair(load, players, window, 12, now)
	if len(buckets) != 12 {
		t.Fatalf("buckets = %d, want 12", len(buckets))
	}
	if buckets[0].Samples != 0 || buckets[0].Load != 0 {
		t.Errorf("first bucket should be empty: %+v", buckets[0])
	}
	last := buckets[11]
	if last.Samples != 3 {
		t.Fatalf("last bucket samples = %d, want 3", last.Samples)
	}
	if last.Load < 0.3999 || last.Load > 0.4001 {
		t.Errorf("last bucket load = %v, want 0.4", last.Load)
	}
	if last.Players != 42 {
		t.Errorf("last bucket players = %v, want 42", last.Players)
	}
	// Bucket timestamps are the bucket START, oldest first.
	if !buckets[0].T.Equal(now.Add(-time.Hour)) {
		t.Errorf("first bucket t = %v, want %v", buckets[0].T, now.Add(-time.Hour))
	}
	if !buckets[11].T.Equal(now.Add(-5 * time.Minute)) {
		t.Errorf("last bucket t = %v, want %v", buckets[11].T, now.Add(-5*time.Minute))
	}
}

func TestBucketizeBoundaryPointGoesToLastBucket(t *testing.T) {
	// A point exactly at now lands in the last bucket (half-open buckets
	// [start, start+span) would otherwise drop the freshest sample).
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	buckets := BucketizePair([]Point{{T: now, V: 0.5}}, nil, time.Hour, 12, now)
	if buckets[11].Samples != 1 || buckets[11].Load != 0.5 {
		t.Errorf("point at now: %+v", buckets[11])
	}
}

func TestBuildServerHeatmapJoinsRecordsAndSeries(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	servers := []*model.Server{
		{ID: "s2", Name: "East-2", Region: "cn-east", Type: "game", Status: model.StatusOnline, Players: 80, Capacity: 1000, Load: 0.8},
		{ID: "s1", Name: "East-1", Region: "cn-east", Type: "game", Status: model.StatusDraining, Players: 10, Capacity: 1000, Load: 0.1},
		{ID: "s3", Name: "West-1", Region: "cn-west", Status: model.StatusOffline, Load: 0},
		nil, // tolerated silently
	}
	// series returns data only for s2 — s1/s3 keep empty buckets.
	series := func(name string, window time.Duration) []Point {
		if name == "load.server.s2.load" {
			return []Point{{T: now.Add(-time.Minute), V: 0.7}}
		}
		return nil
	}

	rows := BuildServerHeatmap(servers, series, time.Hour, 12, now)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	// Sorted by region then id: cn-east/s1, cn-east/s2, cn-west/s3.
	if rows[0].ServerID != "s1" || rows[1].ServerID != "s2" || rows[2].ServerID != "s3" {
		t.Fatalf("row order = %s %s %s", rows[0].ServerID, rows[1].ServerID, rows[2].ServerID)
	}
	if rows[1].Buckets[11].Load != 0.7 || rows[1].Buckets[11].Samples != 1 {
		t.Errorf("s2 buckets: %+v", rows[1].Buckets[11])
	}
	if len(rows[0].Buckets) != 12 || rows[0].Buckets[11].Samples != 0 {
		t.Errorf("s1 should have empty buckets: %+v", rows[0].Buckets[11])
	}
	// Live record fields ride along for the tooltip and no-data state.
	if rows[1].Status != "online" || rows[1].Players != 80 || rows[1].Load != 0.8 {
		t.Errorf("s2 live record: %+v", rows[1])
	}
	if rows[2].Status != "offline" {
		t.Errorf("s3 status: %+v", rows[2].Status)
	}
}

func TestBuildServerHeatmapWithoutSampler(t *testing.T) {
	// Embedders without a sampler still get the grid shape: live records
	// with empty buckets, never a nil slice.
	now := time.Now()
	rows := BuildServerHeatmap([]*model.Server{{ID: "s1", Status: model.StatusOnline}}, nil, time.Hour, 12, now)
	if len(rows) != 1 || rows[0].Buckets == nil || len(rows[0].Buckets) != 0 {
		t.Errorf("nil sampler must yield empty (non-nil) buckets: %+v", rows)
	}
}
