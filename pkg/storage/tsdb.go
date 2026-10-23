package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/tsdb"
	"github.com/prometheus/tsdb/wal"
	"go.uber.org/zap"
)

// TSDBConfig holds configuration for the embedded TSDB
type TSDBConfig struct {
	DataDir              string
	RetentionDuration    time.Duration
	BlockRanges          []int64 // Milliseconds
	WALFlushInterval     time.Duration
	MaxActiveSeries      int
	EnableWAL            bool
	CompactionConcurrency int
}

// DefaultTSDBConfig returns production-ready defaults
func DefaultTSDBConfig(dataDir string) *TSDBConfig {
	return &TSDBConfig{
		DataDir:           dataDir,
		RetentionDuration: 14 * 24 * time.Hour, // 14 days hot retention
		BlockRanges: []int64{
			2 * 60 * 60 * 1000,      // 2 hours
			24 * 60 * 60 * 1000,     // 24 hours
			7 * 24 * 60 * 60 * 1000, // 7 days
			30 * 24 * 60 * 60 * 1000, // 30 days
		},
		WALFlushInterval:      30 * time.Second,
		MaxActiveSeries:       500000,
		EnableWAL:             true,
		CompactionConcurrency: 1,
	}
}

// TSDB wraps the Prometheus TSDB with FinOps-specific functionality
type TSDB struct {
	db           *tsdb.DB
	logger       *zap.Logger
	config       *TSDBConfig
	ingester     *Ingester
	compactor    *tsdb.Compactor
	snapshotter  *Snapshotter
	metrics      *TSDBMetrics
}

// TSDBMetrics holds Prometheus metrics for TSDB observability
type TSDBMetrics struct {
	HeadSeries       prometheus.Gauge
	HeadSamples      prometheus.Counter
	HeadBytes        prometheus.Gauge
	CompactionDuration prometheus.Histogram
	WALCorruptions   prometheus.Counter
	BlockCount       prometheus.Gauge
	IngesterBatchSize prometheus.Histogram
	IngesterFlushDuration prometheus.Histogram
	RejectedSamples  prometheus.Counter
	ExemplarsDropped prometheus.Counter
	QueryDuration    prometheus.Histogram
	QuerySamples     prometheus.Counter
	QueryErrors      prometheus.Counter
}

// NewTSDBMetrics creates and registers TSDB metrics
func NewTSDBMetrics(reg prometheus.Registerer) *TSDBMetrics {
	m := &TSDBMetrics{
		HeadSeries: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tsdb_head_series_total",
			Help: "Total number of series in the head block",
		}),
		HeadSamples: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tsdb_head_samples_appended_total",
			Help: "Total number of samples appended to the head block",
		}),
		HeadBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tsdb_head_bytes",
			Help: "Current size of the head block in bytes",
		}),
		CompactionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "tsdb_compaction_duration_seconds",
			Help:    "Duration of compaction runs",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
		}),
		WALCorruptions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tsdb_wal_corruptions_total",
			Help: "Total number of WAL corruptions encountered",
		}),
		BlockCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tsdb_block_count",
			Help: "Number of persisted blocks",
		}),
		IngesterBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "finops_ingester_batch_size",
			Help:    "Size of batches flushed to TSDB",
			Buckets: prometheus.ExponentialBuckets(100, 2, 10),
		}),
		IngesterFlushDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "finops_ingester_flush_duration_seconds",
			Help:    "Duration of ingester flush operations",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		}),
		RejectedSamples: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_ingester_rejected_samples_total",
			Help: "Total number of samples rejected due to cardinality limit",
		}),
		ExemplarsDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_ingester_exemplars_dropped_total",
			Help: "Total number of exemplars dropped",
		}),
		QueryDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "finops_query_duration_seconds",
			Help:    "Duration of query execution",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
		}),
		QuerySamples: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_query_samples_total",
			Help: "Total number of samples returned by queries",
		}),
		QueryErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_query_errors_total",
			Help: "Total number of query errors",
		}),
	}

	if reg != nil {
		reg.MustRegister(
			m.HeadSeries, m.HeadSamples, m.HeadBytes,
			m.CompactionDuration, m.WALCorruptions, m.BlockCount,
			m.IngesterBatchSize, m.IngesterFlushDuration,
			m.RejectedSamples, m.ExemplarsDropped,
			m.QueryDuration, m.QuerySamples, m.QueryErrors,
		)
	}

	return m
}

// NewTSDB creates a new TSDB instance with the given configuration
func NewTSDB(ctx context.Context, logger *zap.Logger, config *TSDBConfig, reg prometheus.Registerer) (*TSDB, error) {
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Create TSDB options
	opts := &tsdb.Options{
		RetentionDuration:     config.RetentionDuration,
		BlockRanges:           config.BlockRanges,
		NoLockfile:            false,
		WALFlushInterval:      config.WALFlushInterval,
		CompactionConcurrency: config.CompactionConcurrency,
	}

	// Enable WAL
	if config.EnableWAL {
		opts.WAL = &wal.Options{
			Dir: filepath.Join(config.DataDir, "wal"),
		}
	}

	// Create the TSDB
	db, err := tsdb.Open(config.DataDir, logger, reg, opts)
	if err != nil {
		return nil, fmt.Errorf("open tsdb: %w", err)
	}

	metrics := NewTSDBMetrics(reg)

	tsdbInstance := &TSDB{
		db:      db,
		logger:  logger,
		config:  config,
		metrics: metrics,
	}

	// Start background compaction
	tsdbInstance.startCompaction(ctx)

	// Start metrics updater
	go tsdbInstance.updateMetricsLoop(ctx)

	logger.Info("TSDB initialized",
		zap.String("data_dir", config.DataDir),
		zap.Duration("retention", config.RetentionDuration),
		zap.Int64s("block_ranges", config.BlockRanges),
		zap.Int("max_series", config.MaxActiveSeries),
	)

	return tsdbInstance, nil
}

