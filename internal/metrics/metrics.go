// Package metrics wires Atlas's Prometheus instrumentation (TODO v0.1.3).
//
// Two kinds of collectors live on one private registry:
//
//   - Scrape-time gauges (server counts by status, character total) are
//     computed from the store on every scrape — no write-path overhead.
//   - In-process counters/histogram (discovery requests, admin requests,
//     heartbeat lag, health transitions) are observed at their hot spots.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Metrics holds every exported collector and the registry they live on.
type Metrics struct {
	registry *prometheus.Registry

	// HeartbeatLag observes heartbeat age (seconds) at each health sweep.
	HeartbeatLag prometheus.Histogram
	// DiscoveryRequests counts discovery list calls by applied filter.
	DiscoveryRequests *prometheus.CounterVec
	// AdminRequests counts admin API calls by route pattern and status code.
	AdminRequests *prometheus.CounterVec
	// HealthTransitions counts lifecycle transitions by from/to status.
	HealthTransitions *prometheus.CounterVec
	// DirectoryWrites observes directory write-path latency by operation
	// (REST create/update/delete and event-bus projection application).
	DirectoryWrites *prometheus.HistogramVec
	// RegistryWrites observes registry write-path latency (register /
	// heartbeat / unregister) — the hottest write path in the fleet: a
	// slower heartbeat write shows up here long before players feel it
	// (stale runtime → phantom suspect/offline sweeps).
	RegistryWrites *prometheus.HistogramVec
}

// New builds the metric set. The store backs the scrape-time gauges.
func New(s store.Store) *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,
		HeartbeatLag: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "atlas_registry_heartbeat_lag_seconds",
			Help:    "Heartbeat age in seconds as observed by the health monitor sweep.",
			Buckets: []float64{1, 5, 10, 15, 30, 60, 120, 300, 600},
		}),
		DiscoveryRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "atlas_discovery_requests_total",
			Help: "Discovery list requests, labeled by the applied filter.",
		}, []string{"filter"}),
		AdminRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "atlas_admin_requests_total",
			Help: "Admin API requests, labeled by route pattern (REST) or gRPC full method, and status code.",
		}, []string{"endpoint", "status"}),
		HealthTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "atlas_health_transitions_total",
			Help: "Server lifecycle transitions, labeled by from/to status.",
		}, []string{"from", "to"}),
		DirectoryWrites: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "atlas_directory_write_duration_seconds",
			Help:    "Directory write-path latency by operation (service-API create/update/delete, or projection event name).",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
		}, []string{"op"}),
		RegistryWrites: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "atlas_registry_write_duration_seconds",
			Help:    "Registry write-path latency by operation (register/heartbeat/unregister); the heartbeat line is the fleet's hottest write.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
		}, []string{"op"}),
	}

	reg.MustRegister(
		m.HeartbeatLag,
		m.DiscoveryRequests,
		m.AdminRequests,
		m.HealthTransitions,
		m.DirectoryWrites,
		m.RegistryWrites,
		newStoreCollector(s),
	)
	// Queue collector: only for stores implementing QueueStatusProvider (memory store).
	if qsp, ok := s.(store.QueueStatusProvider); ok {
		reg.MustRegister(newQueueCollector(qsp))
	}
	return m
}

// Handler serves the registry in Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ObserveHeartbeatLag records a heartbeat age observation.
func (m *Metrics) ObserveHeartbeatLag(age time.Duration) {
	if m != nil {
		m.HeartbeatLag.Observe(age.Seconds())
	}
}

// CountDiscovery increments the discovery counter for a filter.
func (m *Metrics) CountDiscovery(f store.ServerFilter) {
	if m != nil {
		m.DiscoveryRequests.WithLabelValues(discoveryFilterKey(f)).Inc()
	}
}

// CountHealthTransition increments the transitions counter.
func (m *Metrics) CountHealthTransition(from, to model.ServerStatus) {
	if m != nil {
		m.HealthTransitions.WithLabelValues(string(from), string(to)).Inc()
	}
}

// ObserveDirectoryWrite records a directory write-path operation latency.
// op is "create"/"update"/"delete" for direct service-API writes, or the
// event-bus projection event name ("created"/"updated"/"deleted"/…) — the
// production write path (REST and gRPC both publish events).
func (m *Metrics) ObserveDirectoryWrite(op string, d time.Duration) {
	if m != nil {
		m.DirectoryWrites.WithLabelValues(op).Observe(d.Seconds())
	}
}

// ObserveRegistryWrite records a registry write-path operation latency.
// op is "register", "heartbeat" or "unregister".
func (m *Metrics) ObserveRegistryWrite(op string, d time.Duration) {
	if m != nil {
		m.RegistryWrites.WithLabelValues(op).Observe(d.Seconds())
	}
}

// discoveryFilterKey renders the applied filter as a stable label value.
func discoveryFilterKey(f store.ServerFilter) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("region", f.Region)
	add("realm", f.Realm)
	add("shard", f.Shard)
	add("version", f.Version)
	add("platform", f.Platform)
	add("status", string(f.Status))
	if len(parts) == 0 {
		return "none"
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// RequestCounter is middleware counting requests by route pattern and
// response status. Wrap the auth-finished handler so rejected calls count.
func RequestCounter(counter *prometheus.CounterVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			endpoint := r.Pattern
			if endpoint == "" {
				endpoint = "unmatched"
			}
			counter.WithLabelValues(endpoint, strconv.Itoa(rec.status)).Inc()
		})
	}
}

// statusRecorder captures the status code written by the inner handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// storeCollector computes the server/character gauges from the store at
// scrape time.
type storeCollector struct {
	s store.Store

	serversTotal    *prometheus.Desc
	charactersTotal *prometheus.Desc
}

func newStoreCollector(s store.Store) prometheus.Collector {
	return &storeCollector{
		s: s,
		serversTotal: prometheus.NewDesc(
			"atlas_registry_servers_total",
			"Registered servers by lifecycle status.",
			[]string{"status"}, nil,
		),
		charactersTotal: prometheus.NewDesc(
			"atlas_directory_characters_total",
			"Total character index entries.",
			nil, nil,
		),
	}
}

func (c *storeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.serversTotal
	ch <- c.charactersTotal
}

func (c *storeCollector) Collect(ch chan<- prometheus.Metric) {
	stats, err := c.s.GetStats(context.Background())
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.serversTotal, fmt.Errorf("get stats: %w", err))
		ch <- prometheus.NewInvalidMetric(c.charactersTotal, err)
		return
	}
	for status, n := range stats.ServersByStatus {
		ch <- prometheus.MustNewConstMetric(c.serversTotal, prometheus.GaugeValue, float64(n), status)
	}
	ch <- prometheus.MustNewConstMetric(c.charactersTotal, prometheus.GaugeValue, float64(stats.TotalCharacters))
}
