package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/promql/parser"
	"github.com/yourorg/kube-native-finops/pkg/storage"
)

// QueryAPI handles PromQL-compatible query endpoints
type QueryAPI struct {
	querier  *storage.Querier
	engine   *promqlEngine
	metrics  *QueryMetrics
	registry *prometheus.Registry
}

// QueryMetrics holds query-related metrics
type QueryMetrics struct {
	QueryDuration    *prometheus.HistogramVec
	QuerySamples     prometheus.Counter
	QueryErrors      prometheus.Counter
	QueryTotal       prometheus.Counter
	ActiveQueries    prometheus.Gauge
}

// NewQueryMetrics creates new query metrics
func NewQueryMetrics(reg prometheus.Registerer) *QueryMetrics {
	m := &QueryMetrics{
		QueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "finops_query_duration_seconds",
			Help:    "Duration of query executions",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
		}, []string{"type", "status"}),
		QuerySamples: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_query_samples_total",
			Help: "Total number of samples returned by queries",
		}),
		QueryErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_query_errors_total",
			Help: "Total number of query errors",
		}),
		QueryTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_query_total",
			Help: "Total number of queries executed",
		}),
		ActiveQueries: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "finops_active_queries",
			Help: "Number of currently active queries",
		}),
	}

	if reg != nil {
		reg.MustRegister(
			m.QueryDuration,
			m.QuerySamples,
			m.QueryErrors,
			m.QueryTotal,
			m.ActiveQueries,
		)
	}

	return m
}

// promqlEngine wraps the PromQL engine
type promqlEngine struct {
	// In a real implementation, this would be *promql.Engine
	// For now, it's a placeholder
}

// NewQueryAPI creates a new query API
func NewQueryAPI(querier *storage.Querier, reg prometheus.Registerer) *QueryAPI {
	return &QueryAPI{
		querier: querier,
		engine:  &promqlEngine{},
		metrics: NewQueryMetrics(reg),
		registry: reg,
	}
}

// QueryRequest represents a query request
type QueryRequest struct {
	Query string
	Time  time.Time
}

// QueryRangeRequest represents a range query request
type QueryRangeRequest struct {
	Query string
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// QueryResponse represents a query response
type QueryResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data"`
	Error  string      `json:"error,omitempty"`
}

