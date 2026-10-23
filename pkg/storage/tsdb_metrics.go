package storage

import (
	"github.com/prometheus/client_golang/prometheus"
)

// CardinalityAlertMetrics provides metrics for cardinality alerting
type CardinalityAlertMetrics struct {
	SeriesLimitExceeded prometheus.Counter
	SeriesNearLimit     prometheus.Gauge
	SeriesUsageRatio    prometheus.Gauge
}

// NewCardinalityAlertMetrics creates metrics for cardinality alerting
func NewCardinalityAlertMetrics(reg prometheus.Registerer) *CardinalityAlertMetrics {
	m := &CardinalityAlertMetrics{
		SeriesLimitExceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_tsdb_series_limit_exceeded_total",
			Help: "Total number of times the series limit was exceeded",
		}),
		SeriesNearLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "finops_tsdb_series_near_limit",
			Help: "Whether the series count is near the limit (1 = yes, 0 = no)",
		}),
		SeriesUsageRatio: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "finops_tsdb_series_usage_ratio",
			Help: "Current series usage ratio (0.0 to 1.0)",
		}),
	}

	if reg != nil {
		reg.MustRegister(
			m.SeriesLimitExceeded,
			m.SeriesNearLimit,
			m.SeriesUsageRatio,
		)
	}

	return m
}

// UpdateFromGuard updates alert metrics from a cardinality guard
func (m *CardinalityAlertMetrics) UpdateFromGuard(guard *CardinalityGuard) {
	m.SeriesUsageRatio.Set(guard.GetUsageRatio())
	if guard.IsNearLimit() {
		m.SeriesNearLimit.Set(1)
	} else {
		m.SeriesNearLimit.Set(0)
	}
}

// RecordLimitExceeded increments the limit exceeded counter
func (m *CardinalityAlertMetrics) RecordLimitExceeded() {
	m.SeriesLimitExceeded.Inc()
}
