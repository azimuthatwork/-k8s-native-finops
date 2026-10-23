package storage

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"go.uber.org/zap"
)

// WriteBatch batches samples for efficient writing
type WriteBatch struct {
	samples    []Sample
	exemplars  []Exemplar
	maxSize    int
	mu         sync.Mutex
}

// Sample represents a single metric sample
type Sample struct {
	Labels     labels.Labels
	Timestamp  int64
	Value      float64
}

// Exemplar represents an exemplar attachment
type Exemplar struct {
	Labels     labels.Labels
	Timestamp  int64
	Value      float64
	TraceID    string
	SpanID     string
}

// NewWriteBatch creates a new write batch
func NewWriteBatch(maxSize int) *WriteBatch {
	return &WriteBatch{
		samples:   make([]Sample, 0, maxSize),
		exemplars: make([]Exemplar, 0, maxSize),
		maxSize:   maxSize,
	}
}

// Add adds a sample to the batch
func (b *WriteBatch) Add(lbls labels.Labels, timestamp int64, value float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = append(b.samples, Sample{
		Labels:    lbls,
		Timestamp: timestamp,
		Value:     value,
	})
}

// AddExemplar adds an exemplar to the batch
func (b *WriteBatch) AddExemplar(lbls labels.Labels, ex *value.Exemplar) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ex != nil {
		b.exemplars = append(b.exemplars, Exemplar{
			Labels:    lbls,
			Timestamp: ex.Ts,
			Value:     ex.Value,
			TraceID:   ex.Labels.Get("trace_id"),
			SpanID:    ex.Labels.Get("span_id"),
		})
	}
}

// Len returns the number of samples in the batch
func (b *WriteBatch) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.samples)
}

// Commit writes the batch to the appender
func (b *WriteBatch) Commit(app tsdb.Appender) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, s := range b.samples {
		_, err := app.Add(s.Labels, s.Timestamp, s.Value)
		if err != nil {
			return err
		}
	}

	for _, ex := range b.exemplars {
		// Add exemplar to the last added series
		// Note: TSDB exemplars are added via AddExemplar on the appender
		_ = ex // Placeholder for exemplar support
	}

	return nil
}

// Reset clears the batch
func (b *WriteBatch) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = b.samples[:0]
	b.exemplars = b.exemplars[:0]
}

// Ingester handles batched writes to TSDB
type Ingester struct {
	db           *TSDB
	batch        *WriteBatch
	flushInterval time.Duration
	maxBatchSize int
	labelCache   *LabelCache
	metrics      *IngesterMetrics
	mu           sync.Mutex
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// IngesterMetrics holds ingester-specific metrics
type IngesterMetrics struct {
	BatchSize       prometheus.Histogram
	FlushDuration   prometheus.Histogram
	RejectedSamples prometheus.Counter
	ExemplarsDropped prometheus.Counter
	LabelDropped    prometheus.Counter
	LabelTruncated  prometheus.Counter
}

// NewIngesterMetrics creates ingester metrics
func NewIngesterMetrics(reg prometheus.Registerer) *IngesterMetrics {
	m := &IngesterMetrics{
		BatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "finops_ingester_batch_size",
			Help:    "Size of batches flushed to TSDB",
			Buckets: prometheus.ExponentialBuckets(100, 2, 10),
		}),
		FlushDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
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
		LabelDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_label_normalizer_dropped_total",
			Help: "Total number of labels dropped due to denylist",
		}),
		LabelTruncated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_label_normalizer_truncated_total",
			Help: "Total number of label values truncated",
		}),
	}

	if reg != nil {
		reg.MustRegister(
			m.BatchSize, m.FlushDuration,
			m.RejectedSamples, m.ExemplarsDropped,
			m.LabelDropped, m.LabelTruncated,
		)
	}

	return m
}

// LabelCache caches namespace labels for enrichment
type LabelCache struct {
	mu       sync.RWMutex
	nsLabels map[string]map[string]string // namespace -> labels
}

// NewLabelCache creates a new label cache
func NewLabelCache() *LabelCache {
	return &LabelCache{
		nsLabels: make(map[string]map[string]string),
	}
}

// SetNamespaceLabels sets labels for a namespace
func (c *LabelCache) SetNamespaceLabels(ns string, lbls map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nsLabels[ns] = lbls
}