// startCompaction starts the background compaction loop
func (t *TSDB) startCompaction(ctx context.Context) {
	t.compactor = tsdb.NewCompactor(t.logger, t.db, tsdb.CompactorOptions{
		RetentionDuration: t.config.RetentionDuration,
		BlockRanges:       t.config.BlockRanges,
	})

	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				start := time.Now()
				if err := t.compactor.Write(ctx); err != nil {
					t.logger.Error("Compaction failed", zap.Error(err))
				} else {
					t.metrics.CompactionDuration.Observe(time.Since(start).Seconds())
					t.logger.Debug("Compaction completed", zap.Duration("duration", time.Since(start)))
				}
			}
		}
	}()
}

// updateMetricsLoop periodically updates TSDB metrics
func (t *TSDB) updateMetricsLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.updateMetrics()
		}
	}
}

// updateMetrics updates the Prometheus metrics from TSDB state
func (t *TSDB) updateMetrics() {
	// Head metrics
	t.metrics.HeadSeries.Set(float64(t.db.Head().NumSeries()))
	t.metrics.HeadSamples.Add(float64(t.db.Head().NumSamples()))
	
	// Estimate head bytes (approximate)
	head := t.db.Head()
	t.metrics.HeadBytes.Set(float64(head.ChunkRange().Length() * 16)) // Rough estimate

	// Block count
	blocks := t.db.Blocks()
	t.metrics.BlockCount.Set(float64(len(blocks)))
}

// Appender returns a new appender for writing samples
func (t *TSDB) Appender(ctx context.Context) tsdb.Appender {
	return t.db.Appender(ctx)
}

// Querier returns a querier for the given time range
func (t *TSDB) Querier(ctx context.Context, mint, maxt int64) (tsdb.Querier, error) {
	return t.db.Querier(ctx, mint, maxt)
}

// Snapshot creates a snapshot of the current TSDB state
func (t *TSDB) Snapshot(dir string) error {
	return t.db.Snapshot(dir)
}

// Close closes the TSDB and releases resources
func (t *TSDB) Close() error {
	if t.compactor != nil {
		t.compactor.Close()
	}
	return t.db.Close()
}

// DB returns the underlying TSDB instance (for advanced usage)
func (t *TSDB) DB() *tsdb.DB {
	return t.db
}

// Config returns the TSDB configuration
func (t *TSDB) Config() *TSDBConfig {
	return t.config
}

// Metrics returns the TSDB metrics
func (t *TSDB) Metrics() *TSDBMetrics {
	return t.metrics
}

// ValidateRetention validates that retention settings are correctly applied
func (t *TSDB) ValidateRetention() error {
	// Check that retention duration matches config
	if t.db.Head().MinTime() != 0 {
		// Head block exists, check its time range
		mint, maxt := t.db.Head().TimeRange()
		age := time.Now().UnixMilli() - maxt
		if age > t.config.RetentionDuration.Milliseconds() {
			return fmt.Errorf("head block exceeds retention: age=%dms, retention=%dms", age, t.config.RetentionDuration.Milliseconds())
		}
	}

	// Check persisted blocks
	blocks := t.db.Blocks()
	for _, b := range blocks {
		mint, maxt := b.Meta().MinTime, b.Meta().MaxTime
		age := time.Now().UnixMilli() - maxt
		if age > t.config.RetentionDuration.Milliseconds() {
			t.logger.Warn("Block exceeds retention, should be deleted",
				zap.Int64("block_mint", mint),
				zap.Int64("block_maxt", maxt),
				zap.Int64("age_ms", age),
				zap.Int64("retention_ms", t.config.RetentionDuration.Milliseconds()),
			)
		}
	}

	return nil
}

// ValidateBlockRanges validates that block ranges are correctly configured
func (t *TSDB) ValidateBlockRanges() error {
	expectedRanges := t.config.BlockRanges
	blocks := t.db.Blocks()

	rangeCounts := make(map[int64]int)
	for _, b := range blocks {
		duration := b.Meta().MaxTime - b.Meta().MinTime
		rangeCounts[duration]++
	}

	t.logger.Info("Block range distribution", zap.Any("ranges", rangeCounts))

	// Check that we have blocks in expected ranges (after compaction has run)
	for _, expected := range expectedRanges {
		if rangeCounts[expected] == 0 {
			t.logger.Debug("No blocks found for expected range", zap.Int64("expected_range_ms", expected))
		}
	}

	return nil
}

// GetStorageStats returns storage statistics
func (t *TSDB) GetStorageStats() map[string]interface{} {
	blocks := t.db.Blocks()
	totalSize := int64(0)
	for _, b := range blocks {
		// Block size estimation
		totalSize += int64(b.Meta().Stats.NumSamples * 16) // Rough estimate
	}

	return map[string]interface{}{
		"head_series":       t.db.Head().NumSeries(),
		"head_samples":      t.db.Head().NumSamples(),
		"block_count":       len(blocks),
		"estimated_size_mb": totalSize / 1024 / 1024,
		"retention_hours":   t.config.RetentionDuration.Hours(),
		"block_ranges":      t.config.BlockRanges,
		"max_active_series": t.config.MaxActiveSeries,
	}
}