// HandleQuery handles instant queries
func (q *QueryAPI) HandleQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q.metrics.ActiveQueries.Inc()
	defer q.metrics.ActiveQueries.Dec()

	query := r.URL.Query().Get("query")
	if query == "" {
		q.writeError(w, "query parameter is required", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	timeParam := r.URL.Query().Get("time")
	var ts time.Time
	if timeParam != "" {
		unixTime, err := strconv.ParseFloat(timeParam, 64)
		if err != nil {
			q.writeError(w, "invalid time parameter", http.StatusBadRequest)
			q.metrics.QueryErrors.Inc()
			return
		}
		ts = time.Unix(int64(unixTime), int64((unixTime-float64(int64(unixTime)))*1e9))
	} else {
		ts = time.Now()
	}

	// Parse query to validate
	_, err := parser.ParseExpr(query)
	if err != nil {
		q.writeError(w, "invalid query: "+err.Error(), http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		q.metrics.QueryDuration.WithLabelValues("instant", "success").Observe(v)
	}))

	// Execute query
	// In a real implementation, this would use the PromQL engine
	// value, err := q.engine.Query(ctx, query, ts)
	// For now, return a placeholder response
	value := map[string]interface{}{
		"resultType": "vector",
		"result":     []interface{}{},
	}

	timer.ObserveDuration()

	if err != nil {
		q.metrics.QueryDuration.WithLabelValues("instant", "error").Observe(time.Since(time.Now()).Seconds())
		q.metrics.QueryErrors.Inc()
		q.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	q.metrics.QueryTotal.Inc()
	q.writeJSON(w, QueryResponse{
		Status: "success",
		Data:   value,
	})
}

// HandleQueryRange handles range queries
func (q *QueryAPI) HandleQueryRange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q.metrics.ActiveQueries.Inc()
	defer q.metrics.ActiveQueries.Dec()

	query := r.URL.Query().Get("query")
	if query == "" {
		q.writeError(w, "query parameter is required", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")
	stepParam := r.URL.Query().Get("step")

	if startParam == "" || endParam == "" || stepParam == "" {
		q.writeError(w, "start, end, and step parameters are required", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	startUnix, err := strconv.ParseFloat(startParam, 64)
	if err != nil {
		q.writeError(w, "invalid start parameter", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	endUnix, err := strconv.ParseFloat(endParam, 64)
	if err != nil {
		q.writeError(w, "invalid end parameter", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	stepFloat, err := strconv.ParseFloat(stepParam, 64)
	if err != nil {
		q.writeError(w, "invalid step parameter", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	start := time.Unix(int64(startUnix), int64((startUnix-float64(int64(startUnix)))*1e9))
	end := time.Unix(int64(endUnix), int64((endUnix-float64(int64(endUnix)))*1e9))
	step := time.Duration(stepFloat * float64(time.Second))

	// Parse query to validate
	_, err = parser.ParseExpr(query)
	if err != nil {
		q.writeError(w, "invalid query: "+err.Error(), http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		q.metrics.QueryDuration.WithLabelValues("range", "success").Observe(v)
	}))

	// Execute range query
	// In a real implementation, this would use the PromQL engine
	// value, err := q.engine.QueryRange(ctx, query, start, end, step)
	value := map[string]interface{}{
		"resultType": "matrix",
		"result":     []interface{}{},
	}

	timer.ObserveDuration()

	if err != nil {
		q.metrics.QueryDuration.WithLabelValues("range", "error").Observe(time.Since(time.Now()).Seconds())
		q.metrics.QueryErrors.Inc()
		q.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	q.metrics.QueryTotal.Inc()
	q.writeJSON(w, QueryResponse{
		Status: "success",
		Data:   value,
	})
}

// HandleSeries handles series discovery
func (q *QueryAPI) HandleSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q.metrics.ActiveQueries.Inc()
	defer q.metrics.ActiveQueries.Dec()

	match := r.URL.Query()["match[]"]
	if len(match) == 0 {
		q.writeError(w, "match[] parameter is required", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")

	var start, end time.Time
	if startParam != "" {
		startUnix, _ := strconv.ParseFloat(startParam, 64)
		start = time.Unix(int64(startUnix), 0)
	} else {
		start = time.Now().Add(-24 * time.Hour)
	}

	if endParam != "" {
		endUnix, _ := strconv.ParseFloat(endParam, 64)
		end = time.Unix(int64(endUnix), 0)
	} else {
		end = time.Now()
	}

	// In a real implementation, this would query the TSDB for series
	// For now, return empty result
	value := []interface{}{}

	q.metrics.QueryTotal.Inc()
	q.writeJSON(w, QueryResponse{
		Status: "success",
		Data:   value,
	})
}

// HandleLabels handles label names discovery
func (q *QueryAPI) HandleLabels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q.metrics.ActiveQueries.Inc()
	defer q.metrics.ActiveQueries.Dec()

	// In a real implementation, this would query the TSDB for label names
	value := []string{
		"namespace", "workload", "kind", "cost_center", "team",
		"node", "instance_type", "zone", "container",
		"storageclass", "persistentvolumeclaim",
	}

	q.metrics.QueryTotal.Inc()
	q.writeJSON(w, QueryResponse{
		Status: "success",
		Data:   value,
	})
}

// HandleLabelValues handles label values discovery
func (q *QueryAPI) HandleLabelValues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q.metrics.ActiveQueries.Inc()
	defer q.metrics.ActiveQueries.Dec()

	labelName := r.URL.Query().Get("label")
	if labelName == "" {
		q.writeError(w, "label parameter is required", http.StatusBadRequest)
		q.metrics.QueryErrors.Inc()
		return
	}

	match := r.URL.Query()["match[]"]

	// In a real implementation, this would query the TSDB for label values
	value := []string{}

	q.metrics.QueryTotal.Inc()
	q.writeJSON(w, QueryResponse{
		Status: "success",
		Data:   value,
	})
}

// writeJSON writes a JSON response
func (q *QueryAPI) writeJSON(w http.ResponseWriter, resp QueryResponse) {
	w.Header().Set("Content-Type", "application/json")
	// In a real implementation, use json.NewEncoder(w).Encode(resp)
	// For now, placeholder
}

// writeError writes an error response
func (q *QueryAPI) writeError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	// In a real implementation, write JSON error
	_ = message // placeholder
}
// Package api provides the HTTP API for querying the TSDB.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
)

// QueryAPI handles PromQL-compatible query endpoints.
type QueryAPI struct {
	querier     storage.Querier
	engine      *promql.Engine
	reg         *prometheus.Registry
	queryTimeout time.Duration
	maxSamples  int
}

// QueryResponse represents the standard Prometheus API response format.
type QueryResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data"`
	Error  string      `json:"error,omitempty"`
	ErrorType string   `json:"errorType,omitempty"`
}

// NewQueryAPI creates a new QueryAPI instance.
func NewQueryAPI(querier storage.Querier, reg *prometheus.Registry) *QueryAPI {
	// Create PromQL engine with default options
	engine := promql.NewEngine(promql.EngineOpts{
		MaxSamples: 50000000,
		Timeout:    2 * time.Minute,
		Reg:        reg,
		// Enable lookback delta for rate() etc.
		LookbackDelta: 5 * time.Minute,
	})

	return &QueryAPI{
		querier:      querier,
		engine:       engine,
		reg:          reg,
		queryTimeout: 2 * time.Minute,
		maxSamples:   50000000,
	}
}

// HandleQuery handles instant queries (GET /api/v1/query).
func (q *QueryAPI) HandleQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Parse query parameters
	query := r.URL.Query().Get("query")
	if query == "" {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", "query parameter is required")
		return
	}

	timeParam := r.URL.Query().Get("time")
	var ts time.Time
	if timeParam != "" {
		t, err := parseTime(timeParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid time parameter: %v", err))
			return
		}
		ts = t
	} else {
		ts = time.Now()
	}

	timeoutParam := r.URL.Query().Get("timeout")
	timeout := q.queryTimeout
	if timeoutParam != "" {
		t, err := parseDuration(timeoutParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid timeout parameter: %v", err))
			return
		}
		timeout = t
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute instant query
	result, err := q.engine.Query(ctx, q.querier, query, ts)
	if err != nil {
		q.respondError(w, http.StatusInternalServerError, "ERR_EXEC", err.Error())
		return
	}

	q.respondSuccess(w, result)
}

// HandleQueryRange handles range queries (GET /api/v1/query_range).
func (q *QueryAPI) HandleQueryRange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	query := r.URL.Query().Get("query")
	if query == "" {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", "query parameter is required")
		return
	}

	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")
	stepParam := r.URL.Query().Get("step")

	if startParam == "" || endParam == "" || stepParam == "" {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", "start, end, and step parameters are required")
		return
	}

	start, err := parseTime(startParam)
	if err != nil {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid start parameter: %v", err))
		return
	}

	end, err := parseTime(endParam)
	if err != nil {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid end parameter: %v", err))
		return
	}

	step, err := parseDuration(stepParam)
	if err != nil {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid step parameter: %v", err))
		return
	}

	if step <= 0 {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", "step must be positive")
		return
	}

	if end.Before(start) {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", "end must be after start")
		return
	}

	timeoutParam := r.URL.Query().Get("timeout")
	timeout := q.queryTimeout
	if timeoutParam != "" {
		t, err := parseDuration(timeoutParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid timeout parameter: %v", err))
			return
		}
		timeout = t
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute range query
	result, err := q.engine.QueryRange(ctx, q.querier, query, start, end, step)
	if err != nil {
		q.respondError(w, http.StatusInternalServerError, "ERR_EXEC", err.Error())
		return
	}

	q.respondSuccess(w, result)
}

// HandleSeries handles series discovery (GET /api/v1/series).
func (q *QueryAPI) HandleSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	matchParams := r.URL.Query()["match[]"]
	if len(matchParams) == 0 {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", "match[] parameter is required")
		return
	}

	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")

	var start, end time.Time
	if startParam != "" {
		t, err := parseTime(startParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid start parameter: %v", err))
			return
		}
		start = t
	} else {
		start = time.Now().Add(-24 * time.Hour)
	}

	if endParam != "" {
		t, err := parseTime(endParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid end parameter: %v", err))
			return
		}
		end = t
	} else {
		end = time.Now()
	}

	// Create matchers from match[] parameters
	matchers := make([]*labels.Matcher, 0, len(matchParams))
	for _, m := range matchParams {
		matcher, err := parser.ParseMetricSelector(m)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid match[] parameter: %v", err))
			return
		}
		matchers = append(matchers, matcher...)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, q.queryTimeout)
	defer cancel()

	// Query series
	seriesSet, err := q.querier.Select(ctx, false, nil, matchers...)
	if err != nil {
		q.respondError(w, http.StatusInternalServerError, "ERR_EXEC", err.Error())
		return
	}
	defer seriesSet.Close()

	// Collect series
	var series []map[string]string
	for seriesSet.Next() {
		series = append(series, seriesSet.At().Labels().Map())
	}
	if err := seriesSet.Err(); err != nil {
		q.respondError(w, http.StatusInternalServerError, "ERR_EXEC", err.Error())
		return
	}

	q.respondSuccess(w, series)
}

// HandleLabels handles label names discovery (GET /api/v1/labels).
func (q *QueryAPI) HandleLabels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")

	var start, end time.Time
	if startParam != "" {
		t, err := parseTime(startParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid start parameter: %v", err))
			return
		}
		start = t
	} else {
		start = time.Now().Add(-24 * time.Hour)
	}

	if endParam != "" {
		t, err := parseTime(endParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid end parameter: %v", err))
			return
		}
		end = t
	} else {
		end = time.Now()
	}

	matchParams := r.URL.Query()["match[]"]
	matchers := make([]*labels.Matcher, 0, len(matchParams))
	for _, m := range matchParams {
		matcher, err := parser.ParseMetricSelector(m)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid match[] parameter: %v", err))
			return
		}
		matchers = append(matchers, matcher...)
	}

	ctx, cancel := context.WithTimeout(ctx, q.queryTimeout)
	defer cancel()

	// Get label names
	labelNames, err := q.querier.LabelNames(ctx, start, end, matchers...)
	if err != nil {
		q.respondError(w, http.StatusInternalServerError, "ERR_EXEC", err.Error())
		return
	}

	q.respondSuccess(w, labelNames)
}

// HandleLabelValues handles label values discovery (GET /api/v1/label/<name>/values).
func (q *QueryAPI) HandleLabelValues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract label name from URL path
	// Path format: /api/v1/label/<name>/values
	path := r.URL.Path
	prefix := "/api/v1/label/"
	if len(path) <= len(prefix)+len("/values") || path[len(path)-len("/values"):] != "/values" {
		q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", "invalid label values path")
		return
	}
	labelName := path[len(prefix) : len(path)-len("/values")]

	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")

	var start, end time.Time
	if startParam != "" {
		t, err := parseTime(startParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid start parameter: %v", err))
			return
		}
		start = t
	} else {
		start = time.Now().Add(-24 * time.Hour)
	}

	if endParam != "" {
		t, err := parseTime(endParam)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid end parameter: %v", err))
			return
		}
		end = t
	} else {
		end = time.Now()
	}

	matchParams := r.URL.Query()["match[]"]
	matchers := make([]*labels.Matcher, 0, len(matchParams))
	for _, m := range matchParams {
		matcher, err := parser.ParseMetricSelector(m)
		if err != nil {
			q.respondError(w, http.StatusBadRequest, "ERR_INVALID_PARAM", fmt.Sprintf("invalid match[] parameter: %v", err))
			return
		}
		matchers = append(matchers, matcher...)
	}

	ctx, cancel := context.WithTimeout(ctx, q.queryTimeout)
	defer cancel()

	// Get label values
	labelValues, err := q.querier.LabelValues(ctx, labelName, start, end, matchers...)
	if err != nil {
		q.respondError(w, http.StatusInternalServerError, "ERR_EXEC", err.Error())
		return
	}

	q.respondSuccess(w, labelValues)
}