// GetNamespaceLabels gets labels for a namespace
func (c *LabelCache) GetNamespaceLabels(ns string) (map[string]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	lbls, ok := c.nsLabels[ns]
	return lbls, ok
}

// IngesterConfig holds ingester configuration
type IngesterConfig struct {
	FlushInterval   time.Duration
	MaxBatchSize    int
	MaxActiveSeries int
	LabelAllowlist  []string
	LabelDenylist   []string
	MaxLabelLength  int
}

// DefaultIngesterConfig returns default ingester configuration
func DefaultIngesterConfig() *IngesterConfig {
	return &IngesterConfig{
		FlushInterval:   5 * time.Second,
		MaxBatchSize:    10000,
		MaxActiveSeries: 500000,
		LabelAllowlist: []string{
			"namespace", "workload", "kind", "cost_center", "team",
			"node", "instance_type", "zone", "container",
			"persistentvolumeclaim", "storageclass",
			"reason", "level", "operation", "status",
		},
		LabelDenylist: []string{
			"pod_uid", "container_id", "pod_template_generation",
			"revision", "hash", "uuid", "id",
		},
		MaxLabelLength: 100,
	}
}

// NewIngester creates a new ingester
func NewIngester(db *TSDB, config *IngesterConfig, labelCache *LabelCache, logger *zap.Logger, reg prometheus.Registerer) *Ingester {
	if config == nil {
		config = DefaultIngesterConfig()
	}

	i := &Ingester{
		db:            db,
		batch:         NewWriteBatch(config.MaxBatchSize),
		flushInterval: config.FlushInterval,
		maxBatchSize:  config.MaxBatchSize,
		labelCache:    labelCache,
		metrics:       NewIngesterMetrics(reg),
		stopCh:        make(chan struct{}),
	}

	// Start periodic flush
	i.wg.Add(1)
	go i.periodicFlush(logger)

	return i
}

// Write writes metrics to the ingester
func (i *Ingester) Write(metrics []prometheus.Metric) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Check cardinality limit
	currentSeries := i.db.DB().Head().NumSeries()
	if currentSeries >= i.db.Config().MaxActiveSeries {
		i.metrics.RejectedSamples.Add(float64(len(metrics)))
		return ErrCardinalityLimitExceeded
	}

	for _, m := range metrics {
		// Convert prometheus.Metric to labels.Labels
		lbls := i.metricToLabels(m)
		
		// Sanitize and enrich labels
		lbls = i.sanitizeLabels(lbls)
		lbls = i.enrichCostLabels(lbls)

		// Add to batch
		i.batch.Add(lbls, m.Timestamp(), m.Value())

		// Handle exemplar
		if ex := m.Exemplar(); ex != nil {
			i.batch.AddExemplar(lbls, ex)
		}
	}

	// Flush if batch is full
	if i.batch.Len() >= i.maxBatchSize {
		if err := i.flushLocked(); err != nil {
			return err
		}
	}

	return nil
}

// metricToLabels converts a prometheus.Metric to labels.Labels
func (i *Ingester) metricToLabels(m prometheus.Metric) labels.Labels {
	var lbls labels.Labels
	m.Write(&prometheusMetricWriter{labels: &lbls})
	return lbls
}

// prometheusMetricWriter implements prometheus.MetricWriter
type prometheusMetricWriter struct {
	labels *labels.Labels
}

func (w *prometheusMetricWriter) Write(l labels.Labels) {
	*w.labels = l
}

// sanitizeLabels sanitizes and validates labels
func (i *Ingester) sanitizeLabels(lbls labels.Labels) labels.Labels {
	var result labels.Labels
	result = make(labels.Labels, 0, len(lbls))

	for _, l := range lbls {
		name := l.Name
		value := l.Value

		// Skip empty label names
		if name == "" {
			continue
		}

		// Check denylist
		denied := false
		for _, deniedName := range i.getDenylist() {
			if name == deniedName {
				denied = true
				i.metrics.LabelDropped.Inc()
				break
			}
		}
		if denied {
			continue
		}

		// Sanitize label name (Prometheus format: [a-zA-Z_][a-zA-Z0-9_]*)
		sanitizedName := sanitizeLabelName(name)
		if sanitizedName != name {
			name = sanitizedName
		}

		// Truncate long label values
		if len(value) > i.getMaxLabelLength() {
			value = value[:i.getMaxLabelLength()]
			i.metrics.LabelTruncated.Inc()
		}

		// Sanitize label value (remove control characters)
		value = sanitizeLabelValue(value)

		result = append(result, labels.Label{Name: name, Value: value})
	}

	return result
}

