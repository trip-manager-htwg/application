package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trip-manager-htwg/application//backend/trips/database"
	"github.com/trip-manager-htwg/application//backend/trips/internal/accommodation"
	dbpool "github.com/trip-manager-htwg/application//backend/trips/internal/database"
	"github.com/trip-manager-htwg/application//backend/trips/internal/locations"
	"github.com/trip-manager-htwg/application//backend/trips/internal/transport"
	"github.com/trip-manager-htwg/application//backend/trips/internal/trip"
	"github.com/trip-manager-htwg/application//backend/trips/pubsub"
	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/middleware"
	sharedotel "github.com/trip-manager-htwg/application/backend/shared/otel"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	"github.com/trip-manager-htwg/application/backend/trips/config"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	otelProvider, err := sharedotel.New(ctx, "trips", cfg.OTELCollectorEndpoint)
	if err != nil {
		log.Printf("warn: failed to initialize otel: %v", err)
	}
	var metrics *sharedotel.ServiceMetrics
	if otelProvider != nil {
		defer otelProvider.Shutdown(ctx)
		metrics, _ = sharedotel.NewServiceMetrics(otelProvider.Meter, "trips")
	}

	corsConfig := middleware.DefaultCORSConfig()
	allowedOrigins := cfg.CORSAllowedOrigins
	if len(allowedOrigins) == 0 {
		log.Fatalf("No allowed origin configured")
	}
	corsConfig.AllowedOrigins = allowedOrigins

	// DB
	migrationDB, err := sqlx.Connect("postgres", cfg.MigrationDBURL)
	if err != nil {
		log.Fatalf("failed to connect to migration db: %v", err)
	}
	if err := database.RunMigrations(migrationDB, map[string]string{
		"APP_DB_PASSWORD": cfg.AppDBPassword,
	}); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	migrationDB.Close()

	// Dann App-Verbindung mit _app User
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer func(db *sqlx.DB) {
		err := db.Close()
		if err != nil {
			log.Fatalf("failed to close database connection: %v", err)
		}
	}(db)

	// PubSub Producer
	var pubsubProducer *pubsub.Producer
	if cfg.GCPProjectID != "" && cfg.PubSubTopicID != "" {
		var err error
		pubsubProducer, err = pubsub.NewProducer(cfg.GCPProjectID, cfg.PubSubTopicID)
		if err != nil {
			log.Fatalf("failed to initialize pubsub producer: %v", err)
		}
		defer func(pubsubProducer *pubsub.Producer) {
			err := pubsubProducer.Close()
			if err != nil {
				log.Fatalf("failed to close pubsub producer: %v", err)
			}
		}(pubsubProducer)
		log.Printf("Pub/Sub producer initialized for project %s on topic %s", cfg.GCPProjectID, cfg.PubSubTopicID)
	} else {
		log.Println("warn: GCP_PROJECT_ID or PUBSUB_TOPIC_ID not set, trip.created events will not be published")
	}

	// Clients
	authClient := authclient.NewClient(cfg.AuthServiceURL)
	usersClient := userclient.NewUsersClient(cfg.UsersServiceURL)
	requireAuth := authclient.RequireAuth(authClient)

	// Wire up – trips
	tripRepo := trip.NewRepository(db)
	tripSvc := trip.NewService(tripRepo)

	// Wire up – transport
	transportRepo := transport.NewRepository(db)
	transportSvc := transport.NewService(transportRepo)

	// Wire up – accommodation
	accommodationRepo := accommodation.NewRepository(db)
	accommodationSvc := accommodation.NewService(accommodationRepo)

	// Wire up - locations
	locationRepo := locations.NewRepository(db)
	locationSvc := locations.NewService(locationRepo)

	// Router
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"ok"}`))
		if err != nil {
			return
		}
	})

	poolManager := dbpool.NewPoolManager(db, cfg.UsersServiceURL, cfg.InternalSecret, cfg.AppDBPassword)
	handler := tenantDBMiddleware(poolManager, authClient)(mux)
	// Trips
	mux.HandleFunc("GET /", requireAuth(trip.ListTripsHandler(tripSvc, usersClient)))
	mux.HandleFunc("POST /", requireAuth(trip.CreateTripHandler(tripSvc, usersClient, pubsubProducer)))
	mux.HandleFunc("GET /recent", requireAuth(trip.ListRecentTripsHandler(tripSvc, usersClient)))
	mux.HandleFunc("GET /search", requireAuth(trip.SearchTripsHandler(tripSvc, usersClient)))
	mux.HandleFunc("GET /{tripId}", requireAuth(trip.GetTripHandler(tripSvc, usersClient)))
	mux.HandleFunc("PUT /{tripId}", requireAuth(trip.UpdateTripHandler(tripSvc, usersClient)))
	mux.HandleFunc("DELETE /{tripId}", requireAuth(trip.DeleteTripHandler(tripSvc, usersClient)))

	// Transports
	mux.HandleFunc("GET /{tripId}/transports", requireAuth(transport.ListHandler(transportSvc)))
	mux.HandleFunc("POST /{tripId}/transports", requireAuth(transport.CreateHandler(transportSvc, usersClient)))
	mux.HandleFunc("PUT /{tripId}/transports/{transportId}", requireAuth(transport.UpdateHandler(transportSvc, usersClient)))
	mux.HandleFunc("DELETE /{tripId}/transports/{transportId}", requireAuth(transport.DeleteHandler(transportSvc, usersClient)))

	// Accommodations
	mux.HandleFunc("GET /{tripId}/accommodations", requireAuth(accommodation.ListHandler(accommodationSvc)))
	mux.HandleFunc("POST /{tripId}/accommodations", requireAuth(accommodation.CreateHandler(accommodationSvc, usersClient)))
	mux.HandleFunc("PUT /{tripId}/accommodations/{accommodationId}", requireAuth(accommodation.UpdateHandler(accommodationSvc, usersClient)))
	mux.HandleFunc("DELETE /{tripId}/accommodations/{accommodationId}", requireAuth(accommodation.DeleteHandler(accommodationSvc, usersClient)))

	// Locations
	mux.HandleFunc("GET /{tripId}/locations", requireAuth(locations.ListHandler(locationSvc, cfg.S3Endpoint, cfg.S3Bucket)))
	mux.HandleFunc("POST /{tripId}/locations", requireAuth(locations.CreateHandler(locationSvc, usersClient, cfg.S3Endpoint, cfg.S3Bucket)))
	mux.HandleFunc("PUT /{tripId}/locations/{locationId}", requireAuth(locations.UpdateHandler(locationSvc, cfg.S3Endpoint, cfg.S3Bucket)))
	mux.HandleFunc("DELETE /{tripId}/locations/{locationId}", requireAuth(locations.DeleteHandler(locationSvc)))

	// Location images
	mux.HandleFunc("POST /{tripId}/locations/{locationId}/images", requireAuth(locations.AddImageHandler(locationSvc, cfg.S3Endpoint, cfg.S3Bucket)))
	mux.HandleFunc("DELETE /{tripId}/locations/{locationId}/images/{imageId}", requireAuth(locations.DeleteImageHandler(locationSvc)))

	// Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.CORS(corsConfig)(sharedotel.MetricsMiddleware(metrics, authClient)(handler)),
	}

	// Graceful shutdown
	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
		<-sigch
		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("failed to shutdown server: %v", err)
		}
	}()

	log.Printf("trips service starting on port %s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
	log.Println("Server stopped")
}

func tenantDBMiddleware(poolManager *dbpool.PoolManager, authClient *authclient.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := "default"
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				if result, err := authClient.ValidateBearerToken(r.Context(), authHeader); err == nil && result.Valid {
					if tid, ok := result.Claims["tenant_id"].(string); ok && tid != "" {
						tenantID = tid
					}
				}
			}
			db := poolManager.GetDB(r.Context(), tenantID)
			ctx := dbpool.WithDB(r.Context(), db)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
