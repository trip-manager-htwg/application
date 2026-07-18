package tenant

import (
	"net/http"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	utils "github.com/trip-manager-htwg/application/backend/users/internal/shared"
	"github.com/trip-manager-htwg/application/backend/users/service"
)

func AcceptInvitationHandler(invRepo InvitationRepository, svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			utils.RespondError(w, http.StatusBadRequest, "token is required")
			return
		}

		inv, err := invRepo.GetByToken(r.Context(), token)
		if err != nil {
			utils.RespondError(w, http.StatusNotFound, "invalid or expired invitation")
			return
		}

		userEmail, _ := authclient.GetUserEmail(r)
		if inv.Email != "" && userEmail != inv.Email {
			utils.RespondError(w, http.StatusForbidden, "this invitation was sent to a different email address")
			return
		}

		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// User zum Tenant hinzufügen
		_, _, err = svc.ProvisionWithTenant(r.Context(), service.ProvisionInput{
			FirebaseUID: firebaseUID,
			TenantID:    inv.TenantID,
			Role:        inv.Role,
		})
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Einladung als akzeptiert markieren
		if err := invRepo.Accept(r.Context(), token); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		utils.RespondJSON(w, http.StatusOK, map[string]string{
			"tenantId": inv.TenantID,
			"role":     inv.Role,
		})
	}
}
