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

	"cloud.google.com/go/firestore"
	"github.com/trip-manager-htwg/application//backend/shared/authclient"
	"github.com/trip-manager-htwg/application//backend/shared/middleware"
	"github.com/trip-manager-htwg/application//backend/shared/userclient"
	"github.com/trip-manager-htwg/application//backend/social/config"
	"github.com/trip-manager-htwg/application//backend/social/internal/comment"
	"github.com/trip-manager-htwg/application//backend/social/internal/like"
	"github.com/trip-manager-htwg/application//backend/social/internal/presigner/handler"
	presignerPrv "github.com/trip-manager-htwg/application//backend/social/internal/presigner/provider"
	presignerSvc "github.com/trip-manager-htwg/application//backend/social/internal/presigner/service"
	"github.com/trip-manager-htwg/application//backend/social/pubsub"
)

func main() {
	ctx := context.Background()
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config %v", err)
	}

	log.Printf("Starting Social Service on port %s\n", cfg.Port)

	otelProvider, err := sharedotel.New(ctx, "social", cfg.OTELCollectorEndpoint)
	if err != nil {
		log.Printf("warn: failed to initialize otel: %v", err)
	}
	var metrics *sharedotel.ServiceMetrics
	if otelProvider != nil {
		defer otelProvider.Shutdown(ctx)
		metrics, _ = sharedotel.NewServiceMetrics(otelProvider.Meter, "social")
	}

	corsConfig := middleware.DefaultCORSConfig()
	allowedOrigins := cfg.CORSAllowedOrigins
	if len(allowedOrigins) == 0 {
		log.Fatalf("No allowed origin configured")
	}
	corsConfig.AllowedOrigins = allowedOrigins

	firestoreClient, err := config.ConnectFirestore(ctx, *cfg)
	if err != nil {
		log.Fatalf("Failed to connect to Firestore: %v", err)
	}
	defer func(firestoreClient *firestore.Client) {
		if err := firestoreClient.Close(); err != nil {
			log.Fatalf("Failed to close Firestore client: %v", err)
		}
	}(firestoreClient)

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
		log.Println("warn: GCP_PROJECT_ID or PUBSUB_TOPIC_ID not set, trip.liked/commented events will not be published")
	}

	authClient := authclient.NewClient(cfg.AuthClientConnectionString)
	usersClient := userclient.NewUsersClient(cfg.UsersServiceURL)
	requireAuth := authclient.RequireAuth(authClient)

	likeRepo := like.NewLikeRepository(firestoreClient)
	likeService := like.NewServiceImpl(likeRepo)

	commentRepo := comment.NewCommentRepository(firestoreClient)
	commentService := comment.NewServiceImpl(commentRepo)

	provider, err := presignerPrv.NewFromEnv(ctx, *cfg)
	if err != nil {
		log.Fatalf("Failed to initialize provider %v", err)
	}
	presignerService := presignerSvc.NewService(provider, cfg.StorageTTL)

	// Setup routes
	mux := http.NewServeMux()

	// Presigner endpoints
	mux.HandleFunc("POST /uploads/presigned",
		authclient.RequireAuth(authClient)(handler.GetPresignedUploadURLHandler(presignerService, authClient)))
	mux.HandleFunc("POST /uploads/download-url",
		handler.GetPresignedDownloadURLHandler(presignerService))

	// Like endpoints – alle requireAuth, da tenantId zwingend benötigt wird
	mux.HandleFunc("GET /{tripId}/likes", requireAuth(like.GetTripLikesHandler(likeService)))
	mux.HandleFunc("POST /{tripId}/likes", requireAuth(like.LikeTripHandler(likeService, pubsubProducer)))
	mux.HandleFunc("DELETE /{tripId}/likes", requireAuth(like.UnlikeTripHandler(likeService)))

	// Comment endpoints – alle requireAuth, da tenantId zwingend benötigt wird
	mux.HandleFunc("GET /{tripId}/comments", requireAuth(comment.ListTripCommentsHandler(commentService, usersClient)))
	mux.HandleFunc("POST /{tripId}/comments", requireAuth(comment.CreateTripCommentHandler(commentService, usersClient, pubsubProducer)))
	mux.HandleFunc("DELETE /{tripId}/comments/{commentId}", requireAuth(comment.DeleteCommentHandler(commentService)))

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"ok"}`))
		if err != nil {
			return
		}
	})

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.CORS(corsConfig)(sharedotel.MetricsMiddleware(metrics, authClient)(mux)),
	}

	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
		<-sigch
		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("Failed to shutdown server: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
