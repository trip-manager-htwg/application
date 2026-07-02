package otel

import (
	"log"
	"net/http"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
)

func MetricsMiddleware(metrics *ServiceMetrics, authClient *authclient.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			// tenant_id VOR ServeHTTP aus JWT lesen
			tenantID := "default"
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				if result, err := authClient.ValidateBearerToken(r.Context(), authHeader); err == nil && result.Valid {
					if tid, ok := result.Claims["tenant_id"].(string); ok && tid != "" {
						tenantID = tid
					}
				}
			}

			next.ServeHTTP(w, r)

			// Nur echte Tenant-Requests zählen – kein default, kein unauthenticated
			if metrics != nil && tenantID != "default" && authHeader != "" {
				log.Printf("[MetricsMiddleware] tenant_id=%s path=%s", tenantID, r.URL.Path)
				metrics.RecordAPICall(r.Context(), tenantID, r.URL.Path, r.Method)
			}
		})
	}
}
