package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/trip-manager-htwg/application/backend/travel-info/internal/fetcher"
)

const (
	warningPrefix    = "warning:"
	weatherPrefix    = "weather:"
	defaultTravelTTL = 25 * time.Hour
)

type Cache struct {
	client     *redis.Client
	weatherTTL time.Duration
}

type CachedWarning struct {
	CountryCode    string               `json:"countryCode"`
	CountryName    string               `json:"countryName"`
	Level          fetcher.WarningLevel `json:"level"`
	Warning        bool                 `json:"warning"`
	PartialWarning bool                 `json:"partialWarning"`
	UpdatedAt      time.Time            `json:"updatedAt"`
}

// NewCache initialisiert EINEN Client-Pool für beide Services
func NewCache(redisURL string, weatherTTLHours int) (*Cache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing Redis URL %s: %w", redisURL, err)
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("error connecting to Redis %s: %w", redisURL, err)
	}

	return &Cache{
		client:     client,
		weatherTTL: time.Duration(weatherTTLHours) * time.Hour,
	}, nil
}

func (c *Cache) Close() error {
	return c.client.Close()
}

// ==========================================
// TRAVEL WARNING METHODS
// ==========================================

func (c *Cache) SetWarning(ctx context.Context, w *CachedWarning) error {
	data, err := json.Marshal(w)
	if err != nil {
		return fmt.Errorf("error serializing cached warning: %w", err)
	}

	key := warningPrefix + w.CountryCode
	return c.client.Set(ctx, key, data, defaultTravelTTL).Err()
}

func (c *Cache) GetWarning(ctx context.Context, countryCode string) (*CachedWarning, error) {
	key := warningPrefix + countryCode
	data, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error getting cached warning: %w", err)
	}

	var warning CachedWarning
	if err := json.Unmarshal(data, &warning); err != nil {
		return nil, fmt.Errorf("error deserializing cached warning: %w", err)
	}

	return &warning, nil
}

func (c *Cache) SetWarningBatch(ctx context.Context, warnings []*CachedWarning) error {
	pipe := c.client.Pipeline()
	for _, warning := range warnings {
		data, err := json.Marshal(warning)
		if err != nil {
			continue
		}
		key := warningPrefix + warning.CountryCode
		pipe.Set(ctx, key, data, defaultTravelTTL)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// ==========================================
// WEATHER INFO METHODS
// ==========================================

func weatherCacheKey(lat, lng float64) string {
	return fmt.Sprintf("%s%.2f:%.2f", weatherPrefix, lat, lng)
}

func (c *Cache) SetWeather(ctx context.Context, weather *fetcher.WeatherResponse) error {
	data, err := json.Marshal(weather)
	if err != nil {
		return fmt.Errorf("marshal weather: %w", err)
	}
	return c.client.Set(ctx, weatherCacheKey(weather.Latitude, weather.Longitude), data, c.weatherTTL).Err()
}

func (c *Cache) GetWeather(ctx context.Context, lat, lng float64) (*fetcher.WeatherResponse, error) {
	data, err := c.client.Get(ctx, weatherCacheKey(lat, lng)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil // Cache Miss
	}
	if err != nil {
		return nil, fmt.Errorf("get weather: %w", err)
	}

	var weather fetcher.WeatherResponse
	if err := json.Unmarshal(data, &weather); err != nil {
		return nil, fmt.Errorf("unmarshal weather: %w", err)
	}
	return &weather, nil
}