// respondSuccess writes a successful response.
func (q *QueryAPI) respondSuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(QueryResponse{
		Status: "success",
		Data:   data,
	})
}

// respondError writes an error response.
func (q *QueryAPI) respondError(w http.ResponseWriter, statusCode int, errorType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(QueryResponse{
		Status:    "error",
		ErrorType: errorType,
		Error:     message,
	})
}

// parseTime parses a time parameter (Unix timestamp or RFC3339).
func parseTime(s string) (time.Time, error) {
	// Try Unix timestamp first
	if ts, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Unix(int64(ts), int64((ts-float64(int64(ts)))*1e9)), nil
	}
	// Try RFC3339
	return time.Parse(time.RFC3339, s)
}

// parseDuration parses a duration parameter (seconds or Go duration string).
func parseDuration(s string) (time.Duration, error) {
	// Try seconds first
	if d, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(d * float64(time.Second)), nil
	}
	// Try Go duration string
	return time.ParseDuration(s)
}

// ValidateQuery validates a PromQL query without executing it.
func (q *QueryAPI) ValidateQuery(query string) error {
	_, err := parser.ParseExpr(query)
	return err
}

// GetEngine returns the PromQL engine for advanced usage.
func (q *QueryAPI) GetEngine() *promql.Engine {
	return q.engine
}

