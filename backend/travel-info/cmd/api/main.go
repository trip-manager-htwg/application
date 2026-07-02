package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	sharedotel "otel"
	"syscall"
	"time"

	"github.com/trip-manager-htwg/application//backend/shared/authclient"
	"github.com/trip-manager-htwg/application//backend/shared/middleware"
	"github.com/trip-manager-htwg/application//backend/travel-info/internal/task"

	"github.com/trip-manager-htwg/application//backend/travel-info/config"
	"github.com/trip-manager-htwg/application//backend/travel-info/internal/cache"
	"github.com/trip-manager-htwg/application//backend/travel-info/internal/fetcher"
	"github.com/trip-manager-htwg/application//backend/travel-info/internal/handler"
)

func main() {
	// FIX 1: Erstellt einen Context, der bei SIGINT/SIGTERM automatisch gecancelt wird
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	otelProvider, err := sharedotel.New(ctx, "travel-and-weather-info", cfg.OTELCollectorEndpoint)
	if err != nil {
		log.Printf("warn: failed to initialize otel: %v", err)
	}
	var metrics *sharedotel.ServiceMetrics
	if otelProvider != nil {
		defer otelProvider.Shutdown(context.Background()) // Nutzen von Background beim harten Shutdown
		metrics, _ = sharedotel.NewServiceMetrics(otelProvider.Meter, "travel-and-weather-info")
	}

	sharedCache, err := cache.NewCache(cfg.RedisUrl, cfg.WeatherCacheTTLHours)
	if err != nil {
		log.Fatal(err)
	}
	defer sharedCache.Close()

	meteoClient := fetcher.NewOpenMeteoClient(cfg.WeatherAPIUrl, cfg.WeatherForecastDays)

	warningHandler := handler.NewWarningHandler(sharedCache)
	weatherHandler := handler.NewWeatherHandler(sharedCache, meteoClient)

	// Router
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	})

	mux.HandleFunc("GET /warning/{countryCode}", warningHandler.GetWarning)
	mux.HandleFunc("GET /weather", weatherHandler.GetWeather)

	corsConfig := middleware.DefaultCORSConfig()
	if len(cfg.CORSAllowedOrigins) == 0 {
		log.Fatalf("No allowed origin configured")
	}
	corsConfig.AllowedOrigins = cfg.CORSAllowedOrigins

	authClient := authclient.NewClient(cfg.AuthServiceURL)

	// Initialer Sync
	go func() {
		log.Println("Running initial background sync...")
		task.RunSyncTasks(ctx, sharedCache, cfg, meteoClient)
	}()

	// Periodischer Sync
	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("Stopping periodic background sync ticker...")
				return
			case <-ticker.C:
				log.Println("Triggering periodic background sync...")
				task.RunSyncTasks(ctx, sharedCache, cfg, meteoClient)
			}
		}
	}()

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.CORS(corsConfig)(sharedotel.MetricsMiddleware(metrics, authClient)(mux)),
	}

	go func() {
		<-ctx.Done() // Blockiert bis SIGINT/SIGTERM reinkommt
		log.Println("Shutting down integrated server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Could not gracefully shutdown the server: %v\n", err)
		}
	}()

	log.Printf("travel-and-weather-info: listening on port %s\n", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server error: could not listen on port %s: %v\n", cfg.Port, err)
	}

	log.Println("Server stopped")
}
