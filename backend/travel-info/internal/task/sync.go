package task

import (
	"context"
	"log"
	"time"

	"github.com/trip-manager-htwg/application/backend/travel-info/config"
	"github.com/trip-manager-htwg/application/backend/travel-info/internal/cache"
	"github.com/trip-manager-htwg/application/backend/travel-info/internal/fetcher"
)

func RunSyncTasks(ctx context.Context, sharedCache *cache.Cache, cfg *config.Config) {
	log.Println("--- Starting Travel Warnings Sync ---")
	travelClient := fetcher.NewWarningClient(cfg.TravelWarningUrl)

	startTravel := time.Now()
	warnings, err := travelClient.FetchAll(ctx)
	if err != nil {
		log.Printf("ERROR: failed to fetch travel warnings: %v", err)
		return
	}

	var cachedWarnings []*cache.CachedWarning
	for _, w := range warnings {
		cachedWarnings = append(cachedWarnings, &cache.CachedWarning{
			CountryCode:    w.CountryCode,
			CountryName:    w.CountryName,
			Level:          w.Level(),
			Warning:        w.Warning,
			PartialWarning: w.PartialWarning,
			UpdatedAt:      time.Now(),
		})
	}

	if err := sharedCache.SetWarningBatch(ctx, cachedWarnings); err != nil {
		log.Printf("ERROR: failed to cache travel warnings: %v", err)
	} else {
		log.Printf("SUCCESS: Fetched and cached %d travel warnings in %s", len(warnings), time.Since(startTravel))
	}

	log.Println("--- Sync tasks completed ---")
}
