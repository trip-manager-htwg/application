package platform

import (
	"encoding/json"
	"net/http"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
)

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func GetConfigHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}
		cfg, err := repo.GetConfig(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, cfg)
	}
}

func UpdateConfigHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}

		var req PlatformConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), "default")

		if err := repo.UpdateTierConfig(ctx, "free", req.Free); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := repo.UpdateTierConfig(ctx, "standard", req.Standard); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := repo.UpdateTierConfig(ctx, "enterprise", req.Enterprise); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		respondJSON(w, http.StatusOK, req)
	}
}