// SetQueryTimeout sets the default query timeout.
func (q *QueryAPI) SetQueryTimeout(timeout time.Duration) {
	q.queryTimeout = timeout
}

// SetMaxSamples sets the maximum samples per query.
func (q *QueryAPI) SetMaxSamples(maxSamples int) {
	q.maxSamples = maxSamples
	q.engine = promql.NewEngine(promql.EngineOpts{
		MaxSamples:    maxSamples,
		Timeout:       q.queryTimeout,
		Reg:           q.reg,
		LookbackDelta: 5 * time.Minute,
	})
}

// QueryMetrics returns metrics about query execution.
type QueryMetrics struct {
	TotalQueries      prometheus.Counter
	FailedQueries     prometheus.Counter
	QueryDuration     prometheus.Histogram
	QuerySamples      prometheus.Histogram
	ConcurrentQueries prometheus.Gauge
}

// NewQueryMetrics creates query metrics.
func NewQueryMetrics(reg *prometheus.Registry) *QueryMetrics {
	m := &QueryMetrics{
		TotalQueries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_query_total",
			Help: "Total number of queries executed",
		}),
		FailedQueries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_query_failed_total",
			Help: "Total number of failed queries",
		}),
		QueryDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "finops_query_duration_seconds",
			Help:    "Query execution duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		QuerySamples: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "finops_query_samples_total",
			Help:    "Number of samples processed per query",
			Buckets: []float64{100, 1000, 10000, 100000, 1000000},
		}),
		ConcurrentQueries: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "finops_query_concurrent",
			Help: "Number of concurrent queries",
		}),
	}
	reg.MustRegister(
		m.TotalQueries,
		m.FailedQueries,
		m.QueryDuration,
		m.QuerySamples,
		m.ConcurrentQueries,
	)
	return m
}

