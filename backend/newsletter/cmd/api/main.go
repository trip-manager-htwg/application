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

	"github.com/trip-manager-htwg/application//backend/newsletter/database"
	"github.com/trip-manager-htwg/application//backend/newsletter/internal/db"
	"github.com/trip-manager-htwg/application//backend/newsletter/internal/newsletter"
	"github.com/trip-manager-htwg/application//backend/shared/authclient"
	"github.com/trip-manager-htwg/application//backend/shared/email"
	"github.com/trip-manager-htwg/application//backend/shared/middleware"

	"github.com/jmoiron/sqlx"
	"github.com/kelseyhightower/envconfig"
	_ "github.com/lib/pq"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/robfig/cron/v3"
)

type config struct {
	// API & Gemeinsame Configs
	Port                  string   `envconfig:"PORT" default:"8008"`
	AuthServiceURL        string   `envconfig:"AUTH_SERVICE_URL"`
	LogLevel              string   `envconfig:"LOG_LEVEL"`
	CORSAllowedOrigins    []string `envconfig:"CORS_ALLOWED_ORIGINS"`
	OTELCollectorEndpoint string   `envconfig:"OTEL_COLLECTOR_ENDPOINT" default:""`
	DatabaseURL           string   `envconfig:"DATABASE_URL"`
	MigrationDBURL        string   `envconfig:"MIGRATION_DB_URL"`
	AppDBPassword         string   `envconfig:"APP_DB_PASSWORD"`

	// Worker Configs (aus dem Worker-Service integriert)
	UsersDBURL           string `envconfig:"USERS_DB_URL"`
	Neo4jURI             string `envconfig:"NEO4J_URI"`
	Neo4jUser            string `envconfig:"NEO4J_USERNAME"`
	Neo4jPassword        string `envconfig:"NEO4J_PASSWORD"`
	CronSchedule         string `envconfig:"CRON_SCHEDULE" default:"0 0 * * *"`          // Standard: Täglich um Mitternacht
	InsightsCronSchedule string `envconfig:"INSIGHTS_CRON_SCHEDULE" default:"0 0 * * *"` // siehe oben
	ResendApiKey         string `envconfig:"RESEND_API_KEY" default:""`
}

