// Collector error-arm pins: a store whose stats read fails must surface the
// failure at scrape time (an invalid metric, not a silent zero), and a queue
// provider reporting a disabled snapshot must emit no queue series at all.
package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// faultStatsStore fails the scrape-time stats read.
type faultStatsStore struct{ store.Store }

func (faultStatsStore) GetStats(context.Context) (*model.Stats, error) {
	return nil, errors.New("fault injected")
}

// disabledQueue reports the zero (Enabled=false) snapshot a store without an
// instruction queue produces.
type disabledQueue struct{}

func (disabledQueue) QueueStats() store.QueueStats { return store.QueueStats{} }

func TestStoreCollectorStatsErrorSurfaces(t *testing.T) {
	reg := prometheus.NewRegistry()
	if err := reg.Register(newStoreCollector(faultStatsStore{})); err != nil {
		t.Fatalf("register collector: %v", err)
	}
	_, err := reg.Gather()
	if err == nil || !strings.Contains(err.Error(), "get stats: fault injected") {
		t.Fatalf("gather with failing stats = %v, want the get stats wrap", err)
	}
}

func TestQueueCollectorDisabledSnapshotEmitsNothing(t *testing.T) {
	reg := prometheus.NewRegistry()
	if err := reg.Register(newQueueCollector(disabledQueue{})); err != nil {
		t.Fatalf("register collector: %v", err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if strings.HasPrefix(mf.GetName(), "atlas_store_queue_") {
			t.Fatalf("disabled snapshot emitted %s", mf.GetName())
		}
	}
}
