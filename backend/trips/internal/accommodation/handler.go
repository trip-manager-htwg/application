package accommodation

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	openapitypes "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	"github.com/trip-manager-htwg/application/backend/trips/generated"
	utils "github.com/trip-manager-htwg/application/backend/trips/internal/shared"
)

// ── Mapper ────────────────────────────────────────────────────────────────────

func toPlaceSummary(p Place) generated.PlaceSummary {
	return generated.PlaceSummary{
		Name:        p.Name,
		City:        p.City,
		Country:     p.Country,
		Lat:         p.Lat,
		Lng:         p.Lng,
		CountryCode: p.CountryCode,
	}
}

func toResponse(a *Accommodation) generated.AccommodationResponse {
	id, _ := uuid.Parse(a.ID)
	creatorID, _ := uuid.Parse(a.CreatedBy.ID)

	var address *string
	if a.Address != "" {
		address = &a.Address
	}
	var notes *string
	if a.Notes != "" {
		notes = &a.Notes
	}

	return generated.AccommodationResponse{
		Id: id,
		CreatedBy: generated.UserSummary{
			Id:    creatorID,
			Name:  a.CreatedBy.Name,
			Email: openapitypes.Email(a.CreatedBy.Email),
		},
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
		Location:      toPlaceSummary(a.Location),
		Name:          a.Name,
		Address:       address,
		CheckIn:       a.CheckIn,
		CheckOut:      a.CheckOut,
		PricePerNight: a.PricePerNight,
		Notes:         notes,
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func ListHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		limit := utils.GetIntQuery(r, "limit", 10)
		offset := utils.GetIntQuery(r, "offset", 0)

		accommodations, total, err := svc.ListByTrip(r.Context(), tripID, limit, offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]generated.AccommodationResponse, len(accommodations))
		for i, a := range accommodations {
			data[i] = toResponse(a)
		}
		utils.RespondJSON(w, http.StatusOK, generated.AccommodationListResponse{
			Data:   data,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func CreateHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
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

		var req generated.CreateAccommodationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		a, err := svc.Create(r.Context(), &req, tripID, user.ID, user.Name, user.Email)
		if err != nil {
			if errors.Is(err, ErrInvalidInput) {
				utils.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusCreated, toResponse(a))
	}
}

func UpdateHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accommodationID := r.PathValue("accommodationId")
		if accommodationID == "" {
			utils.RespondError(w, http.StatusBadRequest, "accommodationId is required")
			return
		}
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

		var req generated.UpdateAccommodationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		a, err := svc.Update(r.Context(), &req, accommodationID, user.ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "accommodation not found")
				return
			}
			if errors.Is(err, ErrUnauthorized) {
				utils.RespondError(w, http.StatusForbidden, "forbidden")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusOK, toResponse(a))
	}
}

func DeleteHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accommodationID := r.PathValue("accommodationId")
		if accommodationID == "" {
			utils.RespondError(w, http.StatusBadRequest, "accommodationId is required")
			return
		}
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

		if err := svc.Delete(r.Context(), accommodationID, user.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "accommodation not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
