package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	sharedotel "otel"
	"syscall"
	"time"

	"github.com/trip-manager-htwg/application/backend/auth/config"
	"github.com/trip-manager-htwg/application/backend/auth/internal/handler"
	"github.com/trip-manager-htwg/application/backend/auth/internal/service"
	"github.com/trip-manager-htwg/application/backend/shared/middleware"
)

func main() {
	ctx := context.Background()
	// Load cache
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config %v", err)
	}
	log.Printf("Starting Auth Service on port %s\n", cfg.Port)

	otelProvider, err := sharedotel.New(ctx, "auth", cfg.OTELCollectorEndpoint)
	if err != nil {
		log.Printf("warn: failed to initialize otel: %v", err)
	}
	var metrics *sharedotel.ServiceMetrics
	if otelProvider != nil {
		defer otelProvider.Shutdown(ctx)
		metrics, _ = sharedotel.NewServiceMetrics(otelProvider.Meter, "auth")
	}

	corsConfig := middleware.DefaultCORSConfig()
	allowedOrigins := cfg.CORSAllowedOrigins
	if len(allowedOrigins) == 0 {
		log.Fatalf("Allowed Origins is empty!")
	}
	corsConfig.AllowedOrigins = allowedOrigins

	// Initialize Firebase
	authClient, err := config.InitializeFirebase(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}

	// Setup service
	authService := service.NewService(authClient)
	authHandler := handler.NewHandler(authService)

	// Setup routes
	mux := http.NewServeMux()

	// Auth endpoints
	mux.HandleFunc("POST /validate-token", authHandler.ValidateToken)
	mux.HandleFunc("GET /validate-token", authHandler.ValidateTokenFromHeader)

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Start server
	server := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: middleware.CORS(corsConfig)(func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.ServeHTTP(w, r)
				if metrics != nil && r.URL.Path != "/health" {
					metrics.RecordAPICall(r.Context(), "default", r.URL.Path, r.Method)
				}
			})
		}(mux)),
	}

	// Graceful shutdown
	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
		<-sigch

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := server.Shutdown(ctx)
		if err != nil {
			log.Printf("Error shutting down server: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}
