package advertiser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"tenantdb"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/firebaseclient"
	"github.com/trip-manager-htwg/application/backend/users/internal/tenant"
)

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

// POST /advertisers – nur platform_admin
func CreateHandler(repo Repository, firebaseAuth *firebaseclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		firebaseUID, _ := authclient.GetUserID(r)

		// Platform/Tenant Admins können beliebige Advertiser anlegen
		// Normale User können sich selbst als Advertiser registrieren
		var req struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Email == "" || req.Name == "" {
			respondError(w, http.StatusBadRequest, "email and name are required")
			return
		}

		// Admins können für andere anlegen, normale User nur für sich selbst
		if role != "platform_admin" && role != "tenant_owner" && role != "tenant_admin" {
			// Self-registration: Firebase UID aus JWT
			if firebaseUID == "" {
				respondError(w, http.StatusForbidden, "permission denied")
				return
			}
		}

		adv, err := repo.Create(r.Context(), firebaseUID, req.Email, req.Name)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if firebaseAuth != nil && firebaseUID != "" {
			go firebaseAuth.SetCustomClaims(context.Background(), firebaseUID, map[string]interface{}{
				"role":          "advertiser",
				"advertiser_id": adv.ID,
			})
		}

		respondJSON(w, http.StatusCreated, adv)
	}
}

// GET /advertisers – nur platform_admin
func ListHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}

		advs, err := repo.List(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if advs == nil {
			advs = []*Advertiser{}
		}
		respondJSON(w, http.StatusOK, advs)
	}
}

// GET /advertisers/me – für eingeloggte Advertiser
func GetMeHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		adv, err := repo.GetByFirebaseUID(r.Context(), firebaseUID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "advertiser not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, adv)
	}
}

// POST /advertisers/{id}/tenants – nur platform_admin
func AssignTenantHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}

		advertiserID := r.PathValue("id")
		var req struct {
			TenantID string `json:"tenantId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.TenantID == "" {
			respondError(w, http.StatusBadRequest, "tenantId is required")
			return
		}

		if err := repo.AssignTenant(r.Context(), advertiserID, req.TenantID); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /advertisers/{id}/tenants/{tenantId} – nur platform_admin
func RemoveTenantHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}

		advertiserID := r.PathValue("id")
		tenantID := r.PathValue("tenantId")

		if err := repo.RemoveTenant(r.Context(), advertiserID, tenantID); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /advertisers/{id} – nur platform_admin
func GetByIDHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}

		id := r.PathValue("id")
		adv, err := repo.GetByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "advertiser not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, adv)
	}
}

func ContactTenantHandler(repo Repository, tenantRepo tenant.Repository, emailSvc *tenant.EmailService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		advertiserID := r.PathValue("id")
		tenantID := r.PathValue("tenantId")

		var req struct {
			Message string `json:"message"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		adv, err := repo.GetByID(r.Context(), advertiserID)
		if err != nil || adv.FirebaseUID != firebaseUID {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}

		// Tenant-Owner Email holen
		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenantObj, err := tenantRepo.GetByID(ctx, tenantID)
		if err != nil {
			respondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		// Tenant-Owner aus users holen
		ownerEmail, err := tenantRepo.GetOwnerEmail(ctx, tenantID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to get tenant owner")
			return
		}

		if emailSvc != nil {
			go emailSvc.SendContactRequest(ownerEmail, adv.Name, adv.Email, tenantObj.Name, req.Message)
		}

		respondJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	}
}