// WrappedQueryAPI wraps QueryAPI with metrics.
type WrappedQueryAPI struct {
	*QueryAPI
	metrics *QueryMetrics
}

// NewWrappedQueryAPI creates a QueryAPI with metrics.
func NewWrappedQueryAPI(querier storage.Querier, reg *prometheus.Registry) *WrappedQueryAPI {
	return &WrappedQueryAPI{
		QueryAPI: NewQueryAPI(querier, reg),
		metrics:  NewQueryMetrics(reg),
	}
}

// HandleQuery wraps the instant query handler with metrics.
func (w *WrappedQueryAPI) HandleQuery(wr http.ResponseWriter, r *http.Request) {
	w.metrics.TotalQueries.Inc()
	w.metrics.ConcurrentQueries.Inc()
	defer w.metrics.ConcurrentQueries.Dec()

	start := time.Now()
	
	// Capture response
	rec := &responseRecorder{ResponseWriter: wr, statusCode: http.StatusOK}
	
	w.QueryAPI.HandleQuery(rec, r)
	
	duration := time.Since(start)
	w.metrics.QueryDuration.Observe(duration.Seconds())
	
	if rec.statusCode >= 400 {
		w.metrics.FailedQueries.Inc()
	}
}

// HandleQueryRange wraps the range query handler with metrics.
func (w *WrappedQueryAPI) HandleQueryRange(wr http.ResponseWriter, r *http.Request) {
	w.metrics.TotalQueries.Inc()
	w.metrics.ConcurrentQueries.Inc()
	defer w.metrics.ConcurrentQueries.Dec()

	start := time.Now()
	
	rec := &responseRecorder{ResponseWriter: wr, statusCode: http.StatusOK}
	
	w.QueryAPI.HandleQueryRange(rec, r)
	
	duration := time.Since(start)
	w.metrics.QueryDuration.Observe(duration.Seconds())
	
	if rec.statusCode >= 400 {
		w.metrics.FailedQueries.Inc()
	}
}

