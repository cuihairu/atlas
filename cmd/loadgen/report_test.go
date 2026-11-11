package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPercentileNearestRank(t *testing.T) {
	s := newStream("sample", 1000)
	for i := 1; i <= 100; i++ {
		s.Record(time.Duration(i) * time.Millisecond)
	}
	want := map[int]time.Duration{
		0:   time.Millisecond,       // min
		50:  50 * time.Millisecond,  // nearest-rank p50 on 1..100
		95:  95 * time.Millisecond,  // nearest-rank p95
		99:  99 * time.Millisecond,  // nearest-rank p99
		100: 100 * time.Millisecond, // max
	}
	for p, w := range want {
		if got := s.Pct(p); got != w {
			t.Errorf("Pct(%d) = %v, want %v", p, got, w)
		}
	}
}

func TestPercentileEmpty(t *testing.T) {
	s := newStream("empty", 100)
	if got := s.Pct(99); got != 0 {
		t.Errorf("Pct on empty stream = %v, want 0", got)
	}
}

func TestReservoirCap(t *testing.T) {
	const cap = 50
	s := newStream("reservoir", cap)
	for i := 0; i < 10000; i++ {
		s.Record(time.Duration(i) * time.Nanosecond)
	}
	if got := len(s.samples); got != cap {
		t.Errorf("reservoir kept %d samples, want %d", got, cap)
	}
	if got := s.Count(); got != 10000 {
		t.Errorf("count = %d, want 10000", got)
	}
}

func TestStreamRPS(t *testing.T) {
	s := newStream("rps", 100)
	for i := 0; i < 10; i++ {
		s.Record(time.Millisecond)
	}
	if got := s.RPS(time.Second); got != 10 {
		t.Errorf("RPS over 1s with 10 calls = %v, want 10", got)
	}
}

func TestStreamErrorsSkipped(t *testing.T) {
	s := newStream("mix", 100)
	s.Record(time.Millisecond)
	s.Err(errors.New("boom"))
	s.Skip()
	if s.Errors() != 1 || s.Skipped() != 1 {
		t.Errorf("errors=%d skipped=%d, want 1/1", s.Errors(), s.Skipped())
	}
}

func TestReportRenderText(t *testing.T) {
	e := &Engine{flows: map[string]*stream{
		flowRegister:  newStream(flowRegister, 100),
		flowHeartbeat: newStream(flowHeartbeat, 100),
	}}
	e.flows[flowHeartbeat].Record(5 * time.Millisecond)
	r := e.report(time.Second)
	text := r.RenderText()
	for _, name := range []string{flowRegister, flowHeartbeat, "p95", "rps"} {
		if !strings.Contains(text, name) {
			t.Errorf("report text missing %q:\n%s", name, text)
		}
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	e := &Engine{flows: map[string]*stream{flowRegister: newStream(flowRegister, 100)}}
	e.flows[flowRegister].Record(3 * time.Millisecond)
	e.flows[flowRegister].Err(errors.New("boom"))
	r := e.report(2 * time.Second)

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back report
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Streams) != 1 || back.Streams[0].Name != flowRegister ||
		back.Streams[0].Count != 1 || back.Streams[0].Errors != 1 {
		t.Errorf("round-trip mismatch: %+v", back.Streams)
	}
}