// enrichCostLabels adds cost_center and team labels from namespace cache
func (i *Ingester) enrichCostLabels(lbls labels.Labels) labels.Labels {
	ns := lbls.Get("namespace")
	if ns == "" {
		return lbls
	}

	if nsLabels, ok := i.labelCache.GetNamespaceLabels(ns); ok {
		costCenter := nsLabels["cost-center"]
		team := nsLabels["team"]
		
		if costCenter != "" && lbls.Get("cost_center") == "" {
			lbls = append(lbls, labels.Label{Name: "cost_center", Value: costCenter})
		}
		if team != "" && lbls.Get("team") == "" {
			lbls = append(lbls, labels.Label{Name: "team", Value: team})
		}
	}

	return lbls
}

// getDenylist returns the label denylist
func (i *Ingester) getDenylist() []string {
	// This would come from config in real implementation
	return []string{
		"pod_uid", "container_id", "pod_template_generation",
		"revision", "hash", "uuid", "id",
	}
}

// getMaxLabelLength returns the max label length
func (i *Ingester) getMaxLabelLength() int {
	return 100
}

// periodicFlush periodically flushes the batch
func (i *Ingester) periodicFlush(logger *zap.Logger) {
	defer i.wg.Done()
	ticker := time.NewTicker(i.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-i.stopCh:
			// Final flush on stop
			i.mu.Lock()
			if i.batch.Len() > 0 {
				if err := i.flushLocked(); err != nil {
					logger.Error("Final flush failed", zap.Error(err))
				}
			}
			i.mu.Unlock()
			return
		case <-ticker.C:
			i.mu.Lock()
			if i.batch.Len() > 0 {
				if err := i.flushLocked(); err != nil {
					logger.Error("Periodic flush failed", zap.Error(err))
				}
			}
			i.mu.Unlock()
		}
	}
}

// flushLocked flushes the batch (must hold mutex)
func (i *Ingester) flushLocked() error {
	start := time.Now()
	app := i.db.Appender(context.Background())
	
	if err := i.batch.Commit(app); err != nil {
		return err
	}
	
	if err := app.Commit(); err != nil {
		return err
	}

	i.metrics.BatchSize.Observe(float64(i.batch.Len()))
	i.metrics.FlushDuration.Observe(time.Since(start).Seconds())
	
	i.batch.Reset()
	return nil
}

// Stop stops the ingester
func (i *Ingester) Stop() error {
	close(i.stopCh)
	i.wg.Wait()
	
	// Final flush
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.flushLocked()
}

// ErrCardinalityLimitExceeded is returned when cardinality limit is exceeded
var ErrCardinalityLimitExceeded = &CardinalityError{}

type CardinalityError struct{}

func (e *CardinalityError) Error() string {
	return "cardinality limit exceeded"
}

func (e *CardinalityError) Is(target error) bool {
	_, ok := target.(*CardinalityError)
	return ok
}

// sanitizeLabelName sanitizes a label name to Prometheus format
func sanitizeLabelName(name string) string {
	// Prometheus label names: [a-zA-Z_][a-zA-Z0-9_]*
	if len(name) == 0 {
		return ""
	}
	
	result := make([]byte, 0, len(name))
	for i, r := range name {
		if i == 0 {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
				result = append(result, byte(r))
			} else {
				result = append(result, '_')
			}
		} else {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				result = append(result, byte(r))
			} else {
				result = append(result, '_')
			}
		}
	}
	return string(result)
}

// sanitizeLabelValue sanitizes a label value
func sanitizeLabelValue(value string) string {
	// Remove control characters
	result := make([]byte, 0, len(value))
	for _, r := range value {
		if r >= 32 && r != 127 { // Printable ASCII
			result = append(result, byte(r))
		}
	}
	return string(result)
}