// responseRecorder captures the status code.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// PrecompileQuery pre-compiles a query for faster repeated execution.
func (q *QueryAPI) PrecompileQuery(query string) (promql.Query, error) {
	expr, err := parser.ParseExpr(query)
	if err != nil {
		return nil, err
	}
	return q.engine.NewInstantQuery(q.querier, expr, time.Now()), nil
}

// PrecompileRangeQuery pre-compiles a range query.
func (q *QueryAPI) PrecompileRangeQuery(query string, start, end time.Time, step time.Duration) (promql.Query, error) {
	expr, err := parser.ParseExpr(query)
	if err != nil {
		return nil, err
	}
	return q.engine.NewRangeQuery(q.querier, expr, start, end, step), nil
}

// ExplainQuery returns the execution plan for a query.
func (q *QueryAPI) ExplainQuery(query string) (string, error) {
	expr, err := parser.ParseExpr(query)
	if err != nil {
		return "", err
	}
	
	// Create a query to get the explain plan
	ctx := context.Background()
	query := q.engine.NewInstantQuery(q.querier, expr, time.Now())
	
	// Execute and get explain
	result := query.Exec(ctx)
	if result.Err != nil {
		return "", result.Err
	}
	
	// The explain is available via the query's Explain method
	// Note: This requires prometheus v2.40+
	return query.Explain(), nil
}

