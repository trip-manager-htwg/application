package accommodation

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	generated "github.com/trip-manager-htwg/application/backend/trips/generated"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func getToken(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func getIntQuery(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

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
		Id: openapi_types.UUID(id),
		CreatedBy: generated.UserSummary{
			Id:    openapi_types.UUID(creatorID),
			Name:  a.CreatedBy.Name,
			Email: openapi_types.Email(a.CreatedBy.Email),
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
			respondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		limit := getIntQuery(r, "limit", 10)
		offset := getIntQuery(r, "offset", 0)

		accommodations, total, err := svc.ListByTrip(r.Context(), tripID, limit, offset)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]generated.AccommodationResponse, len(accommodations))
		for i, a := range accommodations {
			data[i] = toResponse(a)
		}
		respondJSON(w, http.StatusOK, generated.AccommodationListResponse{
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
			respondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		token := getToken(r)
		if token == "" {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}

		var req generated.CreateAccommodationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		a, err := svc.Create(r.Context(), &req, tripID, user.ID, user.Name, user.Email)
		if err != nil {
			if errors.Is(err, ErrInvalidInput) {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusCreated, toResponse(a))
	}
}

func UpdateHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accommodationID := r.PathValue("accommodationId")
		if accommodationID == "" {
			respondError(w, http.StatusBadRequest, "accommodationId is required")
			return
		}
		token := getToken(r)
		if token == "" {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}

		var req generated.UpdateAccommodationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		a, err := svc.Update(r.Context(), &req, accommodationID, user.ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "accommodation not found")
				return
			}
			if errors.Is(err, ErrUnauthorized) {
				respondError(w, http.StatusForbidden, "forbidden")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, toResponse(a))
	}
}

func DeleteHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accommodationID := r.PathValue("accommodationId")
		if accommodationID == "" {
			respondError(w, http.StatusBadRequest, "accommodationId is required")
			return
		}
		token := getToken(r)
		if token == "" {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}

		if err := svc.Delete(r.Context(), accommodationID, user.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "accommodation not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
