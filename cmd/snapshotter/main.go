package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/prometheus/tsdb"
	"go.uber.org/zap"
)

var (
	tsdbDataDir     = flag.String("tsdb-data-dir", "/data/tsdb", "TSDB data directory")
	snapshotInterval = flag.Duration("snapshot-interval", 6*time.Hour, "Snapshot interval")
	s3Bucket        = flag.String("s3-bucket", "", "S3 bucket for snapshots (required)")
	s3Prefix        = flag.String("s3-prefix", "", "S3 key prefix")
	awsRegion       = flag.String("aws-region", "us-east-1", "AWS region")
	retentionHours  = flag.Int("retention-hours", 24, "Local snapshot retention in hours")
	logLevel        = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	if *s3Bucket == "" {
		fmt.Fprintln(os.Stderr, "Error: -s3-bucket is required")
		os.Exit(1)
	}

	logger, err := newLogger(*logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting TSDB snapshotter",
		zap.String("data_dir", *tsdbDataDir),
		zap.Duration("interval", *snapshotInterval),
		zap.String("s3_bucket", *s3Bucket),
		zap.String("s3_prefix", *s3Prefix),
	)

	// Initialize AWS config
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(*awsRegion),
	)
	if err != nil {
		logger.Fatal("Failed to load AWS config", zap.Error(err))
	}

	s3Client := s3.NewFromConfig(cfg)

	// Create snapshot directory
	snapshotDir := filepath.Join(*tsdbDataDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		logger.Fatal("Failed to create snapshot directory", zap.Error(err))
	}

	// Open TSDB read-only for snapshots
	db, err := tsdb.Open(*tsdbDataDir, logger, nil, &tsdb.Options{
		RetentionDuration: 0, // No retention for snapshotter
		NoLockfile:        true,
	})
	if err != nil {
		logger.Fatal("Failed to open TSDB", zap.Error(err))
	}
	defer db.Close()

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		logger.Info("Shutdown signal received")
		cancel()
	}()

	// Run snapshot loop
	ticker := time.NewTicker(*snapshotInterval)
	defer ticker.Stop()

	// Initial snapshot
	if err := runSnapshot(ctx, logger, db, s3Client, snapshotDir, *s3Bucket, *s3Prefix); err != nil {
		logger.Error("Initial snapshot failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("Snapshotter shutting down")
			return
		case <-ticker.C:
			if err := runSnapshot(ctx, logger, db, s3Client, snapshotDir, *s3Bucket, *s3Prefix); err != nil {
				logger.Error("Snapshot failed", zap.Error(err))
			}
			// Cleanup old local snapshots
			if err := cleanupOldSnapshots(logger, snapshotDir, *retentionHours); err != nil {
				logger.Error("Cleanup failed", zap.Error(err))
			}
		}
	}
}

func runSnapshot(ctx context.Context, logger *zap.Logger, db *tsdb.DB, s3Client *s3.Client, snapshotDir, bucket, prefix string) error {
	start := time.Now()
	logger.Info("Starting TSDB snapshot")

	// Create timestamped snapshot directory
	timestamp := time.Now().UTC().Format("20060102-150405")
	snapDir := filepath.Join(snapshotDir, timestamp)

	// Create snapshot (hardlinks files, very fast)
	if err := db.Snapshot(snapDir); err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}

	logger.Info("Snapshot created", zap.String("dir", snapDir), zap.Duration("duration", time.Since(start)))

	// Create tarball with zstd compression
	tarballPath := filepath.Join(snapshotDir, fmt.Sprintf("%s.tar.zst", timestamp))
	if err := createTarballZstd(snapDir, tarballPath); err != nil {
		return fmt.Errorf("create tarball: %w", err)
	}

	logger.Info("Tarball created", zap.String("path", tarballPath), zap.Duration("duration", time.Since(start)))

	// Upload to S3
	s3Key := prefix + filepath.Base(tarballPath)
	if err := uploadToS3(ctx, logger, s3Client, bucket, s3Key, tarballPath); err != nil {
		return fmt.Errorf("upload to S3: %w", err)
	}

	logger.Info("Snapshot uploaded to S3",
		zap.String("bucket", bucket),
		zap.String("key", s3Key),
		zap.Duration("duration", time.Since(start)),
	)

	// Cleanup local tarball and snapshot directory
	os.Remove(tarballPath)
	os.RemoveAll(snapDir)

	return nil
}

func createTarballZstd(srcDir, destPath string) error {
	// Use system tar with zstd for best performance
	// tar -I zstd -cf destPath -C srcDir .
	// For pure Go implementation, would use github.com/klauspost/compress/zstd + archive/tar
	// This is a simplified version - in production use a proper archiver
	return nil // Placeholder
}

func uploadToS3(ctx context.Context, logger *zap.Logger, client *s3.Client, bucket, key, filePath string) error {
	// Simplified - in production use s3manager.Uploader for multipart upload
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	return err
}

func cleanupOldSnapshots(logger *zap.Logger, snapshotDir string, retentionHours int) error {
	cutoff := time.Now().Add(-time.Duration(retentionHours) * time.Hour)
	
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				path := filepath.Join(snapshotDir, entry.Name())
				logger.Info("Removing old snapshot", zap.String("path", path))
				os.RemoveAll(path)
			}
		}
	}

	return nil
}

func newLogger(level string) (*zap.Logger, error) {
	var cfg zap.Config
	if level == "debug" {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	
	lvl, err := zap.ParseAtomicLevel(level)
	if err != nil {
		return nil, err
	}
	cfg.Level = lvl
	
	return cfg.Build()
}
