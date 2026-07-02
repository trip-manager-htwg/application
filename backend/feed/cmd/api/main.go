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

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/trip-manager-htwg/application/backend/feed/config"
	"github.com/trip-manager-htwg/application/backend/feed/internal/consumer"
	"github.com/trip-manager-htwg/application/backend/feed/internal/feed"
	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/middleware"
)

func main() {
	// Root-Context für den asynchronen Background-Worker
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// 1. OpenTelemetry Setup
	otelProvider, err := sharedotel.New(workerCtx, "feed", cfg.OTELCollectorEndpoint)
	if err != nil {
		log.Printf("warn: failed to initialize otel: %v", err)
	}
	var metrics *sharedotel.ServiceMetrics
	if otelProvider != nil {
		defer otelProvider.Shutdown(workerCtx)
		metrics, _ = sharedotel.NewServiceMetrics(otelProvider.Meter, "feed")
	}

	// 2. Neo4j Treiber initialisieren (Ein einziger Pool für API + Consumer!)
	driver, err := neo4j.NewDriverWithContext(
		cfg.Neo4jURI,
		neo4j.BasicAuth(cfg.Neo4jUser, cfg.Neo4jPassword, ""),
	)
	if err != nil {
		log.Fatalf("feed: failed to create neo4j driver: %v", err)
	}
	defer func() {
		if err := driver.Close(context.Background()); err != nil {
			log.Printf("feed: failed to close neo4j driver: %v", err)
		}
	}()

	if err := driver.VerifyConnectivity(context.Background()); err != nil {
		log.Fatalf("feed: neo4j not reachable: %v", err)
	}
	log.Printf("feed: connected to neo4j at %s", cfg.Neo4jURI)

	// Idempotentes DB-Schema Setup ausführen
	if err := consumer.SetupSchema(context.Background(), driver); err != nil {
		log.Fatalf("feed: failed to setup neo4j schema: %v", err)
	}

	// 3. PubSub Consumer initialisieren und asynchron starten
	pubSubConsumer, err := consumer.New(driver, cfg.GCPProjectID, cfg.PubSubSubscription)
	if err != nil {
		log.Fatalf("feed: failed to initialize pubsub consumer: %v", err)
	}
	defer func() {
		if err := pubSubConsumer.Close(); err != nil {
			log.Printf("feed: failed to close pubsub consumer: %v", err)
		}
	}()

	// Consumer im Hintergrund anwerfen
	go pubSubConsumer.Start(workerCtx)

	// 4. API Endpunkte verdrahten
	feedRepo := feed.NewRepository(driver)
	feedSvc := feed.NewService(feedRepo)

	authClient := authclient.NewClient(cfg.AuthServiceURL)
	requireAuth := authclient.RequireAuth(authClient)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("GET /", requireAuth(feed.GetGlobalFeedHandler(feedSvc)))
	mux.HandleFunc("GET /personal", requireAuth(feed.GetPersonalFeedHandler(feedSvc)))

	corsConfig := middleware.DefaultCORSConfig()
	corsConfig.AllowedOrigins = cfg.CORSAllowedOrigins

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.CORS(corsConfig)(sharedotel.MetricsMiddleware(metrics, authClient)(mux)),
	}

	// 5. Graceful Shutdown für Server und Background Worker
	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
		<-sigch
		log.Println("feed: shutting down server and background consumers...")

		cancelWorker() // Signalisiert dem PubSub-Consumer sofort aufzuhören

		ctx, cancelServer := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelServer()
		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("feed: shutdown error: %v", err)
		}
	}()

	log.Printf("feed service (including background consumer) starting on port %s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("feed: server error: %v", err)
	}
	log.Println("feed: stopped")
}
