package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/trip-manager-htwg/application//backend/social/internal/presigner/provider"
)

type MediaType string

const (
	MediaTypeAvatar   MediaType = "avatars"
	MediaTypeTrip     MediaType = "trips"
	MediaTypeLocation MediaType = "locations"
	MediaTypeActivity MediaType = "activity"
)

// ParseMediaType Saved for later if more image types are required
func ParseMediaType(s string) (MediaType, error) {
	switch s {
	case "avatar":
		return MediaTypeAvatar, nil
	case "trip":
		return MediaTypeTrip, nil
	case "location":
		return MediaTypeLocation, nil
	case "activity":
		return MediaTypeActivity, nil
	default:
		return "", fmt.Errorf("invalid media type: %q", s)
	}
}

type Service interface {
	PrepareUpload(ctx context.Context, userId string, mediaType MediaType, fileName string) (UploadTicket, error)
	GetDownloadURL(ctx context.Context, key string) (string, error)
	ConfirmUpload(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error
}

type ServiceImpl struct {
	storage provider.Storage
	ttl     time.Duration
	cache   *URLCache
}

func NewService(stor provider.Storage, ttl time.Duration) *ServiceImpl {
	return &ServiceImpl{
		storage: stor,
		ttl:     ttl,
		cache:   NewURLCache(ttl / 2), // Cache TTL is half of the URL TTL
	}
}

type UploadTicket struct {
	UploadURL string        // Short-lived PUT-URL
	Key       string        // Object-Key for DB
	ExpiresIn time.Duration // Expiration time
}

// PrepareUpload generates Object-Key + Upload-URL.
func (ms *ServiceImpl) PrepareUpload(ctx context.Context, userID string, mediaType MediaType, fileName string) (UploadTicket, error) {
	key := buildKey(mediaType, userID, fileName)

	url, err := ms.storage.GetUploadURL(ctx, key)
	if err != nil {
		return UploadTicket{}, fmt.Errorf("upload url: %w", err)
	}

	return UploadTicket{UploadURL: url, Key: key, ExpiresIn: ms.ttl}, nil
}

// GetDownloadURL returns short-lived GET-URL with caching.
func (ms *ServiceImpl) GetDownloadURL(ctx context.Context, key string) (string, error) {
	// Check cache first
	if url, ok := ms.cache.Get(key); ok {
		return url, nil
	}

	// Cache miss: regenerate
	url, err := ms.storage.GetDownloadURL(ctx, key)
	if err != nil {
		return "", fmt.Errorf("download url: %w", err)
	}

	// Store in cache
	ms.cache.Set(key, url)

	return url, nil
}

// ConfirmUpload verifies successful upload.
func (ms *ServiceImpl) ConfirmUpload(ctx context.Context, key string) (bool, error) {
	return ms.storage.Exists(ctx, key)
}

// Delete removes an object.
func (ms *ServiceImpl) Delete(ctx context.Context, key string) error {
	return ms.storage.Delete(ctx, key)
}

// buildKey collision free object-key.
func buildKey(mType MediaType, userID string, originalFileName string) string {
	ext := getFileExtension(originalFileName)

	if mType == MediaTypeAvatar {
		// Avatar pro User ist singulär — User-ID als Key reicht.
		return fmt.Sprintf("avatars/%s%s", userID, ext)
	}

	// Alle anderen Media-Types: UUID pro Upload, verhindert Kollisionen.
	return fmt.Sprintf("%s/%s/%s%s", mType, userID, uuid.NewString(), ext)
}

func getFileExtension(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".heic":
		return ext
	default:
		return ".jpg"
	}
}
