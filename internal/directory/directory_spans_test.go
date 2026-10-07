package directory

import (
	"context"
	"testing"

	"github.com/cuihairu/atlas/internal/store"
	atlastracing "github.com/cuihairu/atlas/internal/tracing"
	"github.com/cuihairu/atlas/internal/tracing/tracingtest"
	"go.opentelemetry.io/otel/trace"
)

// TestServiceSpansParentToRequest: write and read paths of the character
// index each open a named child span under the transport span.
func TestServiceSpansParentToRequest(t *testing.T) {
	rec := tracingtest.Install(t)
	svc, _ := newTestService()

	tracer := atlastracing.Tracer()
	pctx, parent := tracer.Start(context.Background(), "test-root")

	if _, err := svc.CreateCharacter(pctx, 10001, "game-1001", 823712, "TestChar", 50, 3); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	if _, err := svc.UpdateCharacter(pctx, 10001, "game-1001", 823712, store.CharacterPatch{Level: intPtr(51)}); err != nil {
		t.Fatalf("UpdateCharacter: %v", err)
	}
	if _, err := svc.GetCharacter(pctx, 10001, "game-1001", 823712); err != nil {
		t.Fatalf("GetCharacter: %v", err)
	}
	if err := svc.DeleteCharacter(pctx, 10001, "game-1001", 823712); err != nil {
		t.Fatalf("DeleteCharacter: %v", err)
	}
	parent.End()

	want := map[string]bool{
		"directory.create": false,
		"directory.update": false,
		"directory.get":    false,
		"directory.delete": false,
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
