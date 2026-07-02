package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	sharedotel "otel"
	"syscall"
	"tenantdb"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trip-manager-htwg/application//backend/users/repository"
	"github.com/trip-manager-htwg/application//backend/users/service"
	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/firebaseclient"
	"github.com/trip-manager-htwg/application/backend/shared/middleware"
	"github.com/trip-manager-htwg/application/backend/users/config"
	"github.com/trip-manager-htwg/application/backend/users/database"
	"github.com/trip-manager-htwg/application/backend/users/handler"
	advertiser "github.com/trip-manager-htwg/application/backend/users/internal/advertisers"
	"github.com/trip-manager-htwg/application/backend/users/internal/platform"
	"github.com/trip-manager-htwg/application/backend/users/internal/tenant"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	corsConfig := middleware.DefaultCORSConfig()
	allowedOrigins := cfg.CORSAllowedOrigins
	if len(allowedOrigins) == 0 {
		log.Fatalf("No allowed origin configured")
	}
	corsConfig.AllowedOrigins = allowedOrigins

	otelProvider, err := sharedotel.New(ctx, "users", cfg.OTELCollectorEndpoint)
	if err != nil {
		log.Printf("warn: failed to initialize otel: %v", err)
	}
	var metrics *sharedotel.ServiceMetrics
	if otelProvider != nil {
		defer otelProvider.Shutdown(ctx)
		metrics, _ = sharedotel.NewServiceMetrics(otelProvider.Meter, "users")
	}

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

	// Dann App-Verbindung
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()
	// Auth client
	authClient := authclient.NewClient(cfg.AuthServiceURL)

	// Firebase client
	fbClient, err := firebaseclient.New(ctx, cfg.FirebaseProjectID)
	if err != nil {
		log.Fatalf("failed to init firebase client: %v", err)
	}

	// Wire up
	repo := repository.NewRepository(db)
	tenantRepo := tenant.NewRepository(db)
	invRepo := tenant.NewInvitationRepository(db)

	svc := service.NewService(repo, fbClient)

	provisioner := tenant.NewGitHubProvisioner(
		cfg.GitHubToken,
		cfg.GitHubRepo,
		cfg.GitHubBranch,
		cfg.GCPProjectID,
	)

	// Middleware
	requireAuth := authclient.RequireAuth(authClient)

	// Router
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"ok"}`))
		if err != nil {
			log.Printf("failed to write health response: %v", err)
			return
		}
	})

	emailSvc := tenant.NewEmailService(cfg.ResendApiKey)

	var metricsClient tenant.MetricsClient
	if cfg.GCPProjectID != "" {
		metricsClient = tenant.NewGCMMetricsClient(cfg.GCPProjectID)
	} else {
		metricsClient = tenant.NewPrometheusMetricsClient(cfg.PrometheusURL)
	}

	// PLATFORM
	platformRepo := platform.NewRepository(db)
	mux.HandleFunc("GET /platform/config", requireAuth(platform.GetConfigHandler(platformRepo)))
	mux.HandleFunc("PUT /platform/config", requireAuth(platform.UpdateConfigHandler(platformRepo)))
	// USER
	mux.HandleFunc("POST /provision", requireAuth(handler.ProvisionHandler(svc)))
	mux.HandleFunc("GET /me", requireAuth(handler.GetMeHandler(svc)))
	mux.HandleFunc("PUT /me", requireAuth(handler.UpdateMeHandler(svc)))
	mux.HandleFunc("GET /{id}", handler.GetByIDHandler(svc))
	// TENANTS
	mux.HandleFunc("POST /tenants/register", requireAuth(tenant.RegisterHandler(tenantRepo, svc)))
	mux.HandleFunc("GET /tenants/me", requireAuth(tenant.GetTenantHandler(tenantRepo)))
	mux.HandleFunc("GET /tenants/by-slug/{slug}", tenant.GetTenantBySlugHandler(tenantRepo))
	mux.HandleFunc("GET /tenants/me/branding", requireAuth(tenant.GetBrandingHandler(tenantRepo)))
	mux.HandleFunc("PUT /tenants/me/branding", requireAuth(tenant.UpdateBrandingHandler(tenantRepo)))
	mux.HandleFunc("PUT /tenants/me/tier", requireAuth(tenant.UpgradeTierHandler(tenantRepo, provisioner)))
	mux.HandleFunc("GET /tenants/me/usage", requireAuth(tenant.GetUsageHandler(tenantRepo, metricsClient, platformRepo)))
	mux.HandleFunc("GET /tenants/me/settings", requireAuth(tenant.GetSettingsHandler(tenantRepo)))
	mux.HandleFunc("PUT /tenants/me/settings", requireAuth(tenant.UpdateSettingsHandler(tenantRepo)))
	mux.HandleFunc("GET /tenants/me/members", requireAuth(tenant.ListMembersHandler(repo)))
	mux.HandleFunc("DELETE /tenants/me/members/{userId}", requireAuth(tenant.RemoveMemberHandler(repo, fbClient)))
	mux.HandleFunc("GET /tenants/me/invitations", requireAuth(tenant.ListInvitationsHandler(invRepo)))
	mux.HandleFunc("POST /tenants/me/invitations", requireAuth(tenant.CreateInvitationHandler(invRepo, cfg.BaseUrl, emailSvc, tenantRepo)))
	mux.HandleFunc("DELETE /tenants/me/invitations/{invitationId}", requireAuth(tenant.DeleteInvitationHandler(invRepo)))
	mux.HandleFunc("POST /tenants/join", requireAuth(tenant.AcceptInvitationHandler(invRepo, repo, svc)))
	mux.HandleFunc("GET /tenants/all", requireAuth(tenant.ListAllTenantsHandler(tenantRepo)))
	mux.HandleFunc("DELETE /tenants/me", requireAuth(tenant.DeleteTenantHandler(tenantRepo, repo, svc)))
	mux.HandleFunc("GET /tenants/me/usage/timeseries", requireAuth(tenant.GetUsageTimeSeriesHandler(metricsClient)))
	mux.HandleFunc("POST /tenants/join-by-slug", requireAuth(tenant.JoinTenantBySlugHandler(tenantRepo, svc)))

	// internal endpoint only
	mux.HandleFunc("GET /internal/tenants/{tenantId}/db-url", func(w http.ResponseWriter, r *http.Request) {
		// Nur interne Aufrufe erlauben
		if r.Header.Get("X-Internal-Secret") != cfg.InternalSecret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		tenantID := r.PathValue("tenantId")
		ctx := tenantdb.WithTenantID(r.Context(), "default")
		dbURL, err := tenantRepo.GetEnterpriseDBURL(ctx, tenantID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"dbUrl": dbURL})
	})
	// ADVERTISER
	advRepo := advertiser.NewRepository(db)
	mux.HandleFunc("GET /advertisers", requireAuth(advertiser.ListHandler(advRepo)))
	mux.HandleFunc("POST /advertisers", requireAuth(advertiser.CreateHandler(advRepo, fbClient)))
	mux.HandleFunc("GET /advertisers/me", requireAuth(advertiser.GetMeHandler(advRepo)))
	mux.HandleFunc("GET /advertisers/{id}", requireAuth(advertiser.GetByIDHandler(advRepo)))
	mux.HandleFunc("POST /advertisers/{id}/contact/{tenantId}", requireAuth(advertiser.ContactTenantHandler(advRepo, tenantRepo, emailSvc)))
	mux.HandleFunc("POST /advertisers/{id}/tenants", requireAuth(advertiser.AssignTenantHandler(advRepo)))
	mux.HandleFunc("DELETE /advertisers/{id}/tenants/{tenantId}", requireAuth(advertiser.RemoveTenantHandler(advRepo)))

	// Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.CORS(corsConfig)(sharedotel.MetricsMiddleware(metrics, authClient)(mux)),
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
			log.Fatalf("Failed to shutdown server: %v", err)
		}
	}()

	log.Printf("users service starting on port %s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
	log.Println("Server stopped")
}
