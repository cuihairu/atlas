package atlas

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	atlasgrpc "github.com/cuihairu/atlas/internal/grpc"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestDefaultHeadersREST: Options.DefaultHeaders ride on every REST
// request — the correlation-header channel docs/api.md §请求追踪 promises.
func TestDefaultHeadersREST(t *testing.T) {
	var seen atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/discovery/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("X-Request-ID"))
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"code": "SERVER_NOT_FOUND", "message": "x"}) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New(Options{
		Addr:           srv.URL,
		DefaultHeaders: map[string]string{"X-Request-ID": "sdk-rest-1"},
		BaseBackoff:    time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.GetServer(ctx, "nope"); err == nil {
		t.Fatalf("GetServer on empty mock: want error")
	}
	if got, _ := seen.Load().(string); got != "sdk-rest-1" {
		t.Errorf("server saw X-Request-ID %q, want sdk-rest-1", got)
	}
}

// TestDefaultHeadersGRPC: the same option lands as gRPC metadata on the
// wire — captured by a server-side interceptor reading the exact key the
// server's trace interceptor adopts (docs/api.md §请求追踪).
func TestDefaultHeadersGRPC(t *testing.T) {
	var seen atomic.Value
	capture := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if v := md.Get("x-request-id"); len(v) > 0 {
				seen.Store(v[0])
			}
		}
		return handler(ctx, req)
	}
	mem := memory.New()
	adapter := httpadapter.New()
	dirSvc := directory.New(mem)
	if err := adapter.Subscribe(context.Background(), event.TopicCharacters, func(ctx context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(ctx, e)
		return err
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := atlasgrpc.New(
		registry.New(mem, mem, logger),
		discovery.New(mem, mem),
		dirSvc,
		routing.New(mem, mem, mem, mem),
		admin.New(mem),
		adapter,
	)
	g := grpc.NewServer(grpc.ChainUnaryInterceptor(capture))
	svc.RegisterServices(g)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go g.Serve(lis) //nolint:errcheck // test server
	t.Cleanup(g.Stop)

	c, err := New(Options{
		Addr:           lis.Addr().String(),
		Transport:      TransportGRPC,
		DefaultHeaders: map[string]string{"X-Request-ID": "sdk-grpc-1"},
		BaseBackoff:    time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New grpc: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.GetServer(ctx, "nope")
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Code != "NotFound" {
		t.Fatalf("GetServer: want *Error NotFound, got %#v", err)
	}
	if got, _ := seen.Load().(string); got != "sdk-grpc-1" {
		t.Errorf("server metadata x-request-id = %q, want sdk-grpc-1", got)
	}
}
