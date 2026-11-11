package routing

import (
	"context"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store/memory"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
	"github.com/cuihairu/atlas/internal/tracing/tracingtest"
	"go.opentelemetry.io/otel/trace"
)

// TestServiceSpansParentToRequest: recommend and diagnose both open a
// named child span under the transport span.
func TestServiceSpansParentToRequest(t *testing.T) {
	rec := tracingtest.Install(t)

	mem := memory.New()
	srv := &model.Server{
		ID:       "eu-1",
		Name:     "eu-1",
		Type:     "game",
		Region:   "eu",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
		Status:   model.StatusOnline,
	}
	if err := mem.RegisterServer(context.Background(), srv); err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := New(mem, mem, mem, mem)

	tracer := atlastracing.Tracer()
	pctx, parent := tracer.Start(context.Background(), "test-root")

	if _, _, err := svc.Recommend(pctx, Request{Region: "eu"}); err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if _, err := svc.Diagnose(pctx, Request{Region: "eu"}); err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	parent.End()

	want := map[string]bool{
		"routing.recommend": false,
		"routing.diagnose":  false,
	}
	for _, s := range rec.Ended() {
		if _, ok := want[s.Name()]; !ok {
			continue
		}
		want[s.Name()] = true
		if got := s.Parent().SpanID(); got != parent.SpanContext().SpanID() {
			t.Fatalf("%s parent = %v, want the root span", s.Name(), got)
		}
		if s.SpanKind() != trace.SpanKindInternal {
			t.Fatalf("%s kind = %v, want internal", s.Name(), s.SpanKind())
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing service span %q", name)
		}
	}
}
