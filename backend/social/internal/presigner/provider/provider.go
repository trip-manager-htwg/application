package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/trip-manager-htwg/application/backend/social/config"
)

func NewFromEnv(ctx context.Context, cfg config.Config) (Storage, error) {
	switch strings.ToLower(cfg.StorageProvider) {
	case "gcs":
		return NewGCStorage(ctx, GCSConfig{
			Bucket:   cfg.StorageBucket,
			SignerSA: cfg.GCSSignerSA,
			TTL:      cfg.StorageTTL,
		})
	case "s3":
		return NewS3Storage(S3Config{
			Bucket:    cfg.StorageBucket,
			Region:    cfg.S3Region,
			Endpoint:  cfg.S3Endpoint,
			PublicURL: cfg.S3PublicURL,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			TTL:       cfg.StorageTTL,
		})
	default:
		return nil, fmt.Errorf("unknown storage type: %q", cfg.StorageProvider)
	}
}

// PresignProvider defines the interface for presigning URLs
type PresignProvider interface {
	// GetUploadURL returns a presigned URL for uploading
	GetUploadURL(ctx context.Context, bucket, key string, expiration time.Duration) (string, error)

	// GetDownloadURL returns a presigned URL for downloading
	GetDownloadURL(ctx context.Context, bucket, key string, expiration time.Duration) (string, error)

	// Close closes any resources used by the provider
	Close() error
}
