package discovery

import (
	"context"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
	"github.com/cuihairu/atlas/internal/tracing/tracingtest"
	"go.opentelemetry.io/otel/trace"
)

// TestServiceSpansParentToRequest: the read path gets the same child-span
// treatment as the write path — list and get land under the transport
// span pulled from the context.
func TestServiceSpansParentToRequest(t *testing.T) {
	rec := tracingtest.Install(t)
	svc, mem := newTestService()

	srv := &model.Server{
		ID:       "game-1",
		Name:     "game-1",
		Type:     "game",
		Region:   "eu",
		Version:  "1.0.0",
		Platform: "android",
		Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 30001},
		Capacity: 2000,
	}
	if err := mem.RegisterServer(context.Background(), srv); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tracer := atlastracing.Tracer()
	pctx, parent := tracer.Start(context.Background(), "test-root")

	if _, err := svc.ListServers(pctx, store.ServerFilter{}); err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if _, err := svc.GetServer(pctx, "game-1"); err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	parent.End()

	want := map[string]bool{
		"discovery.list_servers": false,
		"discovery.get_server":   false,
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
