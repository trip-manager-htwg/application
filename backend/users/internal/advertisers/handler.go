package advertiser

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"tenantdb"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/firebaseclient"
	utils "github.com/trip-manager-htwg/application/backend/users/internal/shared"
	"github.com/trip-manager-htwg/application/backend/users/internal/tenant"
)

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
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Email == "" || req.Name == "" {
			utils.RespondError(w, http.StatusBadRequest, "email and name are required")
			return
		}

		// Admins können für andere anlegen, normale User nur für sich selbst
		if role != "platform_admin" && role != "tenant_owner" && role != "tenant_admin" {
			// Self-registration: Firebase UID aus JWT
			if firebaseUID == "" {
				utils.RespondError(w, http.StatusForbidden, "permission denied")
				return
			}
		}

		adv, err := repo.Create(r.Context(), firebaseUID, req.Email, req.Name)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if firebaseAuth != nil && firebaseUID != "" {
			go func() {
				err := firebaseAuth.SetCustomClaims(context.Background(), firebaseUID, map[string]interface{}{
					"role":          "advertiser",
					"advertiser_id": adv.ID,
				})
				if err != nil {
					log.Printf("failed to set custom claims: %v", err)
				}
			}()
		}

		utils.RespondJSON(w, http.StatusCreated, adv)
	}
}

func ListHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		advs, err := repo.List(r.Context())
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if advs == nil {
			advs = []*Advertiser{}
		}
		utils.RespondJSON(w, http.StatusOK, advs)
	}
}

func GetMeHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		adv, err := repo.GetByFirebaseUID(r.Context(), firebaseUID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "advertiser not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusOK, adv)
	}
}

func AssignTenantHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		advertiserID := r.PathValue("id")
		var req struct {
			TenantID string `json:"tenantId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.TenantID == "" {
			utils.RespondError(w, http.StatusBadRequest, "tenantId is required")
			return
		}

		if err := repo.AssignTenant(r.Context(), advertiserID, req.TenantID); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func RemoveTenantHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		advertiserID := r.PathValue("id")
		tenantID := r.PathValue("tenantId")

		if err := repo.RemoveTenant(r.Context(), advertiserID, tenantID); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func GetByIDHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		id := r.PathValue("id")
		adv, err := repo.GetByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "advertiser not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusOK, adv)
	}
}

func ContactTenantHandler(repo Repository, tenantRepo tenant.Repository, emailSvc *tenant.EmailService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		advertiserID := r.PathValue("id")
		tenantID := r.PathValue("tenantId")

		var req struct {
			Message string `json:"message"`
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			return
		}

		adv, err := repo.GetByID(r.Context(), advertiserID)
		if err != nil || adv.FirebaseUID != firebaseUID {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		// Tenant-Owner Email holen
		ctx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenantObj, err := tenantRepo.GetByID(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		// Tenant-Owner aus users holen
		ownerEmail, err := tenantRepo.GetOwnerEmail(ctx, tenantID)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get tenant owner")
			return
		}

		if emailSvc != nil {
			go func() {
				err := emailSvc.SendContactRequest(ownerEmail, adv.Name, adv.Email, tenantObj.Name, req.Message)
				if err != nil {
					log.Printf("failed to send contact request email: %v", err)
				}
			}()
		}

		utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	}
}
