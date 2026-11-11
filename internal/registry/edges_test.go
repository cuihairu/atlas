// Error-path coverage for the registry service: every store verb's failure
// wrap is driven with per-method fault hooks, and the two best-effort paths
// (initial heartbeat on register, runtime cleanup on unregister) are pinned
// to succeed-with-a-warn rather than fail the operation.
package registry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

var errFault = errors.New("fault injected")

// faultServers wraps a memory store and lets individual verbs fail on
// demand; unset hooks delegate to the backing store.
type faultServers struct {
	store.ServerStore
	getServerFn      func(ctx context.Context, id string) (*model.Server, error)
	registerServerFn func(ctx context.Context, s *model.Server) error
	updateStatusFn   func(ctx context.Context, id string, status model.ServerStatus) error
}

func (f *faultServers) GetServer(ctx context.Context, id string) (*model.Server, error) {
	if f.getServerFn != nil {
		return f.getServerFn(ctx, id)
	}
	return f.ServerStore.GetServer(ctx, id)
}

func (f *faultServers) RegisterServer(ctx context.Context, s *model.Server) error {
	if f.registerServerFn != nil {
		return f.registerServerFn(ctx, s)
	}
	return f.ServerStore.RegisterServer(ctx, s)
}

func (f *faultServers) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	if f.updateStatusFn != nil {
		return f.updateStatusFn(ctx, id, status)
	}
	return f.ServerStore.UpdateServerStatus(ctx, id, status)
}

// faultRuntime wraps a memory store the same way for the runtime verbs.
type faultRuntime struct {
	store.RuntimeStore
	recordFn func(ctx context.Context, id string, hb model.Heartbeat) error
	deleteFn func(ctx context.Context, id string) error
}

func (f *faultRuntime) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	if f.recordFn != nil {
		return f.recordFn(ctx, id, hb)
	}
	return f.RuntimeStore.RecordHeartbeat(ctx, id, hb)
}

func (f *faultRuntime) DeleteRuntime(ctx context.Context, id string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return f.RuntimeStore.DeleteRuntime(ctx, id)
}

func newBufLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func hasLog(buf *bytes.Buffer, msg string) bool {
	return strings.Contains(buf.String(), msg)
}

func seedServer(t *testing.T, mem *memory.Store, id string, status model.ServerStatus, source string) {
	t.Helper()
	if err := mem.RegisterServer(context.Background(), &model.Server{
		ID:       id,
		Name:     id,
		Region:   "cn-east",
		Endpoint: model.Endpoint{Host: "10.0.0.9", Port: 30009},
		Status:   status,
		Source:   source,
	}); err != nil {
		t.Fatalf("seed server %s: %v", id, err)
	}
}

// TestCheckRegistrationStoreError pins the gate's error propagation: a store
// failure (not a missing server) is surfaced — an unknown verdict must never
// be read as "allowed".
func TestCheckRegistrationStoreError(t *testing.T) {
	mem := memory.New()
	fs := &faultServers{ServerStore: mem, getServerFn: func(context.Context, string) (*model.Server, error) {
		return nil, errFault
	}}
	svc := New(fs, mem, newBufLogger(&bytes.Buffer{}))

	if _, err := svc.CheckRegistration(context.Background(), "any-srv"); err == nil {
		t.Fatal("check registration with a failing store: expected error, got nil")
	}
}

// TestRegisterLookupFailure pins the register pre-check wrap: a lookup error
// other than not-found aborts the registration instead of risking an update
// over an unreadable record.
func TestRegisterLookupFailure(t *testing.T) {
	mem := memory.New()
	fs := &faultServers{ServerStore: mem, getServerFn: func(context.Context, string) (*model.Server, error) {
		return nil, errFault
	}}
	svc := New(fs, mem, newBufLogger(&bytes.Buffer{}))

	if _, err := svc.Register(context.Background(), testRequest()); err == nil {
		t.Fatal("register with a failing lookup: expected error, got nil")
	}
}

// TestRegisterStoreFailure pins the register write wrap.
func TestRegisterStoreFailure(t *testing.T) {
	mem := memory.New()
	fs := &faultServers{ServerStore: mem, registerServerFn: func(context.Context, *model.Server) error {
		return errFault
	}}
	svc := New(fs, mem, newBufLogger(&bytes.Buffer{}))

	if _, err := svc.Register(context.Background(), testRequest()); err == nil {
		t.Fatal("register with a failing store write: expected error, got nil")
	}
}

