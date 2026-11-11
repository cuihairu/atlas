package registry

import (
	"context"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
	"github.com/cuihairu/atlas/internal/tracing/tracingtest"
	"go.opentelemetry.io/otel/trace"
)

// TestServiceSpansParentToRequest pins the tree contract: spans opened via
// atlastracing.Start land as children of whatever the transport put in the
// context (the root span in production, a synthetic parent here) — no
// wiring beyond the context, and the internal kind separates them from
// transport roots in the trace view.
func TestServiceSpansParentToRequest(t *testing.T) {
	rec := tracingtest.Install(t)
	svc := newTestService()

	tracer := atlastracing.Tracer()
	pctx, parent := tracer.Start(context.Background(), "test-root")

	req := testRequest()
	if _, err := svc.Register(pctx, req); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.Heartbeat(pctx, req.ID, model.Heartbeat{Players: 10, Load: 0.5}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if err := svc.Unregister(pctx, req.ID); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	parent.End()

	want := map[string]bool{
		"registry.register":   false,
		"registry.heartbeat":  false,
		"registry.unregister": false,
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
			t.Fatalf("missing service span %q (recorded: %v)", name, rec.Ended())
		}
	}
}
