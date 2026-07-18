package tenant

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
	utils "github.com/trip-manager-htwg/application/backend/users/internal/shared"
	"github.com/trip-manager-htwg/application/backend/users/repository"
	"github.com/trip-manager-htwg/application/backend/users/service"
)

type RegisterRequest struct {
	TenantName string `json:"tenantName"`
	Tier       string `json:"tier"` // free, standard
}

type RegisterResponse struct {
	TenantID string `json:"tenantId"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Tier     string `json:"tier"`
}

func generateSlug(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	slug := re.ReplaceAllString(strings.ToLower(name), "-")
	slug = strings.Trim(slug, "-")
	return slug
}

func RegisterHandler(repo Repository, userSvc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.TenantName == "" {
			utils.RespondError(w, http.StatusBadRequest, "tenantName is required")
			return
		}

		tier := req.Tier
		if tier == "" {
			tier = "free"
		}
		if tier != "free" && tier != "standard" {
			utils.RespondError(w, http.StatusBadRequest, "tier must be free or standard")
			return
		}

		tenantID := fmt.Sprintf("t-%s", uuid.New().String()[:8])
		slug := generateSlug(req.TenantName)
		ctx := tenantdb.WithTenantID(r.Context(), tenantID)

		tenant, err := repo.Create(ctx, &Tenant{
			ID:   tenantID,
			Name: req.TenantName,
			Tier: tier,
			Slug: slug,
		})

		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// User zum tenant_owner machen
		_, _, err = userSvc.ProvisionWithTenant(r.Context(), service.ProvisionInput{
			FirebaseUID: firebaseUID,
			TenantID:    tenantID,
			Role:        "tenant_owner",
		})
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		utils.RespondJSON(w, http.StatusCreated, RegisterResponse{
			TenantID: tenant.ID,
			Name:     tenant.Name,
			Slug:     tenant.Slug,
			Tier:     tenant.Tier,
		})
	}
}

func GetTenantHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			utils.RespondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenant, err := repo.GetByID(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		utils.RespondJSON(w, http.StatusCreated, RegisterResponse{
			TenantID: tenant.ID,
			Name:     tenant.Name,
			Slug:     tenant.Slug,
			Tier:     tenant.Tier,
		})
	}
}

func GetTenantBySlugHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if slug == "" {
			utils.RespondError(w, http.StatusBadRequest, "slug is required")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), "default")
		t, err := repo.GetBySlug(ctx, slug)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		utils.RespondJSON(w, http.StatusOK, RegisterResponse{
			TenantID: t.ID,
			Name:     t.Name,
			Slug:     t.Slug,
			Tier:     t.Tier,
		})
	}
}

// Branding related

type BrandingRequest struct {
	LogoURL      string `json:"logoUrl"`
	PrimaryColor string `json:"primaryColor"`
	CompanyName  string `json:"companyName"`
	CustomDomain string `json:"customDomain"`
}

func GetBrandingHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			utils.RespondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenant, err := repo.GetByID(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		utils.RespondJSON(w, http.StatusOK, tenant.Branding)
	}
}

func UpdateBrandingHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			utils.RespondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		role := authclient.GetUserRole(r)
		if role != "tenant_owner" && role != "tenant_admin" && role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenant, err := repo.GetByID(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		// Branding nur für paid Tiers
		if tenant.Tier == "free" {
			utils.RespondError(w, http.StatusForbidden, "branding is not available on the free tier")
			return
		}

		var req BrandingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		tenant.Branding = map[string]interface{}{
			"logoUrl":      req.LogoURL,
			"primaryColor": req.PrimaryColor,
			"companyName":  req.CompanyName,
			"customDomain": req.CustomDomain,
		}

		updated, err := repo.Update(ctx, tenant)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		utils.RespondJSON(w, http.StatusOK, updated.Branding)
	}
}

type UpgradeTierRequest struct {
	Tier string `json:"tier"`
}

func JoinTenantBySlugHandler(tenantRepo Repository, svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req struct {
			Slug string `json:"slug"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		// Tenant per Slug holen
		ctx := tenantdb.WithTenantID(r.Context(), "default")
		tenant, err := tenantRepo.GetBySlug(ctx, req.Slug)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		email, _ := authclient.GetUserEmail(r)
		_, _, err = svc.ProvisionWithTenant(r.Context(), service.ProvisionInput{
			FirebaseUID: firebaseUID,
			Email:       email,
			TenantID:    tenant.ID,
			Role:        "tenant_member",
		})
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		utils.RespondJSON(w, http.StatusOK, map[string]string{
			"tenantId": tenant.ID,
			"name":     tenant.Name,
		})
	}
}

