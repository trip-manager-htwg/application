package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type S3Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	region    string
	publicURL string
	ttl       time.Duration
}

type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string
	PublicURL string
	AccessKey string
	SecretKey string
	TTL       time.Duration // Lifetime für Upload/Download URLs, default 15min
}

func NewS3Storage(cfg S3Config) (*S3Storage, error) {
	println("NewS3Storage called with cache: ", cfg.Endpoint, cfg.PublicURL, cfg.Bucket, cfg.Region)
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("bucket name is required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.TTL == 0 {
		cfg.TTL = 15 * time.Minute
	}

	var opts []func(*awsconfig.LoadOptions) error
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey, cfg.SecretKey, "",
		)))
	}
	opts = append(opts, awsconfig.WithRegion(cfg.Region))

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS cache: %w", err)
	}

	// Internal Client for Server-Side Ops (Read, Write, Delete)
	internalClient := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})

	// Presign-Client uses PublicURL for generating URLs that are accessible from outside the cluster
	presignSourceClient := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.PublicURL)
		o.UsePathStyle = true
	})

	return &S3Storage{
		client:    internalClient,
		presigner: s3.NewPresignClient(presignSourceClient),
		bucket:    cfg.Bucket,
		region:    cfg.Region,
		publicURL: cfg.PublicURL,
		ttl:       cfg.TTL,
	}, nil
}

// GetUploadURL return short-lived upload url.
func (s *S3Storage) GetUploadURL(ctx context.Context, fileName string) (string, error) {
	result, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fileName),
	}, s3.WithPresignExpires(s.ttl))
	if err != nil {
		return "", fmt.Errorf("presign put %q: %w", fileName, err)
	}
	return result.URL, nil
}

// GetDownloadURL return short-lived download url.
func (s *S3Storage) GetDownloadURL(ctx context.Context, fileName string) (string, error) {
	result, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fileName),
	}, s3.WithPresignExpires(s.ttl))
	if err != nil {
		return "", fmt.Errorf("presign get %q: %w", fileName, err)
	}
	return result.URL, nil
}

// ReadFile read file from backend
func (s *S3Storage) ReadFile(ctx context.Context, fileName string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fileName),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %q: %w", fileName, err)
	}
	return result.Body, nil
}

// WriteFile write a file from backend
func (s *S3Storage) WriteFile(ctx context.Context, fileName string, file io.Reader) error {
	uploader := transfermanager.New(s.client)
	_, err := uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fileName),
		Body:   file,
	})
	if err != nil {
		return fmt.Errorf("upload %q: %w", fileName, err)
	}
	return nil
}

// Delete removes object
func (s *S3Storage) Delete(ctx context.Context, fileName string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fileName),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchKey" {
			return nil
		}
		return fmt.Errorf("delete %q: %w", fileName, err)
	}
	return nil
}

// Exists check if file exists
func (s *S3Storage) Exists(ctx context.Context, fileName string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fileName),
	})
	if err != nil {
		var nsk *types.NotFound
		if errors.As(err, &nsk) {
			return false, nil
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
			return false, nil
		}
		return false, fmt.Errorf("head object %q: %w", fileName, err)
	}
	return true, nil
}
