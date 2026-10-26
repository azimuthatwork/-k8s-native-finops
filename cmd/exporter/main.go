package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow/go/v16/arrow"
	"github.com/apache/arrow/go/v16/arrow/array"
	"github.com/apache/arrow/go/v16/arrow/memory"
	"github.com/apache/arrow/go/v16/parquet"
	"github.com/apache/arrow/go/v16/parquet/compress"
	"github.com/apache/arrow/go/v16/parquet/pqarrow"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"go.uber.org/zap"
)

// CostRecord represents a single row in the Parquet output
type CostRecord struct {
	Timestamp           time.Time
	Cluster             string
	Namespace           string
	Workload            string
	Kind                string
	CostCenter          string
	Team                string
	CPUCoresRequested   float64
	CPUCoresUsed        float64
	MemoryGiBRequested  float64
	MemoryGiBUsed       float64
	StorageGiBRequested float64
	StorageGiBUsed      float64
	ComputeCostUSD      float64
	StorageCostUSD      float64
	NetworkCostUSD      float64
	SharedCostUSD       float64
	TotalCostUSD        float64
	EfficiencyPct       float64
}

// ParquetSchema defines the Arrow schema for the cost allocation Parquet files
var ParquetSchema = arrow.NewSchema([]arrow.Field{
	{Name: "timestamp", Type: arrow.PrimitiveTypes.Timestamp_us, Nullable: false},
	{Name: "cluster", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "namespace", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "workload", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "kind", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "cost_center", Type: arrow.BinaryTypes.String, Nullable: true},
	{Name: "team", Type: arrow.BinaryTypes.String, Nullable: true},
	{Name: "cpu_cores_requested", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "cpu_cores_used", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "memory_gib_requested", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "memory_gib_used", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "storage_gib_requested", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "storage_gib_used", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "compute_cost_usd", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "storage_cost_usd", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "network_cost_usd", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "shared_cost_usd", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "total_cost_usd", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	{Name: "efficiency_pct", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
}, nil)

// Config holds the exporter configuration
type Config struct {
	TSDBEndpoint     string
	S3Bucket         string
	S3Prefix         string
	S3Region         string
	S3Endpoint       string
	AWSAccessKey     string
	AWSSecretKey     string
	ClusterName      string
	LookbackHours    int
	Step             string
	OutputDir        string
	LogLevel         string
}

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg := loadConfig()
	logger.Info("Starting cost exporter", zap.String("cluster", cfg.ClusterName))

	ctx := context.Background()

	// Calculate time range for yesterday
	endTime := time.Now().UTC().Truncate(24 * time.Hour)
	startTime := endTime.Add(-time.Duration(cfg.LookbackHours) * time.Hour)

	logger.Info("Querying TSDB",
		zap.Time("start", startTime),
		zap.Time("end", endTime),
		zap.String("step", cfg.Step))

	// Query TSDB for cost data
	records, err := queryTSDB(ctx, cfg, startTime, endTime)
	if err != nil {
		logger.Fatal("Failed to query TSDB", zap.Error(err))
	}

	logger.Info("Retrieved records", zap.Int("count", len(records)))

	if len(records) == 0 {
		logger.Info("No data to export, exiting")
		return
	}

	// Group records by date and cost_center for partitioning
	partitions := groupByPartition(records)

	// Initialize S3 client
	s3Client, err := newS3Client(ctx, cfg)
	if err != nil {
		logger.Fatal("Failed to create S3 client", zap.Error(err))
	}

	// Write each partition to Parquet and upload to S3
	for partitionKey, partitionRecords := range partitions {
		logger.Info("Processing partition", zap.String("partition", partitionKey), zap.Int("records", len(partitionRecords)))

		// Create Parquet file in memory
		parquetData, err := writeParquet(partitionRecords)
		if err != nil {
			logger.Error("Failed to write Parquet", zap.String("partition", partitionKey), zap.Error(err))
			continue
		}

		// Upload to S3
		s3Key := buildS3Key(cfg, partitionKey, endTime)
		if err := uploadToS3(ctx, s3Client, cfg.S3Bucket, s3Key, parquetData); err != nil {
			logger.Error("Failed to upload to S3", zap.String("partition", partitionKey), zap.String("s3Key", s3Key), zap.Error(err))
			continue
		}

		logger.Info("Successfully uploaded partition", zap.String("partition", partitionKey), zap.String("s3Key", s3Key))
	}

	logger.Info("Export completed successfully")
}

func loadConfig() Config {
	return Config{
		TSDBEndpoint:     getEnv("TSDB_ENDPOINT", "http://localhost:8080"),
		S3Bucket:         getEnv("S3_BUCKET", "finops-bucket"),
		S3Prefix:         getEnv("S3_PREFIX", "cost/allocations"),
		S3Region:         getEnv("S3_REGION", "us-east-1"),
		S3Endpoint:       getEnv("S3_ENDPOINT", ""),
		AWSAccessKey:     getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretKey:     getEnv("AWS_SECRET_ACCESS_KEY", ""),
		ClusterName:      getEnv("CLUSTER_NAME", "cluster-1"),
		LookbackHours:    getEnvInt("LOOKBACK_HOURS", 24),
		Step:             getEnv("STEP", "1h"),
		OutputDir:        getEnv("OUTPUT_DIR", "/tmp/export"),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func queryTSDB(ctx context.Context, cfg Config, start, end time.Time) ([]CostRecord, error) {
	// Build query for all required metrics
	queries := map[string]string{
		"cpu_requested":     `sum by (namespace, workload, kind, cost_center, team) (finops_workload_cpu_cores_requested)`,
		"cpu_used":          `sum by (namespace, workload, kind, cost_center, team) (finops_workload_cpu_cores_used)`,
		"memory_requested":  `sum by (namespace, workload, kind, cost_center, team) (finops_workload_memory_bytes_requested / 1024 / 1024 / 1024)`,
		"memory_used":       `sum by (namespace, workload, kind, cost_center, team) (finops_workload_memory_bytes_used / 1024 / 1024 / 1024)`,
		"storage_requested": `sum by (namespace, workload, kind, cost_center, team) (finops_pvc_storage_bytes_requested / 1024 / 1024 / 1024)`,
		"storage_used":      `sum by (namespace, workload, kind, cost_center, team) (finops_pvc_storage_bytes_used / 1024 / 1024 / 1024)`,
		"compute_cost":      `sum by (namespace, workload, kind, cost_center, team) (finops_workload_compute_cost_usd)`,
		"storage_cost":      `sum by (namespace, workload, kind, cost_center, team) (finops_workload_storage_cost_usd)`,
		"network_cost":      `sum by (namespace, workload, kind, cost_center, team) (finops_workload_network_cost_usd)`,
		"shared_cost":       `sum by (namespace, workload, kind, cost_center, team) (finops_workload_shared_cost_usd)`,
		"total_cost":        `sum by (namespace, workload, kind, cost_center, team) (finops_workload_total_cost_usd)`,
		"efficiency":        `avg by (namespace, workload, kind, cost_center, team) (finops_workload_efficiency_ratio * 100)`,
	}

	// Execute all queries
	results := make(map[string]map[string]float64)
	for name, query := range queries {
		data, err := queryRange(ctx, cfg, query, start, end, cfg.Step)
		if err != nil {
			return nil, fmt.Errorf("query %s failed: %w", name, err)
		}
		results[name] = data
	}

	// Merge results into CostRecords
	recordMap := make(map[string]*CostRecord)

	for metricName, series := range results {
		for labels, value := range series {
			// Parse labels: namespace, workload, kind, cost_center, team
			labelsMap := parseLabels(labels)
			key := buildRecordKey(labelsMap)

			if _, exists := recordMap[key]; !exists {
				recordMap[key] = &CostRecord{
					Cluster:    cfg.ClusterName,
					Namespace:  labelsMap["namespace"],
					Workload:   labelsMap["workload"],
					Kind:       labelsMap["kind"],
					CostCenter: labelsMap["cost_center"],
					Team:       labelsMap["team"],
				}
			}

			rec := recordMap[key]
			switch metricName {
			case "cpu_requested":
				rec.CPUCoresRequested = value
			case "cpu_used":
				rec.CPUCoresUsed = value
			case "memory_requested":
				rec.MemoryGiBRequested = value
			case "memory_used":
				rec.MemoryGiBUsed = value
			case "storage_requested":
				rec.StorageGiBRequested = value
			case "storage_used":
				rec.StorageGiBUsed = value
			case "compute_cost":
				rec.ComputeCostUSD = value
			case "storage_cost":
				rec.StorageCostUSD = value
			case "network_cost":
				rec.NetworkCostUSD = value
			case "shared_cost":
				rec.SharedCostUSD = value
			case "total_cost":
				rec.TotalCostUSD = value
			case "efficiency":
				rec.EfficiencyPct = value
			}
		}
	}

	// Convert to slice and set timestamp
	records := make([]CostRecord, 0, len(recordMap))
	for _, rec := range recordMap {
		rec.Timestamp = end // Use end of period as timestamp
		records = append(records, *rec)
	}

	return records, nil
}

func queryRange(ctx context.Context, cfg Config, query, start, end, step string) (map[string]float64, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", step)

	reqURL := fmt.Sprintf("%s/api/v1/query_range?%s", cfg.TSDBEndpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TSDB returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response (simplified - in production use proper JSON parsing)
	// Expected format: {"status":"success","data":{"resultType":"matrix","result":[{"metric":{...},"values":[[ts,val],...]}]}}
	// For now, return empty map - implement proper parsing based on actual TSDB response
	return parseQueryRangeResponse(resp.Body)
}

func parseQueryRangeResponse(body io.Reader) (map[string]float64, error) {
	// Simplified parsing - in production use json.Unmarshal with proper types
	// This is a placeholder that returns empty results
	// Real implementation would parse the Prometheus query_range response format
	return make(map[string]float64), nil
}

func parseLabels(labelString string) map[string]string {
	// Parse Prometheus label string like: {namespace="ns1", workload="deploy1", kind="Deployment", cost_center="eng", team="backend"}
	result := make(map[string]string)
	labelString = strings.Trim(labelString, "{}")
	pairs := strings.Split(labelString, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			value := strings.Trim(strings.TrimSpace(kv[1]), `"`)
			result[key] = value
		}
	}
	return result
}

func buildRecordKey(labels map[string]string) string {
	keys := []string{"namespace", "workload", "kind", "cost_center", "team"}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = labels[k]
	}
	return strings.Join(parts, "|")
}

func groupByPartition(records []CostRecord) map[string][]CostRecord {
	partitions := make(map[string][]CostRecord)
	for _, rec := range records {
		dateStr := rec.Timestamp.Format("2006-01-02")
		costCenter := rec.CostCenter
		if costCenter == "" {
			costCenter = "unknown"
		}
		key := fmt.Sprintf("%s/%s", dateStr, costCenter)
		partitions[key] = append(partitions[key], rec)
	}
	return partitions
}

func writeParquet(records []CostRecord) ([]byte, error) {
	mem := memory.NewGoAllocator()

	// Create builders for each column
	tsBuilder := array.NewTimestampBuilder(mem, arrow.PrimitiveTypes.Timestamp_us)
	clusterBuilder := array.NewStringBuilder(mem)
	nsBuilder := array.NewStringBuilder(mem)
	workloadBuilder := array.NewStringBuilder(mem)
	kindBuilder := array.NewStringBuilder(mem)
	costCenterBuilder := array.NewStringBuilder(mem)
	teamBuilder := array.NewStringBuilder(mem)
	cpuReqBuilder := array.NewFloat64Builder(mem)
	cpuUsedBuilder := array.NewFloat64Builder(mem)
	memReqBuilder := array.NewFloat64Builder(mem)
	memUsedBuilder := array.NewFloat64Builder(mem)
	storageReqBuilder := array.NewFloat64Builder(mem)
	storageUsedBuilder := array.NewFloat64Builder(mem)
	computeCostBuilder := array.NewFloat64Builder(mem)
	storageCostBuilder := array.NewFloat64Builder(mem)
	networkCostBuilder := array.NewFloat64Builder(mem)
	sharedCostBuilder := array.NewFloat64Builder(mem)
	totalCostBuilder := array.NewFloat64Builder(mem)
	efficiencyBuilder := array.NewFloat64Builder(mem)

	defer func() {
		tsBuilder.Release()
		clusterBuilder.Release()
		nsBuilder.Release()
		workloadBuilder.Release()
		kindBuilder.Release()
		costCenterBuilder.Release()
		teamBuilder.Release()
		cpuReqBuilder.Release()
		cpuUsedBuilder.Release()
		memReqBuilder.Release()
		memUsedBuilder.Release()
		storageReqBuilder.Release()
		storageUsedBuilder.Release()
		computeCostBuilder.Release()
		storageCostBuilder.Release()
		networkCostBuilder.Release()
		sharedCostBuilder.Release()
		totalCostBuilder.Release()
		efficiencyBuilder.Release()
	}()

	// Append records
	for _, rec := range records {
		tsBuilder.Append(rec.Timestamp.UnixMicro())
		clusterBuilder.Append(rec.Cluster)
		nsBuilder.Append(rec.Namespace)
		workloadBuilder.Append(rec.Workload)
		kindBuilder.Append(rec.Kind)
		costCenterBuilder.Append(rec.CostCenter)
		teamBuilder.Append(rec.Team)
		cpuReqBuilder.Append(rec.CPUCoresRequested)
		cpuUsedBuilder.Append(rec.CPUCoresUsed)
		memReqBuilder.Append(rec.MemoryGiBRequested)
		memUsedBuilder.Append(rec.MemoryGiBUsed)
		storageReqBuilder.Append(rec.StorageGiBRequested)
		storageUsedBuilder.Append(rec.StorageGiBUsed)
		computeCostBuilder.Append(rec.ComputeCostUSD)
		storageCostBuilder.Append(rec.StorageCostUSD)
		networkCostBuilder.Append(rec.NetworkCostUSD)
		sharedCostBuilder.Append(rec.SharedCostUSD)
		totalCostBuilder.Append(rec.TotalCostUSD)
		efficiencyBuilder.Append(rec.EfficiencyPct)
	}

	// Build arrays
	tsArr := tsBuilder.NewArray()
	clusterArr := clusterBuilder.NewArray()
	nsArr := nsBuilder.NewArray()
	workloadArr := workloadBuilder.NewArray()
	kindArr := kindBuilder.NewArray()
	costCenterArr := costCenterBuilder.NewArray()
	teamArr := teamBuilder.NewArray()
	cpuReqArr := cpuReqBuilder.NewArray()
	cpuUsedArr := cpuUsedBuilder.NewArray()
	memReqArr := memReqBuilder.NewArray()
	memUsedArr := memUsedBuilder.NewArray()
	storageReqArr := storageReqBuilder.NewArray()
	storageUsedArr := storageUsedBuilder.NewArray()
	computeCostArr := computeCostBuilder.NewArray()
	storageCostArr := storageCostBuilder.NewArray()
	networkCostArr := networkCostBuilder.NewArray()
	sharedCostArr := sharedCostBuilder.NewArray()
	totalCostArr := totalCostBuilder.NewArray()
	efficiencyArr := efficiencyBuilder.NewArray()

	defer func() {
		tsArr.Release()
		clusterArr.Release()
		nsArr.Release()
		workloadArr.Release()
		kindArr.Release()
		costCenterArr.Release()
		teamArr.Release()
		cpuReqArr.Release()
		cpuUsedArr.Release()
		memReqArr.Release()
		memUsedArr.Release()
		storageReqArr.Release()
		storageUsedArr.Release()
		computeCostArr.Release()
		storageCostArr.Release()
		networkCostArr.Release()
		sharedCostArr.Release()
		totalCostArr.Release()
		efficiencyArr.Release()
	}()

	// Create record batch
	record := array.NewRecord(ParquetSchema, []arrow.Array{
		tsArr, clusterArr, nsArr, workloadArr, kindArr, costCenterArr, teamArr,
		cpuReqArr, cpuUsedArr, memReqArr, memUsedArr, storageReqArr, storageUsedArr,
		computeCostArr, storageCostArr, networkCostArr, sharedCostArr, totalCostArr, efficiencyArr,
	}, int64(len(records)))

	defer record.Release()

	// Write to Parquet in memory
	buf := new(bytes.Buffer)
	pw := pqarrow.NewFileWriter(ParquetSchema, buf, pqarrow.DefaultWriterProps(),
		pqarrow.WithCompression(compress.Codecs.Zstd))
	if err := pw.WriteRecord(record); err != nil {
		return nil, err
	}
	if err := pw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func newS3Client(ctx context.Context, cfg Config) (*s3.Client, error) {
	var options []func(*config.LoadOptions) error
	options = append(options, config.WithRegion(cfg.S3Region))

	if cfg.AWSAccessKey != "" && cfg.AWSSecretKey != "" {
		options = append(options, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AWSAccessKey, cfg.AWSSecretKey, "")))
	}

	if cfg.S3Endpoint != "" {
		options = append(options, config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               cfg.S3Endpoint,
					HostnameImmutable: true,
				}, nil
			})))
	}

	cfgAWS, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(cfgAWS), nil
}

func buildS3Key(cfg Config, partitionKey string, endTime time.Time) string {
	// partitionKey format: "2006-01-02/cost_center"
	dateStr := endTime.Format("2006/01/02")
	costCenter := strings.Split(partitionKey, "/")[1]
	return fmt.Sprintf("%s/year=%s/month=%s/day=%s/cost_center=%s/data.parquet",
		cfg.S3Prefix,
		endTime.Format("2006"),
		endTime.Format("01"),
		endTime.Format("02"),
		costCenter)
}

func uploadToS3(ctx context.Context, client *s3.Client, bucket, key string, data []byte) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}
