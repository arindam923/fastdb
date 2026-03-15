package main

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all the Prometheus metrics
type Metrics struct {
	// Server metrics
	connectionsActive prometheus.Gauge
	connectionsTotal  prometheus.Counter
	connectionsErrors prometheus.Counter

	// Operations metrics
	operationsTotal    *prometheus.CounterVec
	operationsErrors   *prometheus.CounterVec
	operationsDuration *prometheus.HistogramVec

	// Storage metrics
	keysTotal         prometheus.Gauge
	keysVersion       prometheus.Gauge
	storageShardUsage *prometheus.GaugeVec

	// WAL metrics
	walEntriesTotal prometheus.Counter
	walEntriesSize  prometheus.Gauge
	walFlushesTotal prometheus.Counter

	// Persistence metrics
	persistenceOperationsTotal    prometheus.Counter
	persistenceOperationsErrors   prometheus.Counter
	persistenceOperationsDuration prometheus.Histogram

	// Snapshot metrics
	snapshotOperationsTotal    prometheus.Counter
	snapshotOperationsErrors   prometheus.Counter
	snapshotOperationsDuration prometheus.Histogram
	snapshotSize               prometheus.Gauge

	// Memory metrics
	memoryAlloc prometheus.Gauge
	memorySys   prometheus.Gauge
}

var metrics *Metrics

func initMetrics() {
	metrics = &Metrics{
		// Server metrics
		connectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flashdb_connections_active",
			Help: "Current number of active WebSocket connections",
		}),
		connectionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_connections_total",
			Help: "Total number of WebSocket connections established",
		}),
		connectionsErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_connections_errors",
			Help: "Number of connection errors",
		}),

		// Operations metrics
		operationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "flashdb_operations_total",
			Help: "Total number of operations by type",
		}, []string{"type"}),
		operationsErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "flashdb_operations_errors",
			Help: "Number of operation errors by type",
		}, []string{"type"}),
		operationsDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "flashdb_operations_duration_seconds",
			Help:    "Duration of operations in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		}, []string{"type"}),

		// Storage metrics
		keysTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flashdb_keys_total",
			Help: "Total number of keys in the database",
		}),
		keysVersion: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flashdb_keys_version",
			Help: "Current version number of keys",
		}),
		storageShardUsage: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "flashdb_storage_shard_usage",
			Help: "Number of keys per shard",
		}, []string{"shard"}),

		// WAL metrics
		walEntriesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_wal_entries_total",
			Help: "Total number of WAL entries",
		}),
		walEntriesSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flashdb_wal_entries_size",
			Help: "Current WAL size in bytes",
		}),
		walFlushesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_wal_flushes_total",
			Help: "Number of WAL flushes",
		}),

		// Persistence metrics
		persistenceOperationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_persistence_operations_total",
			Help: "Number of persistence operations",
		}),
		persistenceOperationsErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_persistence_operations_errors",
			Help: "Number of persistence errors",
		}),
		persistenceOperationsDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "flashdb_persistence_duration_seconds",
			Help:    "Duration of persistence operations in seconds",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms to ~10s
		}),

		// Snapshot metrics
		snapshotOperationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_snapshot_operations_total",
			Help: "Number of snapshot operations",
		}),
		snapshotOperationsErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "flashdb_snapshot_operations_errors",
			Help: "Number of snapshot errors",
		}),
		snapshotOperationsDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "flashdb_snapshot_duration_seconds",
			Help:    "Duration of snapshot operations in seconds",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 100ms to ~100s
		}),
		snapshotSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flashdb_snapshot_size",
			Help: "Size of last snapshot in bytes",
		}),

		// Memory metrics
		memoryAlloc: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flashdb_memory_alloc_bytes",
			Help: "Bytes allocated and not yet freed",
		}),
		memorySys: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "flashdb_memory_sys_bytes",
			Help: "Total bytes obtained from system",
		}),
	}

	// Register all metrics
	prometheus.MustRegister(
		metrics.connectionsActive,
		metrics.connectionsTotal,
		metrics.connectionsErrors,
		metrics.operationsTotal,
		metrics.operationsErrors,
		metrics.operationsDuration,
		metrics.keysTotal,
		metrics.keysVersion,
		metrics.storageShardUsage,
		metrics.walEntriesTotal,
		metrics.walEntriesSize,
		metrics.walFlushesTotal,
		metrics.persistenceOperationsTotal,
		metrics.persistenceOperationsErrors,
		metrics.persistenceOperationsDuration,
		metrics.snapshotOperationsTotal,
		metrics.snapshotOperationsErrors,
		metrics.snapshotOperationsDuration,
		metrics.snapshotSize,
		metrics.memoryAlloc,
		metrics.memorySys,
	)
}

// MetricsHandler returns the Prometheus handler
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// Helper to record operation duration
func RecordOperationDuration(opType string, start time.Time) {
	duration := time.Since(start).Seconds()
	metrics.operationsDuration.WithLabelValues(opType).Observe(duration)
}

// Helper to record operation with duration
func RecordOperation(opType string, err error) func() {
	start := time.Now()

	metrics.operationsTotal.WithLabelValues(opType).Inc()

	if err != nil {
		metrics.operationsErrors.WithLabelValues(opType).Inc()
	}

	return func() {
		RecordOperationDuration(opType, start)
	}
}

// Helper to update storage metrics
func UpdateStorageMetrics(store *Store) {
	keys := store.Keys()
	metrics.keysTotal.Set(float64(len(keys)))

	// Count keys per shard
	shardCounts := make(map[int]int)
	for _, key := range keys {
		shard := store.shardIndex(key)
		shardCounts[shard]++
	}

	for shard, count := range shardCounts {
		metrics.storageShardUsage.WithLabelValues(itoa(int64(shard))).Set(float64(count))
	}
}
