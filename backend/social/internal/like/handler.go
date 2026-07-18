package like

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/social/internal/shared"
	"github.com/trip-manager-htwg/application/backend/social/pubsub"
)

// GetTripLikesHandler handles GET /trips/{tripId}/likes (authclient required)
func GetTripLikesHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Trip ID is required")
			return
		}
		tenantID := authclient.GetTenantID(r)
		userID, _ := authclient.GetUserID(r)

		resp, err := svc.GetEntityLikeInfo(r.Context(), userID, tenantID, tripID)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		shared.RespondJSON(w, http.StatusOK, resp)
	}
}

// LikeTripHandler handles POST /trips/{tripId}/likes (authclient required)
func LikeTripHandler(svc Service, producer *pubsub.Producer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			shared.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		tenantID := authclient.GetTenantID(r)

		tripID := r.PathValue("tripId")
		if tripID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Trip ID is required")
			return
		}

		err := svc.LikeEntity(r.Context(), userID, tenantID, tripID)
		if err != nil {
			if errors.Is(err, shared.ErrConflict) {
				shared.RespondError(w, http.StatusConflict, "already liked")
				return
			}
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Pub/Sub Event – fire-and-forget
		if producer != nil {
			if err := producer.PublishTripCommented(r.Context(), pubsub.TripCommentedEvent{
				TripID:    tripID,
				UserID:    userID, // Firebase UID
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				TenantID:  tenantID,
			}); err != nil {
				log.Printf("warn: failed to publish trip.commented for trip %s: %v", tripID, err)
			}
		}
		w.WriteHeader(http.StatusCreated)
	}
}

// UnlikeTripHandler handles DELETE /trips/{tripId}/likes (authclient required)
func UnlikeTripHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			shared.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		tripID := r.PathValue("tripId")
		if tripID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Trip ID is required")
			return
		}

		err := svc.UnlikeEntity(r.Context(), userID, tripID)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