func UpgradeTierHandler(repo Repository, provisioner *GitHubProvisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			utils.RespondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		role := authclient.GetUserRole(r)
		if role != "tenant_owner" && role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		var req UpgradeTierRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Tier != "free" && req.Tier != "standard" && req.Tier != "enterprise" {
			utils.RespondError(w, http.StatusBadRequest, "tier must be free, standard or enterprise")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenant, err := repo.GetByID(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		previousTier := tenant.Tier
		tenant.Tier = req.Tier
		updated, err := repo.Update(ctx, tenant)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Enterprise Provisioning / Deprovisioning
		if provisioner != nil {
			if req.Tier == "enterprise" && previousTier != "enterprise" {
				dbPassword := generateDBPassword()
				dbURL := fmt.Sprintf(
					"postgres://trips_enterprise:%s@trips-postgres-%s.trip-manager-%s.svc.cluster.local:5432/trips?sslmode=disable",
					dbPassword, tenant.Slug, tenant.Slug,
				)

				// DB-URL in users-DB speichern
				defaultCtx := tenantdb.WithTenantID(r.Context(), "default")
				if err := repo.SaveEnterpriseDBURL(defaultCtx, tenant.ID, dbURL); err != nil {
					log.Printf("warn: failed to save enterprise db url: %v", err)
				}

				go func() {
					if err := provisioner.ProvisionEnterpriseTenant(
						context.Background(),
						tenant.Slug,
						dbPassword,
					); err != nil {
						log.Printf("enterprise provisioning failed for %s: %v", tenant.Slug, err)
					}
				}()
			} else if req.Tier != "enterprise" && previousTier == "enterprise" {
				defaultCtx := tenantdb.WithTenantID(r.Context(), "default")
				if err := repo.DeleteEnterpriseDBURL(defaultCtx, tenant.ID); err != nil {
					log.Printf("warn: failed to delete enterprise db url: %v", err)
				}

				go func() {
					if err := provisioner.DeprovisionEnterpriseTenant(
						context.Background(),
						tenant.Slug,
					); err != nil {
						log.Printf("enterprise deprovisioning failed for %s: %v", tenant.Slug, err)
					}
				}()

				go func() {
					if err := provisioner.DeprovisionEnterpriseTenant(
						context.Background(),
						tenant.Slug,
					); err != nil {
						log.Printf("enterprise deprovisioning failed for %s: %v", tenant.Slug, err)
					}
				}()
			}
		}

		utils.RespondJSON(w, http.StatusOK, RegisterResponse{
			TenantID: updated.ID,
			Name:     updated.Name,
			Slug:     updated.Slug,
			Tier:     updated.Tier,
		})
	}
}

func DeleteTenantHandler(repo Repository, userRepo repository.Repository, userSvc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			utils.RespondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		role := authclient.GetUserRole(r)
		if role != "tenant_owner" && role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "only tenant owners can delete a tenant")
			return
		}

		firebaseUID, _ := authclient.GetUserID(r)
		ctx := tenantdb.WithTenantID(r.Context(), tenantID)

		// Erst alle User auf default zurücksetzen
		if err := userRepo.ResetTenantUsers(ctx, tenantID); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Dann Tenant löschen
		if err := repo.Delete(ctx, tenantID); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Owner Firebase Claims zurücksetzen
		_, _, _ = userSvc.ProvisionWithTenant(r.Context(), service.ProvisionInput{
			FirebaseUID: firebaseUID,
			TenantID:    "default",
			Role:        "tenant_member",
		})

		w.WriteHeader(http.StatusNoContent)
	}
}

func ListAllTenantsHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}
		tenants, err := repo.ListAll(r.Context())
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tenants == nil {
			tenants = []*Tenant{}
		}
		utils.RespondJSON(w, http.StatusOK, tenants)
	}
}

// Settings related

type SettingsRequest struct {
	MaxActiveTrips *int `json:"maxActiveTrips,omitempty"`
}

func GetSettingsHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			utils.RespondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenant, err := repo.GetByID(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		utils.RespondJSON(w, http.StatusOK, tenant.Settings)
	}
}

func UpdateSettingsHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			utils.RespondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		role := authclient.GetUserRole(r)
		if role != "tenant_owner" && role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenant, err := repo.GetByID(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		// Settings (z.B. maxActiveTrips) sind nur auf Standard+ frei konfigurierbar
		if tenant.Tier == "free" {
			utils.RespondError(w, http.StatusForbidden, "settings are not configurable on the free tier")
			return
		}

		var req SettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if tenant.Settings == nil {
			tenant.Settings = map[string]interface{}{}
		}
		if req.MaxActiveTrips != nil {
			tenant.Settings["maxActiveTrips"] = *req.MaxActiveTrips
		}

		updated, err := repo.Update(ctx, tenant)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		utils.RespondJSON(w, http.StatusOK, updated.Settings)
	}
}
