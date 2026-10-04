package fleet

import (
	"context"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// TrackServers wraps a ServerStore so every successful write feeds the
// index in the same call: register upserts, lifecycle status changes, tag
// writes, deletes. Reads delegate unchanged. Wiring this once in main (all
// services share the decorated store) means no service code changes to
// keep the aggregate current.
func TrackServers(inner store.ServerStore, idx *Index) store.ServerStore {
	return &trackedServers{ServerStore: inner, idx: idx}
}

// TrackStore wraps a full store.Store: server writes and heartbeats feed the
// index (via the two sub-decorators), everything else delegates unchanged.
// This is the single wiring point for composite stores — main passes the
// wrapped store to every service, so the aggregate stays current with no
// service-code changes.
func TrackStore(inner store.Store, idx *Index) store.Store {
	return &trackedStore{
		Store: inner,
		sv:    &trackedServers{ServerStore: inner, idx: idx},
		rt:    &trackedRuntime{RuntimeStore: inner, idx: idx},
	}
}

// trackedStore decorates the full store interface. The sub-decorators are
// named (not embedded) fields: embedding both store.Store and *trackedServers
// would promote colliding methods at the same depth, dropping them from the
// method set and breaking store.Store satisfaction.
type trackedStore struct {
	store.Store
	sv *trackedServers
	rt *trackedRuntime
}

func (t *trackedStore) RegisterServer(ctx context.Context, srv *model.Server) error {
	return t.sv.RegisterServer(ctx, srv)
}

func (t *trackedStore) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	return t.sv.UpdateServerStatus(ctx, id, status)
}

func (t *trackedStore) UpdateServerTags(ctx context.Context, id string, tags []model.ServerTag) error {
	return t.sv.UpdateServerTags(ctx, id, tags)
}

func (t *trackedStore) DeleteServer(ctx context.Context, id string) error {
	return t.sv.DeleteServer(ctx, id)
}

func (t *trackedStore) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	return t.rt.RecordHeartbeat(ctx, id, hb)
}

func (t *trackedStore) DeleteRuntime(ctx context.Context, id string) error {
	return t.rt.DeleteRuntime(ctx, id)
}

type trackedServers struct {
	store.ServerStore
	idx *Index
}

func (t *trackedServers) RegisterServer(ctx context.Context, srv *model.Server) error {
	if err := t.ServerStore.RegisterServer(ctx, srv); err != nil {
		return err
	}
	// Re-read: the store owns register-upsert semantics (surviving status,
	// timestamps). The SQL upsert keeps an active status without writing it
	// back to the caller's object, so the index must mirror the stored
	// record, not the request.
	if cur, err := t.ServerStore.GetServer(ctx, srv.ID); err == nil {
		t.idx.Upsert(cur)
	}
	return nil
}

func (t *trackedServers) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	if err := t.ServerStore.UpdateServerStatus(ctx, id, status); err != nil {
		return err
	}
	t.idx.UpdateStatus(id, status)
	return nil
}

func (t *trackedServers) UpdateServerTags(ctx context.Context, id string, tags []model.ServerTag) error {
	if err := t.ServerStore.UpdateServerTags(ctx, id, tags); err != nil {
		return err
	}
	// Re-read the record: the store owns how tags merge with defaults.
	if srv, err := t.ServerStore.GetServer(ctx, id); err == nil {
		t.idx.Upsert(srv)
	}
	return nil
}

func (t *trackedServers) DeleteServer(ctx context.Context, id string) error {
	if err := t.ServerStore.DeleteServer(ctx, id); err != nil {
		return err
	}
	t.idx.Remove(id)
	return nil
}

// TrackRuntime wraps a RuntimeStore so heartbeats feed the index's runtime
// fields (players / load / last-seen). Runtime deletion (unregister /
// disable) zeroes them in the index the same way the runtime view empties.
func TrackRuntime(inner store.RuntimeStore, idx *Index) store.RuntimeStore {
	return &trackedRuntime{RuntimeStore: inner, idx: idx}
}

type trackedRuntime struct {
	store.RuntimeStore
	idx *Index
}

func (t *trackedRuntime) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	if err := t.RuntimeStore.RecordHeartbeat(ctx, id, hb); err != nil {
		return err
	}
	t.idx.UpdateRuntime(id, hb.Players, hb.Load, time.Now())
	return nil
}

func (t *trackedRuntime) DeleteRuntime(ctx context.Context, id string) error {
	if err := t.RuntimeStore.DeleteRuntime(ctx, id); err != nil {
		return err
	}
	t.idx.UpdateRuntime(id, 0, 0, time.Time{})
	return nil
}