func load() (*config, error) {
	var cfg config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	ctx := context.Background()
	cfg, err := load()
	if err != nil {
		log.Fatal("Failed to load config")
	}

	// 1. OpenTelemetry Setup
	otelProvider, err := sharedotel.New(ctx, "newsletter", cfg.OTELCollectorEndpoint)
	if err != nil {
		log.Printf("warn: failed to initialize otel: %v", err)
	}
	var metrics *sharedotel.ServiceMetrics
	if otelProvider != nil {
		defer otelProvider.Shutdown(ctx)
		metrics, _ = sharedotel.NewServiceMetrics(otelProvider.Meter, "newsletter")
	}

	// 2. Datenbanken initialisieren
	// Haupt-Newsletter-DB (Wird von API und Worker genutzt!)
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

	// 3. Einbindung Email Service

	emailSvc := email.NewService(cfg.ResendApiKey)

	// Normaler Betrieb mit App-User
	newsletterDB, err := sqlx.Connect("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}

	// Users-DB (wird nur vom Worker-Teil benötigt)
	usersDB, err := sqlx.Connect("postgres", cfg.UsersDBURL)
	if err != nil {
		log.Fatalf("newsletter: failed to connect to users-db: %v", err)
	}
	defer usersDB.Close()

	// 3. Neo4j Treiber initialisieren (für den Worker-Teil)
	neo4jDriver, err := neo4j.NewDriverWithContext(
		cfg.Neo4jURI,
		neo4j.BasicAuth(cfg.Neo4jUser, cfg.Neo4jPassword, ""),
	)
	if err != nil {
		log.Fatalf("newsletter: failed to create neo4j driver: %v", err)
	}
	defer neo4jDriver.Close(ctx)

	if err := neo4jDriver.VerifyConnectivity(ctx); err != nil {
		log.Fatalf("newsletter: neo4j not reachable: %v", err)
	}

	// ==========================================
	// CRON WORKER THREAD(s) STARTEN
	// ==========================================
	log.Println("newsletter: running initial background generation...")
	go runGeneration(usersDB, newsletterDB, neo4jDriver) // Einmalig asynchron beim Start anwerfen

	c := cron.New()
	_, err = c.AddFunc(cfg.CronSchedule, func() {
		log.Println("newsletter-worker: cron triggered, generating newsletters...")
		runGeneration(usersDB, newsletterDB, neo4jDriver)
	})
	if err != nil {
		log.Printf("newsletter-worker: failed to add cron job: %v", err)
	} else {
		c.Start()
		log.Printf("newsletter-worker: cron scheduler started with schedule '%s'", cfg.CronSchedule)
	}

	go runInsightsGeneration(usersDB, newsletterDB, neo4jDriver, emailSvc) // Initial

	_, err = c.AddFunc(cfg.InsightsCronSchedule, func() {
		log.Println("insights: cron triggered...")
		runInsightsGeneration(usersDB, newsletterDB, neo4jDriver, emailSvc)
	})

	// ==========================================
	// API ROUTER & SERVER STARTEN
	// ==========================================
	repo := newsletter.NewRepository(newsletterDB)
	svc := newsletter.NewService(repo)

	authClient := authclient.NewClient(cfg.AuthServiceURL)
	requireAuth := authclient.RequireAuth(authClient)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("GET /", requireAuth(newsletter.GetNewsletterHandler(svc)))

	mux.HandleFunc("GET /insights", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		firebaseUID := r.Header.Get("X-Firebase-UID")
		if firebaseUID == "" {
			// aus JWT lesen
			if result, err := authClient.ValidateBearerToken(r.Context(), r.Header.Get("Authorization")); err == nil {
				firebaseUID = result.UserID
			}
		}

		var insights []json.RawMessage
		rows, err := newsletterDB.QueryContext(r.Context(), `
        SELECT content FROM advertiser_insights
        WHERE advertiser_id = (
            SELECT id FROM advertisers WHERE firebase_uid = $1
        )
        ORDER BY generated_at DESC
    `, firebaseUID)
		if err != nil {
			http.Error(w, `{"error":"failed to load insights"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var content json.RawMessage
			if err := rows.Scan(&content); err == nil {
				insights = append(insights, content)
			}
		}

		if insights == nil {
			insights = []json.RawMessage{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(insights)
	}))

	corsConfig := middleware.DefaultCORSConfig()
	corsConfig.AllowedOrigins = cfg.CORSAllowedOrigins

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.CORS(corsConfig)(sharedotel.MetricsMiddleware(metrics, authClient)(mux)),
	}

	// Graceful Shutdown
	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
		<-sigch
		log.Println("newsletter: shutting down server and scheduler...")
		c.Stop() // Stoppt den Cron-Scheduler

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("newsletter: shutdown error: %v", err)
		}
	}()

	log.Printf("newsletter service starting on port %s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("newsletter: server error: %v", err)
	}
	log.Println("newsletter: stopped")
}

func runGeneration(usersDB *sqlx.DB, newsletterDB *sqlx.DB, driver neo4j.DriverWithContext) {
	ctx := context.Background()

	var users []struct {
		FirebaseUID string `db:"firebase_uid"`
		TenantID    string `db:"tenant_id"`
	}
	if err := usersDB.SelectContext(ctx, &users, `SELECT firebase_uid, tenant_id FROM users`); err != nil {
		log.Printf("newsletter-worker: failed to fetch users: %v", err)
		return
	}
	log.Printf("newsletter-worker: generating for %d users", len(users))

	for _, u := range users {
		tenantCtx := tenantdb.WithTenantID(ctx, u.TenantID)

		sections, err := db.GenerateForUser(tenantCtx, driver, u.FirebaseUID)
		if err != nil {
			log.Printf("newsletter-worker: error for user %s: %v", u.FirebaseUID, err)
			continue
		}

		content, err := json.Marshal(sections)
		if err != nil {
			log.Printf("newsletter-worker: marshal error for user %s: %v", u.FirebaseUID, err)
			continue
		}

		err = tenantdb.WithTenant(tenantCtx, newsletterDB, func(tx *sqlx.Tx) error {
			_, err := tx.ExecContext(tenantCtx, `
				INSERT INTO newsletters (firebase_uid, tenant_id, content, generated_at)
				VALUES ($1, $2, $3, NOW())
				ON CONFLICT (firebase_uid, tenant_id) DO UPDATE
				SET content = EXCLUDED.content,
				    generated_at = EXCLUDED.generated_at
			`, u.FirebaseUID, u.TenantID, content)
			return err
		})
		if err != nil {
			log.Printf("newsletter-worker: db write error for user %s: %v", u.FirebaseUID, err)
			continue
		}
	}
	log.Println("newsletter-worker: generation complete")
}

func runInsightsGeneration(usersDB *sqlx.DB, newsletterDB *sqlx.DB, driver neo4j.DriverWithContext, emailSvc *email.Service) {
	ctx := context.Background()

	// Alle Advertiser + ihre Tenants holen
	var advTenants []struct {
		AdvertiserID string `db:"advertiser_id"`
		TenantID     string `db:"tenant_id"`
	}
	if err := usersDB.SelectContext(ctx, &advTenants,
		`SELECT advertiser_id, tenant_id FROM advertiser_tenants`); err != nil {
		log.Printf("insights: failed to fetch advertiser tenants: %v", err)
		return
	}

	log.Printf("insights: generating for %d advertiser-tenant pairs", len(advTenants))

	for _, at := range advTenants {
		insights, err := db.GenerateInsightsForTenant(ctx, driver, at.TenantID)
		if err != nil {
			log.Printf("insights: error for tenant %s: %v", at.TenantID, err)
			continue
		}

		content, err := json.Marshal(insights)
		if err != nil {
			log.Printf("insights: marshal error: %v", err)
			continue
		}

		_, err = newsletterDB.ExecContext(ctx, `
            INSERT INTO advertiser_insights (advertiser_id, tenant_id, content, generated_at)
            VALUES ($1, $2, $3, NOW())
            ON CONFLICT (advertiser_id, tenant_id) DO UPDATE
            SET content = EXCLUDED.content,
                generated_at = EXCLUDED.generated_at
        `, at.AdvertiserID, at.TenantID, content)
		if err != nil {
			log.Printf("insights: db write error: %v", err)
			continue
		}
	}
	if emailSvc != nil {
		sendInsightEmails(ctx, usersDB, newsletterDB, emailSvc)
	}
	log.Println("insights: generation complete")
}

func sendInsightEmails(ctx context.Context, usersDB *sqlx.DB, newsletterDB *sqlx.DB, emailSvc *email.Service) {
	var advertisers []struct {
		ID    string `db:"id"`
		Email string `db:"email"`
		Name  string `db:"name"`
	}
	if err := usersDB.SelectContext(ctx, &advertisers, `SELECT id, email, name FROM advertisers`); err != nil {
		log.Printf("insights email: failed to fetch advertisers: %v", err)
		return
	}

	for _, adv := range advertisers {
		var insights []struct {
			TenantID string          `db:"tenant_id"`
			Content  json.RawMessage `db:"content"`
		}
		if err := newsletterDB.SelectContext(ctx, &insights,
			`SELECT tenant_id, content FROM advertiser_insights WHERE advertiser_id = $1`, adv.ID); err != nil {
			continue
		}

		var summaries []email.InsightSummary
		for _, ins := range insights {
			var data struct {
				TopDestinations []struct {
					Country string `json:"country"`
				} `json:"topDestinations"`
				Engagement struct {
					TotalLikes int64 `json:"totalLikes"`
				} `json:"engagement"`
				SeasonalPattern struct {
					PeakMonth string `json:"peakMonth"`
				} `json:"seasonalPattern"`
			}
			if err := json.Unmarshal(ins.Content, &data); err != nil {
				continue
			}
			topDest := ""
			if len(data.TopDestinations) > 0 {
				topDest = data.TopDestinations[0].Country
			}
			summaries = append(summaries, email.InsightSummary{
				TenantID:       ins.TenantID,
				TenantName:     ins.TenantID,
				TopDestination: topDest,
				TotalLikes:     data.Engagement.TotalLikes,
				PeakMonth:      data.SeasonalPattern.PeakMonth,
			})
		}

		if len(summaries) > 0 {
			go emailSvc.SendInsightsReport(adv.Email, adv.Name, summaries)
		}
	}
}
