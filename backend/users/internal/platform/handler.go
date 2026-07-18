package platform

import (
	"encoding/json"
	"net/http"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
	utils "github.com/trip-manager-htwg/application/backend/users/internal/shared"
)

func GetConfigHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}
		cfg, err := repo.GetConfig(r.Context())
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusOK, cfg)
	}
}

func UpdateConfigHandler(repo Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := authclient.GetUserRole(r)
		if role != "platform_admin" {
			utils.RespondError(w, http.StatusForbidden, "permission denied")
			return
		}

		var req Config
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		ctx := tenantdb.WithTenantID(r.Context(), "default")

		if err := repo.UpdateTierConfig(ctx, "free", req.Free); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := repo.UpdateTierConfig(ctx, "standard", req.Standard); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := repo.UpdateTierConfig(ctx, "enterprise", req.Enterprise); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		utils.RespondJSON(w, http.StatusOK, req)
	}
}
