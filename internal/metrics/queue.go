package metrics

// 指令写队列观测 (TODO v0.2 ④ dash 队列可观测).
//
// queueCollector is the same posture as storeCollector: it reads the store's
// queue snapshot on every scrape — zero write-path overhead, no second source
// of truth. The snapshot comes from store.QueueStatusProvider, which only the
// memory store implements (the SQL stores have no instruction queue), so New
// registers this collector only when the store supports it. Metric semantics
// are the QueueStats field comments (internal/store/store.go) — this file
// adds no semantics of its own.
//
// Rates (enqueue / merge rate) are meant to be computed in PromQL from the
// *_total counters, not tracked here.

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cuihairu/atlas/internal/store"
)

// queueCollector exposes the instruction write-path snapshot as scrape-time
// metrics under the atlas_store_queue_* family.
type queueCollector struct {
	provider store.QueueStatusProvider

	depthControl   *prometheus.Desc
	depthHot       *prometheus.Desc
	enqueuedTotal  *prometheus.Desc
	mergedTotal    *prometheus.Desc
	appliedTotal   *prometheus.Desc
	idempotentHits *prometheus.Desc
	backpressure   *prometheus.Desc
	watermark      *prometheus.Desc
	flushDuration  *prometheus.Desc
	flushBatchSize *prometheus.Desc
	appliedByKind  *prometheus.Desc
	captureEntries *prometheus.Desc
}

func newQueueCollector(p store.QueueStatusProvider) prometheus.Collector {
	return &queueCollector{
		provider: p,
		depthControl: prometheus.NewDesc(
			"atlas_store_queue_depth_control",
			"Pending instructions in control lanes (config changes: register, status, tags, delete, character upsert/update/delete).",
			nil, nil,
		),
		depthHot: prometheus.NewDesc(
			"atlas_store_queue_depth_hot",
			"Pending instructions in hot lanes (heartbeats).",
			nil, nil,
		),
		enqueuedTotal: prometheus.NewDesc(
			"atlas_store_queue_enqueued_total",
			"Total instructions enqueued since process start.",
			nil, nil,
		),
		mergedTotal: prometheus.NewDesc(
			"atlas_store_queue_merged_total",
			"Total instructions merged (coalesced) by same-entity same-kind rules.",
			nil, nil,
		),
		appliedTotal: prometheus.NewDesc(
			"atlas_store_queue_applied_total",
			"Total instructions applied (committed) since process start.",
			nil, nil,
		),
		idempotentHits: prometheus.NewDesc(
			"atlas_store_queue_idempotent_hits_total",
			"Total idempotency-key deduplication hits.",
			nil, nil,
		),
		backpressure: prometheus.NewDesc(
			"atlas_store_queue_backpressure_total",
			"Total times the queue was full and the caller fell back to inline synchronous apply.",
			nil, nil,
		),
		watermark: prometheus.NewDesc(
			"atlas_store_queue_watermark",
			"Monotonic sequence number of the last applied instruction (visibility watermark).",
			nil, nil,
		),
		flushDuration: prometheus.NewDesc(
			"atlas_store_queue_flush_duration_seconds",
			"Duration of the last flush critical section (data + index mutations).",
			nil, nil,
		),
		flushBatchSize: prometheus.NewDesc(
			"atlas_store_queue_flush_batch_size",
			"Number of instructions committed in the last flush batch.",
			nil, nil,
		),
		appliedByKind: prometheus.NewDesc(
			"atlas_store_queue_applied_by_kind_total",
			"Applied instructions by kind (register_server, heartbeat, ...) — the coarse shape of the write workload.",
			[]string{"kind"}, nil,
		),
		captureEntries: prometheus.NewDesc(
			"atlas_store_queue_capture_entries",
			"Instructions in the replay capture ring (排障回放用, bounded).",
			nil, nil,
		),
	}
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.depthControl
	ch <- c.depthHot
	ch <- c.enqueuedTotal
	ch <- c.mergedTotal
	ch <- c.appliedTotal
	ch <- c.idempotentHits
	ch <- c.backpressure
	ch <- c.watermark
	ch <- c.flushDuration
	ch <- c.flushBatchSize
	ch <- c.appliedByKind
	ch <- c.captureEntries
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.provider.QueueStats()
	// Composites forward snapshots for every backend: an Enabled=false
	// snapshot (SQL store) emits no series at all — queue metrics imply a
	// queue exists.
	if !stats.Enabled {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.depthControl, prometheus.GaugeValue, float64(stats.DepthControl))
	ch <- prometheus.MustNewConstMetric(c.depthHot, prometheus.GaugeValue, float64(stats.DepthHot))
	ch <- prometheus.MustNewConstMetric(c.enqueuedTotal, prometheus.CounterValue, float64(stats.Enqueued))
	ch <- prometheus.MustNewConstMetric(c.mergedTotal, prometheus.CounterValue, float64(stats.Merged))
	ch <- prometheus.MustNewConstMetric(c.appliedTotal, prometheus.CounterValue, float64(stats.Applied))
	ch <- prometheus.MustNewConstMetric(c.idempotentHits, prometheus.CounterValue, float64(stats.IdempotentHits))
	ch <- prometheus.MustNewConstMetric(c.backpressure, prometheus.CounterValue, float64(stats.BackpressureSync))
	ch <- prometheus.MustNewConstMetric(c.watermark, prometheus.GaugeValue, float64(stats.Watermark))
	if !stats.LastFlush.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.flushDuration, prometheus.GaugeValue, stats.LastFlushDuration.Seconds())
		ch <- prometheus.MustNewConstMetric(c.flushBatchSize, prometheus.GaugeValue, float64(stats.LastFlushBatch))
	}
	for kind, n := range stats.AppliedByKind {
		ch <- prometheus.MustNewConstMetric(c.appliedByKind, prometheus.CounterValue, float64(n), kind)
	}
	ch <- prometheus.MustNewConstMetric(c.captureEntries, prometheus.GaugeValue, float64(stats.CaptureLen))
}
