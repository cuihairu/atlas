package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestSamplerLockstepSeries(t *testing.T) {
	s := NewSampler(10*time.Millisecond, 200*time.Millisecond)
	if s.Capacity() != 20 {
		t.Fatalf("capacity = %d, want 20 (retention/interval)", s.Capacity())
	}
	tick := 0
	now := time.Now()
	s.tickAt = func() time.Time { tick++; return now.Add(time.Duration(tick) * 10 * time.Millisecond) }

	s.Probe(func() map[string]float64 {
		return map[string]float64{"load.fleet.players": float64(tick) * 10, "load.fleet.load": 0.5}
	})

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	time.Sleep(55 * time.Millisecond)
	cancel()

	players := s.Series("load.fleet.players", 200*time.Millisecond)
	if len(players) < 3 {
		t.Fatalf("series too short: %d points", len(players))
	}
	if last := players[len(players)-1]; last.V <= players[0].V {
		t.Errorf("series not advancing: first=%v last=%v", players[0].V, last.V)
	}
	// Same-probe rings stay aligned (chart merge is a zip).
	load := s.Series("load.fleet.load", 200*time.Millisecond)
	if len(load) != len(players) {
		t.Fatalf("lockstep violated: players=%d load=%d", len(players), len(load))
	}
	for i := range players {
		if !players[i].T.Equal(load[i].T) {
			t.Fatalf("timestamps diverge at %d", i)
		}
	}
}

func TestRateFromCumulative(t *testing.T) {
	base := time.Now()
	pts := []Point{
		{T: base, V: 100},
		{T: base.Add(10 * time.Second), V: 120},
		{T: base.Add(20 * time.Second), V: 140},
	}
	if r := Rate(pts); r < 1.9 || r > 2.1 {
		t.Errorf("rate = %v, want ~2/s", r)
	}
	// Counter reset (process restart): rate counts from zero to last.
	reset := []Point{{T: base, V: 500}, {T: base.Add(10 * time.Second), V: 30}}
	if r := Rate(reset); r < 2.9 || r > 3.1 {
		t.Errorf("reset rate = %v, want ~3/s", r)
	}
	if r := Rate(pts[:1]); r != 0 {
		t.Errorf("single point rate = %v, want 0", r)
	}
}
