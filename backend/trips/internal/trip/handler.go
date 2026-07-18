package trip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	openapitypes "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	"github.com/trip-manager-htwg/application/backend/trips/generated"
	utils "github.com/trip-manager-htwg/application/backend/trips/internal/shared"
	"github.com/trip-manager-htwg/application/backend/trips/pubsub"
)

// ── Helpers ───────────────────────────────────────────────────────────────────
func toResponse(t *Trip) generated.TripResponse {
	var avatarUrl *string
	if t.CreatedBy.AvatarKey != nil {
		avatarUrl = t.CreatedBy.AvatarKey
	}
	return generated.TripResponse{
		Id:               uuid.MustParse(t.ID),
		Title:            t.Title,
		ShortDescription: t.ShortDescription,
		Description:      toStringPtr(t.Description),
		StartDate:        openapitypes.Date{Time: t.StartDate},
		EndDate:          openapitypes.Date{Time: t.EndDate},
		Status:           generated.TripResponseStatus(t.Status),
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
		CreatedBy: generated.UserSummary{
			Id:        uuid.MustParse(t.CreatedBy.ID),
			Name:      t.CreatedBy.Name,
			Email:     openapitypes.Email(t.CreatedBy.Email),
			AvatarUrl: avatarUrl,
		},
	}
}

func toStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func enrichWithUserInfo(ctx context.Context, trips []*Trip, usersClient *userclient.UsersClient) {
	userIDs := make(map[string]struct{})
	for _, t := range trips {
		userIDs[t.CreatedBy.ID] = struct{}{}
	}

	users := make(map[string]*userclient.UserResponse)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for id := range userIDs {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			user, err := usersClient.GetByID(ctx, userID)
			if err != nil {
				return
			}
			mu.Lock()
			users[userID] = user
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	for _, t := range trips {
		if u, ok := users[t.CreatedBy.ID]; ok {
			t.CreatedBy.Name = u.Name
			t.CreatedBy.Email = u.Email
			t.CreatedBy.AvatarKey = &u.AvatarUrl
		}
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func ListTripsHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := utils.GetToken(r)
		if token == "" {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}
		limit := utils.GetIntQuery(r, "limit", 10)
		offset := utils.GetIntQuery(r, "offset", 0)
		trips, total, err := svc.List(r.Context(), user.ID, limit, offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		enrichWithUserInfo(r.Context(), trips, usersClient)
		data := make([]generated.TripResponse, len(trips))
		for i, t := range trips {
			data[i] = toResponse(t)
		}
		utils.RespondJSON(w, http.StatusOK, generated.TripListResponse{
			Data:   data,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func ListRecentTripsHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := utils.GetIntQuery(r, "limit", 25)
		offset := utils.GetIntQuery(r, "offset", 0)
		trips, total, err := svc.ListRecent(r.Context(), limit, offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		enrichWithUserInfo(r.Context(), trips, usersClient)
		data := make([]generated.TripResponse, len(trips))
		for i, t := range trips {
			data[i] = toResponse(t)
		}
		utils.RespondJSON(w, http.StatusOK, generated.TripListResponse{
			Data:   data,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func SearchTripsHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			utils.RespondError(w, http.StatusBadRequest, "q is required")
			return
		}
		limit := utils.GetIntQuery(r, "limit", 10)
		offset := utils.GetIntQuery(r, "offset", 0)
		trips, total, err := svc.Search(r.Context(), q, limit, offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		enrichWithUserInfo(r.Context(), trips, usersClient)
		data := make([]generated.TripResponse, len(trips))
		for i, t := range trips {
			data[i] = toResponse(t)
		}
		utils.RespondJSON(w, http.StatusOK, generated.TripListResponse{
			Data:   data,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func GetTripHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		trip, err := svc.GetByID(r.Context(), tripID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "trip not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// enrich with user info
		user, err := usersClient.GetByID(r.Context(), trip.CreatedBy.ID)
		if err == nil {
			trip.CreatedBy.Name = user.Name
			trip.CreatedBy.Email = user.Email
			trip.CreatedBy.AvatarKey = &user.AvatarUrl
		}
		utils.RespondJSON(w, http.StatusOK, toResponse(trip))
	}
}

func CreateTripHandler(svc Service, usersClient *userclient.UsersClient, producer *pubsub.Producer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := utils.GetToken(r)
		if token == "" {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}

		// Tenant-Limit prüfen (pro User)
		settings, err := usersClient.GetTenantSettings(r.Context(), token)
		if err != nil {
			log.Printf("warn: failed to fetch tenant settings, skipping limit check: %v", err)
		} else if settings.MaxActiveTrips > 0 {
			activeCount, err := svc.CountActiveByUser(r.Context(), user.ID)
			if err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to check trip limit")
				return
			}
			if activeCount >= settings.MaxActiveTrips {
				utils.RespondError(w, http.StatusForbidden, fmt.Sprintf(
					"trip limit reached: your plan allows a maximum of %d active trips",
					settings.MaxActiveTrips,
				))
				return
			}
		}

		var req generated.CreateTripRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		desc := ""
		if req.Description != nil {
			desc = *req.Description
		}
		trip, err := svc.Create(r.Context(), CreateInput{
			Title:            req.Title,
			ShortDescription: req.ShortDescription,
			Description:      desc,
			StartDate:        req.StartDate.Time,
			EndDate:          req.EndDate.Time,
			UserID:           user.ID,
			UserName:         user.Name,
			UserEmail:        user.Email,
		})
		if err != nil {
			if errors.Is(err, ErrInvalidInput) {
				utils.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// PubSub Event – fire-and-forget, Fehler nur loggen
		tenantID := authclient.GetTenantID(r)

		if producer != nil {
			if err := producer.PublishTripCreated(r.Context(), pubsub.TripCreatedEvent{
				TripID:    trip.ID,
				UserID:    user.ID,
				UserName:  user.Name,
				Title:     trip.Title,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				TenantID:  tenantID,
			}); err != nil {
				log.Printf("warn: failed to publish trip.created for trip %s: %v", trip.ID, err)
			}
		}

		utils.RespondJSON(w, http.StatusCreated, toResponse(trip))
	}
}

func UpdateTripHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := utils.GetToken(r)
		if token == "" {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}
		tripID := r.PathValue("tripId")
		if tripID == "" {
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		var req generated.UpdateTripRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		input := UpdateInput{
			ID:               tripID,
			UserID:           user.ID,
			Title:            req.Title,
			ShortDescription: req.ShortDescription,
			Description:      req.Description,
		}
		if req.StartDate != nil {
			t := req.StartDate.Time
			input.StartDate = &t
		}
		if req.EndDate != nil {
			t := req.EndDate.Time
			input.EndDate = &t
		}
		if req.Status != nil {
			s := string(*req.Status)
			input.Status = &s
		}
		trip, err := svc.Update(r.Context(), input)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "trip not found")
				return
			}
			if errors.Is(err, ErrUnauthorized) {
				utils.RespondError(w, http.StatusForbidden, "forbidden")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusOK, toResponse(trip))
	}
}

func DeleteTripHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := utils.GetToken(r)
		if token == "" {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}
		tripID := r.PathValue("tripId")
		if tripID == "" {
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		err = svc.Delete(r.Context(), tripID, user.ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "trip not found")
				return
			}
			if errors.Is(err, ErrUnauthorized) {
				utils.RespondError(w, http.StatusForbidden, "forbidden")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