// TestRegisterInitialHeartbeatFailure pins the best-effort contract: the
// initial heartbeat is a convenience seed — its failure is a warn, and the
// registration itself still lands (the next heartbeat refills runtime data).
func TestRegisterInitialHeartbeatFailure(t *testing.T) {
	mem := memory.New()
	fr := &faultRuntime{RuntimeStore: mem, recordFn: func(context.Context, string, model.Heartbeat) error {
		return errFault
	}}
	var buf bytes.Buffer
	svc := New(mem, fr, newBufLogger(&buf))

	srv, err := svc.Register(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("register must survive an initial-heartbeat failure: %v", err)
	}
	if srv.ID != "game-1001" {
		t.Fatalf("registered server ID = %q, want game-1001", srv.ID)
	}
	if !hasLog(&buf, "failed to record initial heartbeat") {
		t.Fatalf("initial-heartbeat failure was not warned: %q", buf.String())
	}
}

// TestHeartbeatRecordFailure pins the runtime write wrap: a heartbeat that
// cannot be recorded is an error (liveness data must not be silently lost).
func TestHeartbeatRecordFailure(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	seedServer(t, mem, "hb-srv", model.StatusOnline, "")
	fr := &faultRuntime{RuntimeStore: mem, recordFn: func(context.Context, string, model.Heartbeat) error {
		return errFault
	}}
	svc := New(mem, fr, newBufLogger(&bytes.Buffer{}))

	if _, err := svc.Heartbeat(ctx, "hb-srv", model.Heartbeat{Players: 1, Load: 0.1}); err == nil {
		t.Fatal("heartbeat with a failing runtime write: expected error, got nil")
	}
}

// TestHeartbeatPromoteFailure pins the starting→online promote wrap: a
// failed status write surfaces as an error even though the heartbeat itself
// was recorded.
func TestHeartbeatPromoteFailure(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	seedServer(t, mem, "promo-srv", model.StatusStarting, "")
	fs := &faultServers{ServerStore: mem, updateStatusFn: func(context.Context, string, model.ServerStatus) error {
		return errFault
	}}
	svc := New(fs, mem, newBufLogger(&bytes.Buffer{}))

	if _, err := svc.Heartbeat(ctx, "promo-srv", model.Heartbeat{Players: 1, Load: 0.1}); err == nil {
		t.Fatal("heartbeat with a failing promote write: expected error, got nil")
	}
}

// TestHeartbeatConfigReentryPromoteFailure pins the config-managed re-entry
// promote wrap: a declared server coming back from suspect/offline whose
// status write fails surfaces as an error.
func TestHeartbeatConfigReentryPromoteFailure(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	seedServer(t, mem, "cfg-srv", model.StatusSuspect, "config")
	fs := &faultServers{ServerStore: mem, updateStatusFn: func(context.Context, string, model.ServerStatus) error {
		return errFault
	}}
	svc := New(fs, mem, newBufLogger(&bytes.Buffer{}))

	if _, err := svc.Heartbeat(ctx, "cfg-srv", model.Heartbeat{Players: 1, Load: 0.1}); err == nil {
		t.Fatal("config re-entry with a failing promote write: expected error, got nil")
	}
}

// TestUnregisterStatusFailure pins the unregister status wrap.
func TestUnregisterStatusFailure(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	seedServer(t, mem, "un-srv", model.StatusOnline, "")
	fs := &faultServers{ServerStore: mem, updateStatusFn: func(context.Context, string, model.ServerStatus) error {
		return errFault
	}}
	svc := New(fs, mem, newBufLogger(&bytes.Buffer{}))

	if err := svc.Unregister(ctx, "un-srv"); err == nil {
		t.Fatal("unregister with a failing status write: expected error, got nil")
	}
}

// TestUnregisterRuntimeCleanupFailure pins the best-effort cleanup: a runtime
// delete failure is a warn — the server is already offline, so unregister
// still reports success.
func TestUnregisterRuntimeCleanupFailure(t *testing.T) {
	mem := memory.New()
	ctx := context.Background()
	seedServer(t, mem, "un-rt-srv", model.StatusOnline, "")
	fr := &faultRuntime{RuntimeStore: mem, deleteFn: func(context.Context, string) error {
		return errFault
	}}
	var buf bytes.Buffer
	svc := New(mem, fr, newBufLogger(&buf))

	if err := svc.Unregister(ctx, "un-rt-srv"); err != nil {
		t.Fatalf("unregister must survive a runtime-cleanup failure: %v", err)
	}
	if !hasLog(&buf, "failed to delete runtime during unregister") {
		t.Fatalf("runtime-cleanup failure was not warned: %q", buf.String())
	}
}