// AnalyzeQuery analyzes a query for potential issues.
func (q *QueryAPI) AnalyzeQuery(query string) (*QueryAnalysis, error) {
	expr, err := parser.ParseExpr(query)
	if err != nil {
		return nil, err
	}
	
	analysis := &QueryAnalysis{
		Query: query,
	}
	
	// Walk the AST to analyze
	parser.Inspect(expr, func(node parser.Node, path []parser.Node) bool {
		switch n := node.(type) {
		case *parser.VectorSelector:
			analysis.HasVectorSelector = true
			if n.Name != "" {
				analysis.MetricNames = append(analysis.MetricNames, n.Name)
			}
			analysis.LabelMatchers = append(analysis.LabelMatchers, n.LabelMatchers...)
		case *parser.MatrixSelector:
			analysis.HasRangeVector = true
			analysis.Range = n.Range
		case *parser.Call:
			analysis.Functions = append(analysis.Functions, n.Func.Name)
			// Check for expensive functions
			switch n.Func.Name {
			case "rate", "irate", "increase":
				analysis.HasRateFunction = true
			case "histogram_quantile":
				analysis.HasHistogramQuantile = true
			case "topk", "bottomk":
				analysis.HasTopK = true
			case "sort", "sort_desc":
				analysis.HasSort = true
			}
		case *parser.AggregateExpr:
			analysis.HasAggregation = true
			analysis.AggregationOps = append(analysis.AggregationOps, n.Op.String())
		case *parser.BinaryExpr:
			analysis.HasBinaryOp = true
		case *parser.SubqueryExpr:
			analysis.HasSubquery = true
		}
		return true
	})
	
	// Estimate complexity
	analysis.Complexity = estimateComplexity(analysis)
	
	return analysis, nil
}

// QueryAnalysis contains analysis results for a query.
type QueryAnalysis struct {
	Query              string
	HasVectorSelector  bool
	HasRangeVector     bool
	HasRateFunction    bool
	HasHistogramQuantile bool
	HasTopK            bool
	HasSort            bool
	HasAggregation     bool
	HasBinaryOp        bool
	HasSubquery        bool
	MetricNames        []string
	LabelMatchers      []*labels.Matcher
	Functions          []string
	AggregationOps     []string
	Range              time.Duration
	Complexity         string // "low", "medium", "high"
}

// estimateComplexity estimates query complexity.
func estimateComplexity(a *QueryAnalysis) string {
	score := 0
	
	if a.HasRangeVector {
		score += 2
	}
	if a.HasRateFunction {
		score += 2
	}
	if a.HasHistogramQuantile {
		score += 3
	}
	if a.HasTopK {
		score += 2
	}
	if a.HasSort {
		score += 2
	}
	if a.HasAggregation {
		score += 1
	}
	if a.HasSubquery {
		score += 3
	}
	if len(a.Functions) > 3 {
		score += 2
	}
	if len(a.LabelMatchers) > 10 {
		score += 2
	}
	
	switch {
	case score >= 8:
		return "high"
	case score >= 4:
		return "medium"
	default:
		return "low"
	}
}

// TSDBQuerier wraps tsdb.DB to implement storage.Querier.
type TSDBQuerier struct {
	db *tsdb.DB
}

// NewTSDBQuerier creates a new TSDBQuerier.
func NewTSDBQuerier(db *tsdb.DB) *TSDBQuerier {
	return &TSDBQuerier{db: db}
}

// Querier returns a storage.Querier for the given time range.
func (t *TSDBQuerier) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return t.db.Querier(ctx, mint, maxt)
}

// Close closes the querier.
func (t *TSDBQuerier) Close() error {
	return t.db.Close()
}

// LabelNames returns all label names.
func (t *TSDBQuerier) LabelNames(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) {
	q, err := t.db.Querier(ctx, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer q.Close()
	return q.LabelNames(ctx, matchers...)
}

// LabelValues returns all values for a label name.
func (t *TSDBQuerier) LabelValues(ctx context.Context, name string, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) {
	q, err := t.db.Querier(ctx, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer q.Close()
	return q.LabelValues(ctx, name, matchers...)
}

// Select returns a SeriesSet for the given matchers.
func (t *TSDBQuerier) Select(ctx context.Context, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	q, err := t.db.Querier(ctx, hints.Start, hints.End)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	return q.Select(ctx, hints, matchers...)
}

// SeriesSet is a helper for testing.
type SeriesSet struct {
	series []storage.Series
	err    error
}

func (s *SeriesSet) Next() bool {
	return len(s.series) > 0
}

func (s *SeriesSet) At() storage.Series {
	if len(s.series) == 0 {
		return nil
	}
	ser := s.series[0]
	s.series = s.series[1:]
	return ser
}

func (s *SeriesSet) Err() error {
	return s.err
}

func (s *SeriesSet) Close() {
	s.series = nil
}
