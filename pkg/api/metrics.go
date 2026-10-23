package api

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler returns an HTTP handler that exposes all registered metrics
func MetricsHandler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry:          reg,
		EnableOpenMetrics: true,
		MaxRequestsInFlight: 100,
		Timeout:           30 * time.Second,
	})
}

// RegisterMetrics registers all FinOps metrics with the given registry
func RegisterMetrics(reg *prometheus.Registerer) {
	// This function is called during initialization to register all metrics
	// The actual metrics are registered in their respective packages:
	// - storage.NewTSDBMetrics() registers TSDB metrics
	// - storage.NewIngester() registers ingester metrics
	// - api.NewQueryAPI() registers query metrics
	// This function exists as a central point for documentation
}
